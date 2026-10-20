package main

import (
	"errors"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const userEnvKey = `Environment`

// readUserPath returns the persisted user PATH (HKCU\Environment\Path),
// unexpanded, and its registry value type.
func readUserPath() (string, uint32, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, userEnvKey, registry.QUERY_VALUE)
	if err != nil {
		return "", 0, err
	}
	defer k.Close()
	v, typ, err := k.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		return "", registry.EXPAND_SZ, nil
	}
	return v, typ, err
}

func writeUserPath(v string, typ uint32) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, userEnvKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if typ == registry.SZ {
		err = k.SetStringValue("Path", v)
	} else {
		err = k.SetExpandStringValue("Path", v)
	}
	if err != nil {
		return err
	}
	broadcastEnvChange()
	return nil
}

func addToUserPath(dir string) (bool, string, error) {
	const where = `HKCU\Environment\Path`
	cur, typ, err := readUserPath()
	if err != nil {
		return false, where, err
	}
	if userPathHas(cur, dir) {
		return false, where, nil
	}
	next := dir
	if cur = strings.TrimRight(cur, ";"); cur != "" {
		next = cur + ";" + dir
	}
	return true, where, writeUserPath(next, typ)
}

func removeFromUserPath(dir string) (bool, string, error) {
	const where = `HKCU\Environment\Path`
	cur, typ, err := readUserPath()
	if err != nil {
		return false, where, err
	}
	var keep []string
	for _, e := range strings.Split(cur, ";") {
		if e != "" && !samePath(expandWin(e), dir) {
			keep = append(keep, e)
		}
	}
	next := strings.Join(keep, ";")
	if next == strings.Trim(cur, ";") {
		return false, where, nil
	}
	return true, where, writeUserPath(next, typ)
}

// userPathPersisted reports whether dir is on the persisted user PATH, which is
// what new terminals and Neovim instances will see.
func userPathPersisted(dir string) (bool, string) {
	cur, _, err := readUserPath()
	if err != nil {
		return false, `HKCU\Environment\Path (unreadable: ` + err.Error() + `)`
	}
	return userPathHas(cur, dir), `HKCU\Environment\Path`
}

func userPathHas(list, dir string) bool {
	for _, e := range strings.Split(list, ";") {
		if e != "" && samePath(expandWin(e), dir) {
			return true
		}
	}
	return false
}

// expandWin expands %VAR% references the way Windows does for REG_EXPAND_SZ.
func expandWin(s string) string {
	src, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return s
	}
	buf := make([]uint16, 1024)
	n, err := windows.ExpandEnvironmentStrings(src, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return s
	}
	return syscall.UTF16ToString(buf[:n])
}

// broadcastEnvChange tells Explorer (and thus newly started programs) that the
// user environment changed, so new terminals see PATH without a re-login.
func broadcastEnvChange() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001A
		smtoAbortIfHung = 0x0002
	)
	env, _ := syscall.UTF16PtrFromString("Environment")
	proc := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	var result uintptr
	proc.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)),
		smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
}
