package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bernard-linux/bernard/internal/i18n"
)

// PartPath renvoie le nom du fichier temporaire, stable pour une clé donnée :
// après une coupure, la réception reprend dans le même fichier.
func PartPath(dstDir, key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(dstDir, ".bernard-"+hex.EncodeToString(sum[:8])+".part")
}

// OpenPart ouvre le fichier temporaire pour écrire à partir de offset (point
// sûr lu dans le journal). Ce qui dépasse ce point, non garanti, est tronqué.
// Si le fichier est plus court, la reprise se fait à sa taille réelle.
func OpenPart(path string, offset int64) (*os.File, int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	if info.Size() < offset {
		offset = info.Size()
	}
	if err := f.Truncate(offset); err != nil {
		f.Close()
		return nil, 0, err
	}
	if _, err := f.Seek(offset, 0); err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, offset, nil
}

// CommitPartNoSync fait comme CommitPart, sans synchroniser le dossier :
// l'appelant groupe la synchronisation (syncfs) pour de nombreux fichiers.
func CommitPartNoSync(part, dst, expectHash string, size int64, mode fs.FileMode, mtime time.Time) (FileResult, error) {
	return commitPart(part, dst, expectHash, size, mode, mtime, false)
}

// CommitPart vérifie le fichier temporaire (relu depuis le disque) contre
// l'empreinte annoncée par la source, applique droits et date, puis le place
// sous son nom final sans jamais écraser.
func CommitPart(part, dst, expectHash string, size int64, mode fs.FileMode, mtime time.Time) (FileResult, error) {
	return commitPart(part, dst, expectHash, size, mode, mtime, true)
}

func commitPart(part, dst, expectHash string, size int64, mode fs.FileMode, mtime time.Time, sync bool) (FileResult, error) {
	res := FileResult{Src: part, Hash: expectHash, Bytes: size}
	got, n, err := HashFile(part)
	if err != nil {
		return res, err
	}
	if got != expectHash || n != size {
		return res, i18n.Errorf("%s : %w", dst, ErrVerifyFailed)
	}
	if err := os.Chmod(part, mode.Perm()); err != nil {
		return res, err
	}
	if err := os.Chtimes(part, time.Now(), mtime); err != nil {
		return res, err
	}
	final, status, err := place(part, dst, expectHash, size)
	if err != nil {
		return res, err
	}
	if status == StatusAlreadyPresent {
		os.Remove(part)
	}
	if sync {
		if err := syncDir(filepath.Dir(final)); err != nil {
			return res, err
		}
	}
	res.Dst, res.Status = final, status
	return res, nil
}

// EnsureDir crée le dossier s'il manque et indique s'il a été créé (pour
// que l'annulation ne supprime que ce que Bernard a créé).
func EnsureDir(dst string, perm fs.FileMode) (bool, error) {
	fi, err := os.Lstat(dst)
	switch {
	case err == nil && fi.IsDir():
		return false, nil
	case err == nil:
		return false, i18n.Errorf("%s existe déjà et n'est pas un dossier", dst)
	case errors.Is(err, os.ErrNotExist):
		return true, os.Mkdir(dst, perm|0o700)
	default:
		return false, err
	}
}

// PlaceSymlink crée un lien symbolique vers target, sans écraser : un lien
// identique déjà présent est accepté, sinon un nom de conflit est utilisé.
func PlaceSymlink(target, dst string) (FileResult, error) {
	res := FileResult{Hash: "symlink:" + target}
	for i := 0; i <= maxConflicts; i++ {
		cand := conflictName(dst, i)
		if existing, err := os.Readlink(cand); err == nil && existing == target {
			res.Dst, res.Status = cand, StatusAlreadyPresent
			return res, nil
		}
		err := os.Symlink(target, cand)
		if err == nil {
			res.Dst, res.Status = cand, StatusCopied
			if i > 0 {
				res.Status = StatusRenamed
			}
			return res, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return res, err
		}
	}
	return res, i18n.Errorf("%s : %w", dst, ErrTooManyNames)
}

// WhiteoutHash marque dans le journal un fichier « effacé » d'overlay.
const WhiteoutHash = "whiteout"

// PlaceWhiteout crée un fichier « effacé » d'overlay (périphérique caractère
// 0:0), sans rien écraser.
func PlaceWhiteout(dst string) (FileResult, error) {
	res := FileResult{Hash: WhiteoutHash, Dst: dst}
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeCharDevice != 0 {
			res.Status = StatusAlreadyPresent
			return res, nil
		}
		return res, i18n.Errorf("%s existe déjà", dst)
	}
	if err := syscall.Mknod(dst, syscall.S_IFCHR|0o600, 0); err != nil {
		return res, err
	}
	res.Status = StatusCopied
	return res, nil
}
