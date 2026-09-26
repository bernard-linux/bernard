//go:build linux && (amd64 || arm64)

package transfer

import (
	"os"
	"syscall"
)

// SyncFS écrit sur disque tout ce qui est en attente sur le système de
// fichiers qui contient dir (syncfs) : un seul appel pour des centaines de
// fichiers, au lieu d'un fsync par fichier.
func SyncFS(dir string, _ []string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if _, _, errno := syscall.Syscall(sysSyncfs, d.Fd(), 0, 0); errno != 0 {
		return errno
	}
	return nil
}
