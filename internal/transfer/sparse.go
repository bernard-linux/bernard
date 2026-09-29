package transfer

import (
	"io"
	"os"
)

// Taille des blocs examinés pour trouver les trous d'un fichier creux.
const holeBlock = 4096

// WriteSparse écrit b à la position courante de f, en sautant les blocs
// entièrement nuls au lieu de les écrire : le fichier reste creux (un disque
// virtuel de 100 Go qui n'en occupe que 20 garde ses 20 Go sur la cible).
// Le contenu lu reste identique ; l'appelant fixe la taille finale par
// Truncate, un trou final n'écrivant rien.
func WriteSparse(f *os.File, b []byte) error {
	for len(b) > 0 {
		n := holeBlock
		if len(b) < n {
			n = len(b)
		}
		chunk := b[:n]
		if n == holeBlock && allZero(chunk) {
			if _, err := f.Seek(int64(n), io.SeekCurrent); err != nil {
				return err
			}
		} else if _, err := f.Write(chunk); err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}
