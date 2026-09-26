package linux

import (
	"io/fs"
	"path/filepath"

	"github.com/bernard-linux/bernard/internal/source"
)

// DefaultExcludes sont les chemins, relatifs au dossier personnel, exclus par
// défaut : caches et corbeilles, régénérables ou jetables. L'utilisateur
// pourra les réintégrer dans l'interface.
var DefaultExcludes = []string{
	".cache",
	".local/share/Trash",
	".thumbnails",
	".var/app/*/cache",
	"snap/*/*/.cache",
}

// excluded délègue au motif partagé avec le transfert.
func excluded(rel string, patterns []string) bool { return source.Excluded(rel, patterns) }

// measureStats résume un parcours de dossier.
type measureStats struct {
	Files      int64
	Bytes      int64
	Unreadable int64 // entrées illisibles (droits), signalées dans le rapport
}

// measure parcourt root sans suivre les liens symboliques et compte les
// fichiers réguliers et liens. Les fichiers spéciaux (sockets, FIFO,
// périphériques) sont ignorés : ils ne se migrent pas.
func measure(root string, patterns []string) (measureStats, error) {
	var st measureStats
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			st.Unreadable++
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if excluded(filepath.ToSlash(rel), patterns) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		switch {
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				st.Unreadable++
				return nil
			}
			st.Files++
			st.Bytes += info.Size()
		case d.Type()&fs.ModeSymlink != 0:
			st.Files++
		}
		return nil
	})
	return st, err
}
