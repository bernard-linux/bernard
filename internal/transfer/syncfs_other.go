//go:build !linux || !(amd64 || arm64)

package transfer

import "os"

// SyncFS synchronise chaque fichier listé (repli hors Linux).
func SyncFS(_ string, files []string) error {
	for _, p := range files {
		f, err := os.OpenFile(p, os.O_RDWR, 0)
		if err != nil {
			return err
		}
		err = f.Sync()
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
