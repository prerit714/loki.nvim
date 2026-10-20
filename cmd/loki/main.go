// Command loki renders a markdown file read-only, fullscreen, in the terminal.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev"

const usage = `loki - read-only markdown viewer for the terminal

Usage:
  loki [flags] <file.md>
  cat file.md | loki [flags]
  loki setup [-dir DIR] [-no-path] [-uninstall]   install loki and add it to the user PATH
  loki health                                     check install, PATH, terminal and Neovim
  loki version

Flags:
  -style string   glamour style: dark, light, auto, notty, dracula, ... (default "dark")
  -width int      max wrap width, 0 = terminal width (default 100)
  -no-watch       do not re-render when the file changes on disk
  -version        print version and exit

Keys:
  j/k up/down     scroll          d/u  half page      f/b space  page
  g/G home/end    top/bottom      /    search         n/N        next/prev match
  r               reload          q    esc  ctrl+c    quit
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "loki:", err)
		os.Exit(1)
	}
}

func run() error {
	// Subcommands. A markdown file with one of these names can still be
	// viewed as ./setup.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "setup":
			return runSetup(os.Args[2:])
		case "health":
			return runHealth(os.Args[2:])
		case "version":
			fmt.Println("loki", appVersion())
			return nil
		}
	}

	fs := flag.NewFlagSet("loki", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	style := fs.String("style", "dark", "")
	width := fs.Int("width", 100, "")
	noWatch := fs.Bool("no-watch", false, "")
	showVersion := fs.Bool("version", false, "")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *showVersion {
		fmt.Println("loki", appVersion())
		return nil
	}

	src, err := openSource(fs.Args())
	if err != nil {
		fs.Usage()
		return err
	}
	if *noWatch {
		src.path = ""
	}

	m := newModel(src, *style, *width)
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseCellMotion()}
	if src.name == "stdin" {
		// stdin holds the markdown, so read keys from the terminal instead.
		opts = append(opts, tea.WithInputTTY())
	}
	p := tea.NewProgram(m, opts...)
	_, err = p.Run()
	return err
}

// source is the markdown being viewed. path is empty when it came from stdin
// (or watching is disabled), in which case it is never reloaded.
type source struct {
	name    string
	path    string
	content []byte
	stamp   fileStamp
}

func openSource(args []string) (source, error) {
	switch {
	case len(args) > 1:
		return source{}, errors.New("expected a single file")
	case len(args) == 1 && args[0] != "-":
		path, err := filepath.Abs(args[0])
		if err != nil {
			return source{}, err
		}
		content, stamp, err := readFile(path)
		if err != nil {
			return source{}, err
		}
		return source{name: filepath.Base(path), path: path, content: content, stamp: stamp}, nil
	default:
		if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			return source{}, errors.New("no file given and nothing piped on stdin")
		}
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return source{}, err
		}
		return source{name: "stdin", content: content}, nil
	}
}

// fileStamp identifies a version of a file on disk cheaply.
type fileStamp struct {
	size    int64
	modNano int64
}

func readFile(path string) ([]byte, fileStamp, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fileStamp{}, err
	}
	if fi.IsDir() {
		return nil, fileStamp{}, fmt.Errorf("%s is a directory", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fileStamp{}, err
	}
	return content, fileStamp{fi.Size(), fi.ModTime().UnixNano()}, nil
}
