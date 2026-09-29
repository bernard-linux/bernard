// Package transfer copie des fichiers en garantissant qu'aucune donnée n'est
// perdue ni écrasée.
//
// Garanties de CopyFile :
//   - la source est ouverte en lecture seule et n'est jamais modifiée ;
//   - le contenu est écrit dans un fichier temporaire, synchronisé sur disque
//     (fsync), puis relu et comparé à l'empreinte BLAKE3 de la source ;
//   - le fichier n'apparaît sous son nom final qu'une fois vérifié ;
//   - un fichier existant sur la cible n'est jamais remplacé : s'il est
//     identique, la copie est considérée comme déjà faite ; sinon le nouveau
//     fichier reçoit un nom de conflit (« nom (bernard 1).ext »).
package transfer

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zeebo/blake3"

	"github.com/bernard-linux/bernard/internal/i18n"
)

// Erreurs signalées dans le rapport.
var (
	ErrNotRegular    = i18n.NewError("pas un fichier ordinaire")
	ErrVerifyFailed  = i18n.NewError("vérification échouée : le contenu écrit diffère de la source")
	ErrSourceChanged = i18n.NewError("fichier source modifié pendant la copie")
	ErrTooManyNames  = i18n.NewError("trop de conflits de noms")
)

// Status décrit l'issue d'une copie réussie.
type Status string

const (
	StatusCopied         Status = "copied"         // copié sous le nom prévu
	StatusRenamed        Status = "renamed"        // copié sous un nom de conflit
	StatusAlreadyPresent Status = "alreadyPresent" // fichier identique déjà présent
)

// FileResult est le résultat vérifié d'une copie.
type FileResult struct {
	Src    string `json:"src"`
	Dst    string `json:"dst"` // chemin final réel
	Bytes  int64  `json:"bytes"`
	Hash   string `json:"hash"` // "blake3:<hex>"
	Status Status `json:"status"`
}

const (
	bufSize      = 1 << 20
	maxAttempts  = 3
	maxConflicts = 1000
)

// bufPool évite d'allouer 1 Mo par fichier (des milliers de petits fichiers).
var bufPool = sync.Pool{New: func() any { b := make([]byte, bufSize); return &b }}

// HashFile calcule l'empreinte BLAKE3 d'un fichier.
func HashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := blake3.New()
	buf := bufPool.Get().(*[]byte)
	defer bufPool.Put(buf)
	n, err := io.CopyBuffer(h, onlyReader{f}, *buf)
	if err != nil {
		return "", n, err
	}
	return "blake3:" + hex.EncodeToString(h.Sum(nil)), n, nil
}

// CopyFile copie src vers dst selon les garanties du package.
func CopyFile(src, dst string) (FileResult, error) {
	res := FileResult{Src: src}
	info, err := os.Lstat(src)
	if err != nil {
		return res, err
	}
	if !info.Mode().IsRegular() {
		return res, i18n.Errorf("%s : %w", src, ErrNotRegular)
	}

	var tmp, sum string
	for attempt := 1; ; attempt++ {
		tmp, sum, err = writeTemp(src, filepath.Dir(dst), info)
		if err == nil {
			break
		}
		if !errors.Is(err, ErrSourceChanged) || attempt == maxAttempts {
			return res, err
		}
		if info, err = os.Lstat(src); err != nil {
			return res, err
		}
	}
	res.Hash, res.Bytes = sum, info.Size()
	cleanup := func() { os.Remove(tmp) }

	// Relecture depuis le disque : ce qui est vérifié, c'est ce qui a été écrit.
	written, _, err := HashFile(tmp)
	if err != nil {
		cleanup()
		return res, err
	}
	if written != sum {
		cleanup()
		return res, i18n.Errorf("%s : %w", src, ErrVerifyFailed)
	}
	if err := os.Chmod(tmp, info.Mode().Perm()); err != nil {
		cleanup()
		return res, err
	}
	if err := os.Chtimes(tmp, time.Now(), info.ModTime()); err != nil {
		cleanup()
		return res, err
	}

	final, status, err := place(tmp, dst, sum, info.Size())
	if err != nil {
		cleanup()
		return res, err
	}
	if status == StatusAlreadyPresent {
		cleanup()
	}
	if err := syncDir(filepath.Dir(final)); err != nil {
		return res, err
	}
	res.Dst, res.Status = final, status
	return res, nil
}

// writeTemp copie src dans un fichier temporaire de dir en calculant
// l'empreinte au passage, puis synchronise le fichier sur disque.
func writeTemp(src, dir string, before os.FileInfo) (string, string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", "", err
	}
	defer in.Close()
	out, err := os.CreateTemp(dir, ".bernard-*.part")
	if err != nil {
		return "", "", err
	}
	tmp := out.Name()
	fail := func(e error) (string, string, error) {
		out.Close()
		os.Remove(tmp)
		return "", "", e
	}
	h := blake3.New()
	n, err := io.CopyBuffer(io.MultiWriter(out, h), in, make([]byte, bufSize))
	if err != nil {
		return fail(err)
	}
	if err := out.Sync(); err != nil {
		return fail(err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return "", "", err
	}
	after, err := os.Lstat(src)
	if err != nil {
		os.Remove(tmp)
		return "", "", err
	}
	if n != before.Size() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		os.Remove(tmp)
		return "", "", i18n.Errorf("%s : %w", src, ErrSourceChanged)
	}
	return tmp, "blake3:" + hex.EncodeToString(h.Sum(nil)), nil
}

// place donne au fichier temporaire son nom final sans jamais remplacer un
// fichier existant.
func place(tmp, dst, sum string, size int64) (string, Status, error) {
	for i := 0; i <= maxConflicts; i++ {
		cand := conflictName(dst, i)
		existing, err := os.Lstat(cand)
		if err == nil {
			// Un fichier identique déjà présent : rien à faire (reprise).
			if existing.Mode().IsRegular() && existing.Size() == size {
				if h, _, err := HashFile(cand); err == nil && h == sum {
					return cand, StatusAlreadyPresent, nil
				}
			}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", "", err
		}
		switch err := renameNoReplace(tmp, cand); {
		case err == nil:
			if i == 0 {
				return cand, StatusCopied, nil
			}
			return cand, StatusRenamed, nil
		case errors.Is(err, os.ErrExist):
			continue // apparu entre-temps : nom suivant
		default:
			return "", "", err
		}
	}
	return "", "", i18n.Errorf("%s : %w", dst, ErrTooManyNames)
}

// conflictName renvoie dst pour i = 0, sinon « nom (bernard i).ext ».
// Les fichiers cachés (« .bashrc ») gardent leur point initial.
func conflictName(dst string, i int) string {
	if i == 0 {
		return dst
	}
	dir, base := filepath.Split(dst)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" { // fichier caché sans extension, ex. ".bashrc"
		stem, ext = base, ""
	}
	return filepath.Join(dir, fmt.Sprintf("%s (bernard %d)%s", stem, i, ext))
}

// syncDir force l'écriture sur disque de l'entrée de répertoire.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil && !errors.Is(err, os.ErrInvalid) {
		return err
	}
	return nil
}

// onlyReader empêche io.CopyBuffer de contourner le tampon fourni.
type onlyReader struct{ r io.Reader }

func (o onlyReader) Read(p []byte) (int, error) { return o.r.Read(p) }
