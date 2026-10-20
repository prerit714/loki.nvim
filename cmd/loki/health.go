package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/muesli/termenv"
	"golang.org/x/term"
)

const healthUsage = `Usage: loki health [flags]

Checks the loki install, PATH, terminal and Neovim integration. Prints one
line per check prefixed with OK, WARN, FAIL or INFO, and exits 1 if any check
failed.

Flags:
  -dir string   install directory to check (default %s)
`

type checker struct{ failed bool }

func (c *checker) report(level, format string, a ...any) {
	if level == "FAIL" {
		c.failed = true
	}
	fmt.Printf("%-4s  %s\n", level, fmt.Sprintf(format, a...))
}

func runHealth(args []string) error {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprintf(os.Stderr, healthUsage, defaultInstallDir()) }
	dir := fs.String("dir", defaultInstallDir(), "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	installDir, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	target := filepath.Join(installDir, binaryName())
	c := &checker{}

	self, _ := os.Executable()
	c.report("INFO", "loki %s (%s/%s, %s) at %s", appVersion(), runtime.GOOS, runtime.GOARCH, runtime.Version(), self)

	// Installed binary.
	if _, err := os.Stat(target); err != nil {
		c.report("FAIL", "not installed at %s; run `loki setup`", target)
	} else if !sameContent(self, target) {
		c.report("WARN", "installed %s (version %q) differs from this binary (%q); run `loki setup` from the newer one", target, binaryVersion(target), appVersion())
	} else {
		c.report("OK", "installed at %s", target)
	}

	// Persisted user PATH: what new terminals and editors will inherit.
	if ok, where := userPathPersisted(installDir); ok {
		c.report("OK", "%s is on the persisted user PATH (%s)", installDir, where)
	} else {
		c.report("FAIL", "%s is not on the persisted user PATH (%s); run `loki setup`", installDir, where)
	}

	// PATH of this process: what `loki` resolves to right now.
	all := findOnPath(binaryName())
	switch {
	case len(all) == 0:
		if pathListHas(os.Getenv("PATH"), installDir) {
			c.report("FAIL", "loki not found on the current PATH")
		} else {
			c.report("WARN", "loki not on this shell's PATH yet; open a new terminal after `loki setup`")
		}
	case samePath(all[0], target):
		c.report("OK", "`loki` resolves to %s", all[0])
	default:
		c.report("WARN", "`loki` resolves to %s (version %q), which shadows %s; remove it or reorder PATH", all[0], binaryVersion(all[0]), target)
	}
	for _, p := range all[min(1, len(all)):] {
		c.report("INFO", "also on PATH: %s", p)
	}

	// Rendering.
	if _, err := renderMarkdown([]byte("# ok\n\n- `code`\n"), "dark", 80); err != nil {
		c.report("FAIL", "markdown rendering: %v", err)
	} else {
		c.report("OK", "markdown rendering works")
	}

	// Terminal.
	if term.IsTerminal(int(os.Stdout.Fd())) {
		c.report("INFO", "terminal: TERM=%q COLORTERM=%q colors=%s", os.Getenv("TERM"), os.Getenv("COLORTERM"), profileName(termenv.NewOutput(os.Stdout).EnvColorProfile()))
	} else {
		c.report("INFO", "stdout is not a terminal (fine when run from an editor)")
	}

	checkNeovim(c, target)

	if c.failed {
		return errors.New("some checks failed")
	}
	return nil
}

// checkNeovim asks a clean Neovim which loki it would launch; Neovim inherits
// PATH from wherever it was started, so this catches stale environments.
func checkNeovim(c *checker, target string) {
	nvim, err := exec.LookPath("nvim")
	if err != nil {
		c.report("INFO", "nvim not found on PATH; skipping Neovim checks")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, nvim, "--headless", "--clean",
		"--cmd", "lua io.stdout:write(vim.fn.exepath('loki'))", "--cmd", "qa!")
	// Windows searches the working directory first; keep a stray loki.exe
	// there (e.g. a build output) from masking the real lookup.
	cmd.Dir = os.TempDir()
	out, err := cmd.Output()
	got := strings.TrimSpace(string(out))
	switch {
	case err != nil:
		c.report("WARN", "could not query nvim: %v", err)
	case got == "":
		c.report("WARN", "nvim (%s) cannot find loki; restart Neovim from a new terminal after `loki setup`", nvim)
	case !samePath(got, target):
		c.report("WARN", "nvim launches %s instead of %s", got, target)
	default:
		c.report("OK", "nvim finds loki at %s", got)
	}
}

func findOnPath(name string) []string {
	var found []string
	seen := map[string]bool{}
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if d == "" {
			continue
		}
		p := filepath.Join(d, name)
		key := strings.ToLower(filepath.Clean(p))
		if seen[key] {
			continue
		}
		seen[key] = true
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			found = append(found, p)
		}
	}
	return found
}

func sameContent(a, b string) bool {
	if samePath(a, b) {
		return true
	}
	x, err1 := os.ReadFile(a)
	y, err2 := os.ReadFile(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

func binaryVersion(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "loki"))
}

func appVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				return "dev-" + s.Value[:7]
			}
		}
	}
	return version
}

func profileName(p termenv.Profile) string {
	switch p {
	case termenv.TrueColor:
		return "truecolor"
	case termenv.ANSI256:
		return "256"
	case termenv.ANSI:
		return "16"
	default:
		return "none"
	}
}
