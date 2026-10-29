//go:build linux

package archive

import (
	"fmt"
	"os"
	"syscall"
)

const archiveSupported = true

func archiveOpenLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("archive already locked: %w", err)
	}
	return file, nil
}
func archiveUnlock(file *os.File) error { return syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }
func archiveOpenRegular(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
