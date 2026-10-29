//go:build !linux

package archive

import "os"

const archiveSupported = false

func archiveOpenLock(string) (*os.File, error)              { return nil, ErrUnsupported }
func archiveUnlock(*os.File) error                          { return ErrUnsupported }
func archiveOpenRegular(*os.Root, string) (*os.File, error) { return nil, ErrUnsupported }
