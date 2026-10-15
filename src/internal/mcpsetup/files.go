// @module MCPSetupFiles protects profile edits with private backups and checked atomic replacement.
package mcpsetup

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func noSymlinks(path string) error {
	current := string(os.PathSeparator)
	for _, part := range strings.Split(strings.TrimPrefix(path, current), string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlinked config, state or executable path refused")
		}
	}
	return nil
}
func privateDir(path string) error {
	if e := noSymlinks(path); e != nil {
		return e
	}
	if e := os.MkdirAll(path, 0700); e != nil {
		return e
	}
	info, e := os.Stat(path)
	if e != nil {
		return e
	}
	if !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return errors.New("config/state directory must not be group/world writable")
	}
	return nil
}
func openDirectory(path string) (*os.Root, error) {
	if e := noSymlinks(path); e != nil {
		return nil, e
	}
	return os.OpenRoot(path)
}
func readPrivate(path string) ([]byte, bool, error) {
	if e := noSymlinks(path); e != nil {
		return nil, false, e
	}
	f, e := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if os.IsNotExist(e) {
		return nil, false, nil
	}
	if e != nil {
		return nil, false, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return nil, false, e
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() > 8<<20 {
		return nil, false, errors.New("unsafe or oversized config/state file")
	}
	b, e := io.ReadAll(io.LimitReader(f, 8<<20+1))
	return b, true, e
}
func lock(root *os.Root) (func(), error) {
	f, e := root.OpenFile("setup.lock", os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if e != nil {
		return nil, e
	}
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, errors.New("unsafe setup lock file")
	}
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("another MCP setup operation holds the lock")
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
}
func randomName(prefix string) (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return prefix + hex.EncodeToString(b[:]), nil
}
func writeBackup(root *os.Root, data []byte) (string, error) {
	name, e := randomName("backup-")
	if e != nil {
		return "", e
	}
	f, e := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return "", e
	}
	if _, e = f.Write(data); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return "", e
	}
	if closeErr != nil {
		return "", closeErr
	}
	return name, syncRoot(root)
}
func syncRoot(root *os.Root) error {
	f, e := root.Open(".")
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

// Atomic rename plus a final byte check detects external editor changes. The
// advisory lock serializes this module; non-cooperating editors must be closed
// during the explicitly confirmed edit (portable filesystems lack rename-CAS).
func replaceChecked(path string, before []byte, existed bool, after []byte) error {
	parent := filepath.Dir(path)
	root, e := openDirectory(parent)
	if e != nil {
		return e
	}
	defer root.Close()
	name, e := randomName(".sessionsift-mcp-")
	if e != nil {
		return e
	}
	defer root.Remove(name)
	f, e := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(after); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	current, nowExists, e := readPrivate(path)
	if e != nil {
		return e
	}
	if nowExists != existed || !bytes.Equal(current, before) {
		return errors.New("config changed concurrently; refusing replacement")
	}
	if e = noSymlinks(parent); e != nil {
		return e
	}
	base := filepath.Base(path)
	if !existed {
		// Link, rather than rename, cannot overwrite an entry created concurrently.
		if e = root.Link(name, base); e != nil {
			return fmt.Errorf("create config without overwriting: %w", e)
		}
	} else if e = root.Rename(name, base); e != nil {
		return e
	}
	return syncRoot(root)
}
