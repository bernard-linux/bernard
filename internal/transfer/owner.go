package transfer

import (
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/bernard-linux/bernard/internal/i18n"
)

// Owner est le propriétaire final des fichiers écrits en tant que root
// (UID et GID du compte de destination).
type Owner struct {
	UID, GID int
	// Mode, s'il porte des bits spéciaux (setuid, setgid, sticky), est
	// réappliqué après le changement de propriétaire, qui les efface.
	Mode fs.FileMode
	// Xattrs : attributs étendus à poser après le propriétaire (le
	// changement de propriétaire efface les capacités), valeurs en base64.
	Xattrs map[string]string
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
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&fs.ModeSymlink != 0 {
		return nil
	}
	if o.Mode&specialBits != 0 {
		if err := os.Chmod(path, o.Mode&(fs.ModePerm|specialBits)); err != nil {
			return err
		}
	}
	for name, b64 := range o.Xattrs {
		val, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			continue
		}
		if err := syscall.Setxattr(path, name, val, 0); err != nil && !errors.Is(err, syscall.ENOTSUP) {
			return i18n.Errorf("attribut %s de %s : %w", name, path, err)
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
		return i18n.Errorf("%s est hors de %s", path, root)
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
			return i18n.Errorf("%s n'est pas un dossier ordinaire : écriture refusée", cur)
		}
	}
	return nil
}
