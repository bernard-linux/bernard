package transfer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Owner est le propriétaire final des fichiers écrits en tant que root
// (UID et GID du compte de destination).
type Owner struct {
	UID, GID int
}

// Apply attribue path à son propriétaire, sans suivre de lien symbolique.
// Sans propriétaire (mode non administrateur), ne fait rien.
func (o *Owner) Apply(path string) error {
	if o == nil {
		return nil
	}
	return os.Lchown(path, o.UID, o.GID)
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
