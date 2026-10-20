package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallBinaryCopiesAndReplaces(t *testing.T) {
	target := filepath.Join(t.TempDir(), "bin", binaryName())
	if err := installBinary(target); err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	if !sameContent(self, target) {
		t.Fatal("installed binary differs from the running one")
	}
	if err := os.WriteFile(target, []byte("stale"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installBinary(target); err != nil {
		t.Fatal(err)
	}
	if !sameContent(self, target) {
		t.Fatal("reinstall did not replace a stale binary")
	}
}

func TestPathListHas(t *testing.T) {
	dir := t.TempDir()
	list := "/a" + string(os.PathListSeparator) + dir + string(filepath.Separator) + string(os.PathListSeparator) + "/b"
	if !pathListHas(list, dir) {
		t.Errorf("%q should contain %q", list, dir)
	}
	if pathListHas("/a", dir) {
		t.Errorf("false positive")
	}
}
