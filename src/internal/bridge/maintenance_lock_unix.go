//go:build linux || darwin

// @module BridgeMaintenanceLock serializes bridge processes and offline maintenance.
package bridge

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// The parent is operator-owned. O_NOFOLLOW and a stable inode prevent lock-file substitution.
func acquireMaintenanceLock(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := lockDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(path+".maintenance.lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, errors.New("bridge maintenance lock unavailable")
	}
	file := os.NewFile(uintptr(fd), "bridge-maintenance-lock")
	if err = privateFile(file); err != nil {
		file.Close()
		return nil, err
	}
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("bridge is running or maintenance is active: stop every bridge writer first")
	}
	return file, nil
}

func privateFile(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		return errors.New("bridge maintenance requires owned, single-link 0600 regular files")
	}
	return nil
}

func privateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("bridge maintenance requires an owned 0700 real directory")
	}
	return nil
}

func lockDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("bridge lock requires an owned directory without group/other write access")
	}
	return nil
}

func readPrivate(path string, limit int64) ([]byte, error) {
	file, err := openPrivateFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readBounded(file, limit)
}

func openPrivateFile(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("protected bridge file unavailable")
	}
	file := os.NewFile(uintptr(fd), "protected-bridge-file")
	if err = privateFile(file); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
