package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const setupUsage = `Usage: loki setup [flags]

Copies this loki binary into a per-user install directory and adds that
directory to the user-level PATH, so editors such as Neovim can run "loki".
Safe to re-run: it upgrades the installed binary and never duplicates PATH.

Flags:
  -dir string   install directory (default %s)
  -no-path      only install the binary, leave PATH alone
  -uninstall    remove the installed binary and the PATH entry
`

func runSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprintf(os.Stderr, setupUsage, defaultInstallDir()) }
	dir := fs.String("dir", defaultInstallDir(), "")
	noPath := fs.Bool("no-path", false, "")
	uninstall := fs.Bool("uninstall", false, "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	installDir, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	target := filepath.Join(installDir, binaryName())

	if *uninstall {
		return uninstallFrom(installDir, target, *noPath)
	}

	if err := installBinary(target); err != nil {
		return fmt.Errorf("install binary: %w", err)
	}
	fmt.Println("installed", target)

	if *noPath {
		return nil
	}
	changed, where, err := addToUserPath(installDir)
	if err != nil {
		return fmt.Errorf("update PATH: %w", err)
	}
	if changed {
		fmt.Printf("added %s to the user PATH (%s)\n", installDir, where)
		fmt.Println("open a new terminal (and restart Neovim) to pick it up")
	} else {
		fmt.Printf("%s is already on the user PATH (%s)\n", installDir, where)
	}
	return nil
}

func uninstallFrom(installDir, target string, noPath bool) error {
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	os.Remove(target + ".old")
	fmt.Println("removed", target)
	if noPath {
		return nil
	}
	changed, where, err := removeFromUserPath(installDir)
	if err != nil {
		return fmt.Errorf("update PATH: %w", err)
	}
	if changed {
		fmt.Printf("removed %s from the user PATH (%s)\n", installDir, where)
	}
	return nil
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "loki.exe"
	}
	return "loki"
}

func defaultInstallDir() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "Programs", "loki")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".local", "bin")
}

// installBinary copies the running executable to target, atomically where the
// OS allows. A loki that is currently running from target (e.g. open inside
// Neovim) is moved aside first, since Windows cannot overwrite a running exe
// but can rename it.
func installBinary(target string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if self, err = filepath.EvalSymlinks(self); err != nil {
		return err
	}
	if samePath(self, target) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	in, err := os.Open(self)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(target), ".loki-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}

	if err := os.Rename(tmp.Name(), target); err == nil {
		return nil
	}
	old := target + ".old"
	os.Remove(old)
	if err := os.Rename(target, old); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// pathListHas reports whether dir is an entry of a PATH-style list.
func pathListHas(list, dir string) bool {
	for _, e := range filepath.SplitList(list) {
		if e != "" && samePath(os.ExpandEnv(e), dir) {
			return true
		}
	}
	return false
}
