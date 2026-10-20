//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	markBegin = "# >>> loki >>>"
	markEnd   = "# <<< loki <<<"
)

var markBlock = regexp.MustCompile(`(?s)\n?` + regexp.QuoteMeta(markBegin) + `.*?` + regexp.QuoteMeta(markEnd) + `\n?`)

type rcFile struct {
	path   string
	line   string
	create bool // create the file when missing
}

// rcFiles lists the shell startup files loki manages. ~/.profile is always
// written so login shells (and GUI-launched editors on most desktops) see it;
// interactive rc files are only touched when they already exist.
func rcFiles(dir string) []rcFile {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	sh := fmt.Sprintf("export PATH=%q:\"$PATH\"", dir)
	files := []rcFile{
		{filepath.Join(home, ".profile"), sh, true},
		{filepath.Join(home, ".bashrc"), sh, false},
		{filepath.Join(home, ".zshrc"), sh, false},
	}
	if fi, err := os.Stat(filepath.Join(home, ".config", "fish")); err == nil && fi.IsDir() {
		files = append(files, rcFile{filepath.Join(home, ".config", "fish", "conf.d", "loki.fish"), fmt.Sprintf("fish_add_path %q", dir), true})
	}
	return files
}

func addToUserPath(dir string) (bool, string, error) {
	var touched []string
	for _, rc := range rcFiles(dir) {
		data, err := os.ReadFile(rc.path)
		if errors.Is(err, os.ErrNotExist) {
			if !rc.create {
				continue
			}
		} else if err != nil {
			return false, "", err
		}
		block := markBegin + "\n" + rc.line + "\n" + markEnd + "\n"
		text := string(data)
		if strings.Contains(text, block) {
			continue
		}
		text = markBlock.ReplaceAllString(text, "\n")
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if err := os.MkdirAll(filepath.Dir(rc.path), 0o755); err != nil {
			return false, "", err
		}
		if err := os.WriteFile(rc.path, []byte(text+block), 0o644); err != nil {
			return false, "", err
		}
		touched = append(touched, rc.path)
	}
	if len(touched) == 0 {
		return false, "shell rc files", nil
	}
	return true, strings.Join(touched, ", "), nil
}

func removeFromUserPath(dir string) (bool, string, error) {
	var touched []string
	for _, rc := range rcFiles(dir) {
		data, err := os.ReadFile(rc.path)
		if err != nil {
			continue
		}
		text := string(data)
		if !markBlock.MatchString(text) {
			continue
		}
		next := markBlock.ReplaceAllString(text, "\n")
		if strings.TrimSpace(next) == "" && strings.HasSuffix(rc.path, "loki.fish") {
			err = os.Remove(rc.path)
		} else {
			err = os.WriteFile(rc.path, []byte(next), 0o644)
		}
		if err != nil {
			return false, "", err
		}
		touched = append(touched, rc.path)
	}
	return len(touched) > 0, strings.Join(touched, ", "), nil
}

func userPathPersisted(dir string) (bool, string) {
	for _, rc := range rcFiles(dir) {
		data, err := os.ReadFile(rc.path)
		if err == nil && strings.Contains(string(data), markBegin) && strings.Contains(string(data), dir) {
			return true, rc.path
		}
	}
	return false, "shell rc files"
}
