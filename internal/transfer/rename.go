package transfer

import (
	"errors"
	"os"
	"syscall"
)

// renameNoReplace renomme oldpath en newpath en échouant avec os.ErrExist si
// newpath existe. Trois stratégies, de la plus sûre à la plus permissive :
//  1. renameat2(RENAME_NOREPLACE), atomique (Linux, amd64 et arm64) ;
//  2. lien dur puis suppression de l'ancien nom, atomique aussi ;
//  3. vérification d'absence puis renommage, pour les systèmes de fichiers
//     sans liens durs (FAT, exFAT des disques externes). La fenêtre de course
//     ne concerne que le dossier de destination, écrit par Bernard seul.
func renameNoReplace(oldpath, newpath string) error {
	err := renameat2NoReplace(oldpath, newpath)
	if err == nil || errors.Is(err, os.ErrExist) {
		return err
	}
	if !errors.Is(err, errUnsupported) {
		return err
	}

	err = os.Link(oldpath, newpath)
	if err == nil {
		return os.Remove(oldpath)
	}
	if errors.Is(err, os.ErrExist) {
		return os.ErrExist
	}
	if !linkUnsupported(err) {
		return err
	}

	if _, err := os.Lstat(newpath); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(oldpath, newpath)
}

var errUnsupported = errors.New("opération non prise en charge")

func linkUnsupported(err error) bool {
	var le *os.LinkError
	if errors.As(err, &le) {
		err = le.Err
	}
	return errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.ENOTSUP) ||
		errors.Is(err, syscall.EOPNOTSUPP) || errors.Is(err, syscall.EXDEV)
}
