// Package source définit ce qu'une source de migration expose (agent réseau
// ou paquet sur disque externe) et fournit le parcours sûr des fichiers.
//
// Sécurité : un fichier n'est servi que s'il se trouve réellement sous la
// racine d'un jeu de données de l'inventaire. Les chemins remontants (« .. »),
// absolus ou passant par un lien symbolique qui sort de la racine sont refusés.
package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bernard-linux/bernard/internal/inventory"
)

// Types d'entrées.
const (
	KindDir     = "dir"
	KindFile    = "file"
	KindSymlink = "symlink"
	KindOther   = "other" // socket, tube, périphérique : non migrable
	// KindUnreadable : la source n'a pas pu lire l'élément ; le message
	// d'erreur est placé dans Entry.Link.
	KindUnreadable = "unreadable"
)

// Entry décrit un élément d'un jeu de données.
type Entry struct {
	Rel   string      `json:"rel"` // chemin relatif, séparateurs '/'
	Kind  string      `json:"kind"`
	Size  int64       `json:"size,omitempty"`
	Mode  fs.FileMode `json:"mode"`
	MTime time.Time   `json:"mtime"`
	Link  string      `json:"link,omitempty"` // cible d'un lien symbolique
	// User et Group : propriétaire sur la source, par nom (les numéros
	// diffèrent d'une machine à l'autre). Servent hors des dossiers
	// personnels, où chaque fichier garde son propriétaire (mysql, www-data…).
	User  string `json:"user,omitempty"`
	Group string `json:"group,omitempty"`
}

// FileStream est le contenu d'un fichier à partir d'un décalage. Après avoir
// tout lu, Finish renvoie l'empreinte du fichier ENTIER telle que calculée par
// la source ; la cible la compare à ce qu'elle a écrit.
type FileStream interface {
	io.Reader
	Info() Entry
	Finish() (hash string, err error)
	Close() error
}

// Source est ce que le moteur de la cible consomme : un agent réseau ou un
// paquet sur disque externe.
type Source interface {
	Inventory(ctx context.Context) (*inventory.Inventory, error)
	List(ctx context.Context, dataset string, fn func(Entry) error) error
	Get(ctx context.Context, dataset, rel string, offset int64) (FileStream, error)
	Close() error
}

// ErrOutsideRoot signale une tentative d'accès hors d'un jeu de données.
var ErrOutsideRoot = errors.New("chemin refusé : hors des dossiers à migrer")

// Excluded indique si rel (séparateurs '/') correspond à un motif d'exclusion.
// Un motif exclut aussi tout ce qui se trouve dessous.
func Excluded(rel string, patterns []string) bool {
	parts := strings.Split(rel, "/")
	for _, p := range patterns {
		pp := strings.Split(p, "/")
		if len(parts) < len(pp) {
			continue
		}
		if ok, _ := filepath.Match(p, strings.Join(parts[:len(pp)], "/")); ok {
			return true
		}
	}
	return false
}

// Walk parcourt root sans suivre les liens symboliques, en appliquant les
// exclusions. Les erreurs de lecture sont transmises à onErr et n'arrêtent pas
// le parcours.
func Walk(root string, excludes []string, fn func(Entry) error, onErr func(rel string, err error)) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if err != nil {
			if onErr != nil {
				onErr(rel, err)
			}
			if d != nil && d.IsDir() && path != root {
				return fs.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		if Excluded(rel, excludes) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if onErr != nil {
				onErr(rel, err)
			}
			return nil
		}
		e := Entry{Rel: rel, Mode: info.Mode(), MTime: info.ModTime()}
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			e.User, e.Group = ownerNames(st.Uid, st.Gid)
		}
		switch {
		case d.IsDir():
			e.Kind = KindDir
		case info.Mode().IsRegular():
			e.Kind, e.Size = KindFile, info.Size()
		case info.Mode()&fs.ModeSymlink != 0:
			e.Kind = KindSymlink
			if e.Link, err = os.Readlink(path); err != nil {
				if onErr != nil {
					onErr(rel, err)
				}
				return nil
			}
		default:
			e.Kind = KindOther
		}
		return fn(e)
	})
}

var (
	namesMu sync.Mutex
	users   = map[uint32]string{}
	groups  = map[uint32]string{}
)

// ownerNames traduit UID et GID en noms (mis en cache).
func ownerNames(uid, gid uint32) (string, string) {
	namesMu.Lock()
	defer namesMu.Unlock()
	u, ok := users[uid]
	if !ok {
		if x, err := user.LookupId(strconv.Itoa(int(uid))); err == nil {
			u = x.Username
		}
		users[uid] = u
	}
	g, ok := groups[gid]
	if !ok {
		if x, err := user.LookupGroupId(strconv.Itoa(int(gid))); err == nil {
			g = x.Name
		}
		groups[gid] = g
	}
	return u, g
}

// Includer limite un jeu de données à une liste de chemins relatifs (et aux
// dossiers qui les contiennent). Liste vide : tout est inclus.
type Includer struct {
	files map[string]bool
	dirs  map[string]bool
}

// NewIncluder prépare le filtre.
func NewIncluder(include []string) *Includer {
	if len(include) == 0 {
		return nil
	}
	in := &Includer{files: map[string]bool{}, dirs: map[string]bool{}}
	for _, p := range include {
		p = strings.Trim(p, "/")
		in.files[p] = true
		for d := path.Dir(p); d != "." && d != "/"; d = path.Dir(d) {
			in.dirs[d] = true
		}
	}
	return in
}

// Dir indique qu'un dossier mène à un chemin inclus.
func (in *Includer) Dir(rel string) bool { return in == nil || in.dirs[rel] || in.files[rel] }

// File indique qu'un fichier est inclus.
func (in *Includer) File(rel string) bool { return in == nil || in.files[rel] }

// WalkDataSet parcourt un jeu de données : exclusions, puis liste
// d'inclusion éventuelle.
func WalkDataSet(root string, excludes, include []string, fn func(Entry) error, onErr func(rel string, err error)) error {
	in := NewIncluder(include)
	return Walk(root, excludes, func(e Entry) error {
		if e.Kind == KindDir && !in.Dir(e.Rel) || e.Kind != KindDir && !in.File(e.Rel) {
			return nil
		}
		return fn(e)
	}, onErr)
}

// SafeJoin résout rel sous root en refusant toute sortie de la racine,
// y compris par un dossier intermédiaire qui serait un lien symbolique.
func SafeJoin(root, rel string) (string, error) {
	native := filepath.FromSlash(rel)
	if rel == "" || !filepath.IsLocal(native) {
		return "", ErrOutsideRoot
	}
	full := filepath.Join(root, native)
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	realParent, err := filepath.EvalSymlinks(filepath.Dir(full))
	if err != nil {
		return "", err
	}
	if realParent != realRoot && !strings.HasPrefix(realParent, realRoot+string(filepath.Separator)) {
		return "", ErrOutsideRoot
	}
	return filepath.Join(realParent, filepath.Base(full)), nil
}

// OpenRegular ouvre en lecture seule un fichier ordinaire sous root, sans
// suivre de lien symbolique, et vérifie qu'il n'a pas été remplacé entre la
// vérification et l'ouverture.
func OpenRegular(root, rel string) (*os.File, os.FileInfo, error) {
	full, err := SafeJoin(root, rel)
	if err != nil {
		return nil, nil, err
	}
	before, err := os.Lstat(full)
	if err != nil {
		return nil, nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%s : pas un fichier ordinaire", rel)
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, nil, err
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		f.Close()
		return nil, nil, fmt.Errorf("%s : fichier remplacé pendant l'ouverture", rel)
	}
	return f, after, nil
}

// FileError est une erreur limitée à un élément : la migration continue avec
// les suivants et l'élément figure au rapport. Toute autre erreur venant d'une
// source (coupure réseau, disque débranché) interrompt la session, qui pourra
// reprendre.
type FileError struct{ Rel, Msg string }

func (e *FileError) Error() string {
	if e.Rel == "" {
		return "source : " + e.Msg
	}
	return "source : " + e.Rel + " : " + e.Msg
}
