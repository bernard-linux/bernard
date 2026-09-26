package transfer

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// TreeOptions paramètre CopyTree.
type TreeOptions struct {
	// Exclude reçoit le chemin relatif (séparateurs '/') et renvoie vrai pour
	// ignorer l'élément (et tout son contenu si c'est un dossier).
	Exclude func(rel string) bool
	// OnFile est appelé après chaque fichier traité (progression, journal).
	OnFile func(FileResult)
}

// FileError associe une erreur au chemin concerné.
type FileError struct {
	Path string `json:"path"`
	Err  string `json:"error"`
}

// TreeReport résume une copie d'arborescence. Il alimente le rapport final :
// tout ce qui n'a pas été copié y figure explicitement.
type TreeReport struct {
	Files          int64        `json:"files"`          // fichiers et liens copiés et vérifiés
	Bytes          int64        `json:"bytes"`          // octets vérifiés
	AlreadyPresent int64        `json:"alreadyPresent"` // identiques déjà sur la cible
	Renamed        []FileResult `json:"renamed,omitempty"`
	Skipped        []string     `json:"skipped,omitempty"` // fichiers spéciaux non migrables
	Excluded       []string     `json:"excluded,omitempty"`
	Errors         []FileError  `json:"errors,omitempty"`
}

// OK indique qu'aucune erreur n'a été rencontrée.
func (r *TreeReport) OK() bool { return len(r.Errors) == 0 }

// CopyTree copie srcRoot dans dstRoot. Une erreur sur un fichier n'arrête pas
// la copie : elle est consignée et le reste continue. Les liens symboliques
// sont recréés tels quels, jamais suivis.
func CopyTree(srcRoot, dstRoot string, opt TreeOptions) (*TreeReport, error) {
	rep := &TreeReport{}
	info, err := os.Lstat(srcRoot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s n'est pas un dossier", srcRoot)
	}
	if err := os.MkdirAll(dstRoot, info.Mode().Perm()|0o700); err != nil {
		return nil, err
	}
	fail := func(path string, err error) {
		rep.Errors = append(rep.Errors, FileError{Path: path, Err: err.Error()})
	}

	err = filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			fail(path, walkErr)
			if d != nil && d.IsDir() && path != srcRoot {
				return fs.SkipDir
			}
			return nil
		}
		if path == srcRoot {
			return nil
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			fail(path, err)
			return nil
		}
		if opt.Exclude != nil && opt.Exclude(filepath.ToSlash(rel)) {
			rep.Excluded = append(rep.Excluded, rel)
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		dst := filepath.Join(dstRoot, rel)

		switch t := d.Type(); {
		case d.IsDir():
			di, err := d.Info()
			if err != nil {
				fail(path, err)
				return fs.SkipDir
			}
			if _, err := EnsureDir(dst, di.Mode().Perm()); err != nil {
				fail(path, err)
				return fs.SkipDir
			}
		case t.IsRegular():
			res, err := CopyFile(path, dst)
			if err != nil {
				fail(path, err)
				return nil
			}
			record(rep, res, opt)
		case t&fs.ModeSymlink != 0:
			res, err := copySymlink(path, dst)
			if err != nil {
				fail(path, err)
				return nil
			}
			record(rep, res, opt)
		default:
			rep.Skipped = append(rep.Skipped, rel)
		}
		return nil
	})
	return rep, err
}

func record(rep *TreeReport, res FileResult, opt TreeOptions) {
	switch res.Status {
	case StatusAlreadyPresent:
		rep.AlreadyPresent++
	case StatusRenamed:
		rep.Renamed = append(rep.Renamed, res)
		rep.Files++
		rep.Bytes += res.Bytes
	default:
		rep.Files++
		rep.Bytes += res.Bytes
	}
	if opt.OnFile != nil {
		opt.OnFile(res)
	}
}

// copySymlink recrée un lien symbolique avec la même cible.
func copySymlink(src, dst string) (FileResult, error) {
	target, err := os.Readlink(src)
	if err != nil {
		return FileResult{Src: src}, err
	}
	res, err := PlaceSymlink(target, dst)
	res.Src = src
	return res, err
}
