package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const pollInterval = 250 * time.Millisecond

var (
	barStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("236"))
	nameStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).Bold(true).Padding(0, 1)
	dimStyle   = barStyle.Foreground(lipgloss.Color("245"))
	errStyle   = barStyle.Foreground(lipgloss.Color("203"))
	matchStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("214"))
)

type model struct {
	src      source
	style    string
	maxWidth int

	vp    viewport.Model
	ready bool
	lines []string // rendered lines, ANSI stripped and lowercased, for search

	searching bool
	input     textinput.Model
	query     string
	matches   []int // line indices
	cur       int   // index into matches

	status string // transient message (errors, reloads)
}

type tickMsg struct{}

type reloadMsg struct {
	content []byte
	stamp   fileStamp
}

type errMsg struct{ err error }

func newModel(src source, style string, maxWidth int) model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.CharLimit = 256
	return model{src: src, style: style, maxWidth: maxWidth, input: ti}
}

func (m model) Init() tea.Cmd {
	return m.tick()
}

func (m model) tick() tea.Cmd {
	if m.src.path == "" {
		return nil
	}
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// checkFile stats the file off the main loop and reports a reload only when it
// changed. Errors are swallowed: editors that save by rename briefly remove the
// file, and the next tick will pick it up again.
func checkFile(path string, old fileStamp) tea.Cmd {
	return func() tea.Msg {
		content, stamp, err := readFile(path)
		if err != nil || stamp == old {
			return nil
		}
		return reloadMsg{content, stamp}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h := max(msg.Height-1, 1)
		if !m.ready {
			m.vp = viewport.New(msg.Width, h)
			m.ready = true
		} else {
			m.vp.Width, m.vp.Height = msg.Width, h
		}
		m.render()
		return m, nil

	case tickMsg:
		return m, tea.Batch(checkFile(m.src.path, m.src.stamp), m.tick())

	case reloadMsg:
		m.src.content, m.src.stamp = msg.content, msg.stamp
		m.render()
		m.status = "reloaded " + time.Now().Format("15:04:05")
		return m, nil

	case errMsg:
		m.status = "error: " + msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		if m.searching {
			return m.updateSearch(msg)
		}
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "g", "home":
			m.vp.GotoTop()
			return m, nil
		case "G", "end":
			m.vp.GotoBottom()
			return m, nil
		case "/":
			m.searching = true
			m.input.SetValue("")
			return m, m.input.Focus()
		case "n":
			m.jump(1)
			return m, nil
		case "N":
			m.jump(-1)
			return m, nil
		case "r":
			if m.src.path != "" {
				return m, checkFile(m.src.path, fileStamp{})
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.searching = false
		m.input.Blur()
		return m, nil
	case "enter":
		m.searching = false
		m.input.Blur()
		m.query = strings.ToLower(strings.TrimSpace(m.input.Value()))
		m.findMatches()
		m.cur = -1
		// Start from the first match at or below the top of the screen.
		for i, line := range m.matches {
			if line >= m.vp.YOffset {
				m.cur = i - 1
				break
			}
		}
		m.jump(1)
		m.render()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *model) findMatches() {
	m.matches = m.matches[:0]
	if m.query == "" {
		return
	}
	for i, line := range m.lines {
		if strings.Contains(line, m.query) {
			m.matches = append(m.matches, i)
		}
	}
}

func (m *model) jump(dir int) {
	if len(m.matches) == 0 {
		if m.query != "" {
			m.status = "no match: " + m.query
		}
		return
	}
	m.cur = (m.cur + dir + len(m.matches)) % len(m.matches)
	// Keep a little context above the match.
	m.vp.SetYOffset(m.matches[m.cur] - 3)
	m.status = fmt.Sprintf("match %d/%d", m.cur+1, len(m.matches))
}

// render re-runs glamour for the current width and keeps the scroll position.
func (m *model) render() {
	if !m.ready {
		return
	}
	width := m.vp.Width
	if m.maxWidth > 0 && width > m.maxWidth {
		width = m.maxWidth
	}
	out, err := renderMarkdown(m.src.content, m.style, width)
	if err != nil {
		m.status = "render error: " + err.Error()
		out = string(m.src.content)
	}

	raw := strings.Split(strings.TrimRight(out, "\n"), "\n")
	m.lines = make([]string, len(raw))
	for i, l := range raw {
		m.lines[i] = strings.ToLower(ansi.Strip(l))
	}
	m.findMatches()
	if m.cur >= len(m.matches) {
		m.cur = len(m.matches) - 1
	}

	matched := make(map[int]bool, len(m.matches))
	for _, i := range m.matches {
		matched[i] = true
	}
	pad := strings.Repeat(" ", max((m.vp.Width-width)/2, 0))
	for i, l := range raw {
		if matched[i] {
			// Mark matching lines in the gutter; recoloring inside glamour's
			// ANSI output would fight its own styling.
			raw[i] = markGutter(pad) + l
		} else {
			raw[i] = pad + l
		}
	}

	y := m.vp.YOffset
	m.vp.SetContent(strings.Join(raw, "\n"))
	m.vp.SetYOffset(y)
}

func markGutter(pad string) string {
	if pad == "" {
		return matchStyle.Render(">")
	}
	return pad[1:] + matchStyle.Render(">")
}

func renderMarkdown(content []byte, style string, width int) (string, error) {
	opts := []glamour.TermRendererOption{
		glamour.WithWordWrap(max(width-4, 20)),
		glamour.WithEmoji(),
	}
	if style == "auto" {
		opts = append(opts, glamour.WithAutoStyle())
	} else {
		opts = append(opts, glamour.WithStandardStyle(style))
	}
	r, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		return "", err
	}
	out, err := r.RenderBytes(content)
	return string(out), err
}

func (m model) View() string {
	if !m.ready {
		return ""
	}
	return m.vp.View() + "\n" + m.statusBar()
}

func (m model) statusBar() string {
	w := m.vp.Width
	if m.searching {
		m.input.Width = max(w-2, 1)
		return barStyle.Width(w).Render(m.input.View())
	}

	left := nameStyle.Render(m.src.name)
	right := barStyle.Render(fmt.Sprintf(" %3.f%% ", m.vp.ScrollPercent()*100))
	mid := " q quit  / search  g/G top/bottom"
	midStyle := dimStyle
	if m.status != "" {
		mid = " " + m.status
		if strings.HasPrefix(m.status, "error") || strings.HasPrefix(m.status, "render error") || strings.HasPrefix(m.status, "no match") {
			midStyle = errStyle
		}
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		return ansi.Truncate(left+right, w, "")
	}
	mid = ansi.Truncate(mid, gap, "…")
	return left + midStyle.Width(gap).Render(mid) + right
}
