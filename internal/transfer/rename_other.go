//go:build !linux || !(amd64 || arm64)

package transfer

func renameat2NoReplace(oldpath, newpath string) error { return errUnsupported }
