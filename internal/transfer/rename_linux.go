//go:build linux && (amd64 || arm64)

package transfer

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	atFdcwd         = -100
	renameNoreplace = 0x1
)

func renameat2NoReplace(oldpath, newpath string) error {
	o, err := syscall.BytePtrFromString(oldpath)
	if err != nil {
		return err
	}
	n, err := syscall.BytePtrFromString(newpath)
	if err != nil {
		return err
	}
	fd := atFdcwd
	_, _, errno := syscall.Syscall6(sysRenameat2,
		uintptr(fd), uintptr(unsafe.Pointer(o)),
		uintptr(fd), uintptr(unsafe.Pointer(n)),
		renameNoreplace, 0)
	switch {
	case errno == 0:
		return nil
	case errno == syscall.EEXIST:
		return os.ErrExist
	case errno == syscall.ENOSYS || errno == syscall.EINVAL:
		return errUnsupported
	default:
		return &os.LinkError{Op: "renameat2", Old: oldpath, New: newpath, Err: errno}
	}
}
