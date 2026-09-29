package transfer

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Owner est le propriétaire final des fichiers écrits en tant que root
// (UID et GID du compte de destination).
type Owner struct {
	UID, GID int
	// Mode, s'il porte des bits spéciaux (setuid, setgid, sticky), est
	// réappliqué après le changement de propriétaire, qui les efface.
	Mode fs.FileMode
}

const specialBits = fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky

// Apply attribue path à son propriétaire, sans suivre de lien symbolique.
// Sans propriétaire (mode non administrateur), ne fait rien.
func (o *Owner) Apply(path string) error {
	if o == nil {
		return nil
	}
	if err := os.Lchown(path, o.UID, o.GID); err != nil {
		return err
	}
	if o.Mode&specialBits != 0 {
		if fi, err := os.Lstat(path); err == nil && fi.Mode()&fs.ModeSymlink == 0 {
			return os.Chmod(path, o.Mode&(fs.ModePerm|specialBits))
		}
	}
	return nil
}

// SafeParents vérifie que chaque dossier entre root (inclus) et le parent de
// path est un vrai dossier et non un lien symbolique. En écrivant en root dans
// le dossier d'un utilisateur, cela empêche qu'un lien piégé (ex. Documents →
// /etc) détourne l'écriture hors du dossier personnel.
func SafeParents(root, path string) error {
	rel, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s est hors de %s", path, root)
	}
	cur := root
	parts := []string{""}
	if rel != "." {
		parts = append(parts, strings.Split(rel, string(filepath.Separator))...)
	}
	for _, p := range parts {
		if p != "" {
			cur = filepath.Join(cur, p)
		}
		fi, err := os.Lstat(cur)
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
			return fmt.Errorf("%s n'est pas un dossier ordinaire : écriture refusée", cur)
		}
	}
	return nil
}
