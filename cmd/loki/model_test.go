package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func longDoc(n int) []byte {
	var b strings.Builder
	b.WriteString("# Title\n\n")
	for i := range n {
		fmt.Fprintf(&b, "paragraph number %d\n\n", i)
	}
	b.WriteString("the needle is here\n")
	return []byte(b.String())
}

func sized(t *testing.T, m model, w, h int) model {
	t.Helper()
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(model)
}

func key(m model, k string) (model, tea.Cmd) {
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	next, cmd := m.Update(msg)
	return next.(model), cmd
}

func TestRendersAndFillsScreen(t *testing.T) {
	m := sized(t, newModel(source{name: "doc.md", content: longDoc(50)}, "dark", 100), 80, 20)
	view := ansi.Strip(m.View())
	lines := strings.Split(view, "\n")
	if len(lines) != 20 {
		t.Fatalf("view has %d lines, want 20", len(lines))
	}
	if !strings.Contains(view, "Title") {
		t.Errorf("heading not rendered:\n%s", view)
	}
	if !strings.Contains(lines[19], "doc.md") || !strings.Contains(lines[19], "%") {
		t.Errorf("status bar = %q", lines[19])
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > 80 {
			t.Errorf("line %d is %d cells wide", i, w)
		}
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "esc"} {
		m := sized(t, newModel(source{name: "x", content: []byte("hi")}, "dark", 100), 80, 20)
		_, cmd := key(m, k)
		if cmd == nil || cmd() != tea.Quit() {
			t.Errorf("%s did not quit", k)
		}
	}
}

func TestSearchJumpsToMatch(t *testing.T) {
	m := sized(t, newModel(source{name: "x", content: longDoc(100)}, "dark", 100), 80, 20)
	m, _ = key(m, "/")
	for _, r := range "NEEDLE" {
		m, _ = key(m, string(r))
	}
	m, _ = key(m, "enter")
	if len(m.matches) != 1 {
		t.Fatalf("matches = %v, want 1 (search is case-insensitive)", m.matches)
	}
	if !strings.Contains(ansi.Strip(m.vp.View()), "the needle is here") {
		t.Errorf("match not visible after search")
	}
	if !strings.Contains(m.status, "match 1/1") {
		t.Errorf("status = %q", m.status)
	}
}

func TestReloadKeepsScroll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(path, longDoc(100), 0o644); err != nil {
		t.Fatal(err)
	}
	content, stamp, err := readFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := sized(t, newModel(source{name: "doc.md", path: path, content: content, stamp: stamp}, "dark", 100), 80, 20)
	m.vp.SetYOffset(40)

	if err := os.WriteFile(path, append(longDoc(100), "\nappended line\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := checkFile(path, fileStamp{})()
	reload, ok := msg.(reloadMsg)
	if !ok {
		t.Fatalf("checkFile returned %T, want reloadMsg", msg)
	}
	next, _ := m.Update(reload)
	m = next.(model)
	if m.vp.YOffset != 40 {
		t.Errorf("YOffset = %d after reload, want 40", m.vp.YOffset)
	}
	if !strings.Contains(strings.Join(m.lines, "\n"), "appended line") {
		t.Errorf("reloaded content missing")
	}
	if checkFile(path, reload.stamp)() != nil {
		t.Errorf("unchanged file reported as changed")
	}
}

func TestOpenSourceErrors(t *testing.T) {
	if _, err := openSource([]string{filepath.Join(t.TempDir(), "missing.md")}); err == nil {
		t.Error("missing file: want error")
	}
	if _, err := openSource([]string{t.TempDir()}); err == nil {
		t.Error("directory: want error")
	}
	if _, err := openSource([]string{"a", "b"}); err == nil {
		t.Error("two files: want error")
	}
}
