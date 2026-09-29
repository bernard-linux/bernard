package apply

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/bernard-linux/bernard/internal/i18n"
	"github.com/bernard-linux/bernard/internal/journal"
	"github.com/bernard-linux/bernard/internal/transfer"
)

// PreferSourceRoots sont les dossiers, relatifs au dossier personnel, où la
// version de l'ancien ordinateur doit l'emporter sur celle, toute neuve, du
// compte créé à l'installation du nouveau :
//
//   - le trousseau de clés : il contient la clé qui déchiffre les mots de
//     passe enregistrés dans Brave, Chrome, Chromium, Edge… Sans lui, ces
//     mots de passe arrivent mais restent illisibles ;
//   - les fichiers d'index des profils Firefox et Thunderbird (profiles.ini,
//     installs.ini) : si Firefox a été ouvert une fois sur le nouvel
//     ordinateur, il continuerait sinon d'utiliser son profil vide ;
//   - les profils des navigateurs de la famille Chromium, pour la même
//     raison.
//
// La règle normale (ne jamais écraser, nommer la copie « (bernard 1) ») ne
// convient pas ici : les deux versions ne peuvent pas cohabiter.
var PreferSourceRoots = []string{
	".local/share/keyrings",
	".mozilla/firefox",
	".var/app/org.mozilla.firefox/.mozilla/firefox",
	"snap/firefox/common/.mozilla/firefox",
	".thunderbird",
	".var/app/org.mozilla.Thunderbird/.thunderbird",
	".config/BraveSoftware",
	".config/google-chrome",
	".config/chromium",
	".config/microsoft-edge",
	".config/vivaldi",
}

// AsideDir reçoit, dans le dossier personnel, les fichiers du nouvel
// ordinateur remplacés par ceux de l'ancien. Rien n'est supprimé.
const AsideDir = ".local/share/bernard/avant-migration"

// FreshInstallDays : en deçà, le système cible est considéré comme
// fraîchement installé et ses fichiers de profil comme vierges.
const FreshInstallDays = 45

// InstallAge renvoie l'âge du système : date de l'installateur (Ubuntu,
// Zorin, Mint) ou, à défaut, de l'identifiant de machine, créé une seule
// fois à l'installation.
func InstallAge(root string) (time.Duration, bool) {
	for _, p := range []string{"var/log/installer", "etc/machine-id"} {
		if fi, err := os.Stat(filepath.Join(root, p)); err == nil {
			return time.Since(fi.ModTime()), true
		}
	}
	return 0, false
}

// FreshInstall indique un système installé il y a peu.
func FreshInstall() bool {
	age, ok := InstallAge("/")
	return ok && age < FreshInstallDays*24*time.Hour
}

func underPreferRoot(rel string) bool {
	for _, r := range PreferSourceRoots {
		if rel == r || strings.HasPrefix(rel, r+"/") {
			return true
		}
	}
	return false
}

// PreferSource remplace, dans le dossier personnel home, les fichiers de
// profil déjà présents sur la cible par ceux de l'ancien ordinateur que la
// copie avait dû renommer (« login (bernard 1).keyring »). Le fichier de la
// cible est déplacé dans AsideDir, jamais supprimé. Chaque échange est
// journalisé avant d'être fait, pour l'annulation (UndoPreferSource).
//
// Seuls les fichiers dont le journal atteste que Bernard les a copiés sont
// concernés. L'opération peut être relancée : ce qui est déjà échangé est
// ignoré.
func PreferSource(home string, st *journal.State, j *journal.Journal) ([]string, error) {
	var done []string
	var keys []string
	for k := range st.Done {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		d := st.Done[k]
		if d.Status != string(transfer.StatusRenamed) {
			continue
		}
		_, rel, ok := strings.Cut(k, "/")
		if !ok || !underPreferRoot(rel) {
			continue
		}
		want := filepath.Join(home, rel)
		if filepath.Dir(d.Dst) != filepath.Dir(want) || !strings.HasPrefix(d.Dst, home+"/") {
			continue
		}
		if _, err := os.Lstat(d.Dst); err != nil {
			continue // déjà échangé, ou retiré depuis
		}
		aside := filepath.Join(home, AsideDir, rel)
		if _, err := os.Lstat(aside); err == nil {
			continue // une version de la cible est déjà mise de côté : on n'y touche plus
		}
		if err := j.Append(journal.Record{T: journal.RecSys, Op: journal.SysPreferSource, Name: d.Dst, Dst: want, Key: aside}); err != nil {
			return done, err
		}
		if err := swapIn(home, d.Dst, want, aside); err != nil {
			return done, err
		}
		done = append(done, rel)
	}
	return done, nil
}

// swapIn met current de côté (s'il existe) puis place incoming sous son nom.
func swapIn(home, incoming, current, aside string) error {
	_, err := os.Lstat(current)
	switch {
	case err == nil:
		if err := mkdirOwned(home, filepath.Dir(aside)); err != nil {
			return err
		}
		if err := os.Rename(current, aside); err != nil {
			return err
		}
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	return os.Rename(incoming, current)
}

// mkdirOwned crée dir (sous home) et ses parents manquants, avec le
// propriétaire du dossier personnel : Bernard tourne en administrateur.
func mkdirOwned(home, dir string) error {
	hi, err := os.Stat(home)
	if err != nil {
		return err
	}
	uid, gid := ownerOf(hi)
	rel, err := filepath.Rel(home, dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return errors.New(i18n.Tf("dossier hors du dossier personnel : %s", dir))
	}
	cur := home
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		if _, err := os.Lstat(cur); err == nil {
			continue
		}
		if err := os.Mkdir(cur, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		os.Lchown(cur, uid, gid)
	}
	return nil
}

// UndoPreferSource défait les échanges, dans l'ordre inverse : le fichier de
// l'ancien ordinateur reprend son nom de conflit (l'annulation des fichiers
// le retirera ensuite), celui de la cible revient à sa place.
func UndoPreferSource(st *journal.State) []string {
	var errs []string
	for i := len(st.Sys) - 1; i >= 0; i-- {
		r := st.Sys[i]
		if r.Op != journal.SysPreferSource {
			continue
		}
		if _, err := os.Lstat(r.Dst); err == nil {
			if _, err := os.Lstat(r.Name); errors.Is(err, os.ErrNotExist) {
				if err := os.Rename(r.Dst, r.Name); err != nil {
					errs = append(errs, i18n.Tf("%s : %v", r.Dst, err))
					continue
				}
			}
		}
		if _, err := os.Lstat(r.Key); err == nil {
			if err := os.Rename(r.Key, r.Dst); err != nil {
				errs = append(errs, i18n.Tf("%s : %v", r.Key, err))
			}
		}
	}
	return errs
}

func ownerOf(fi fs.FileInfo) (int, int) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid), int(st.Gid)
	}
	return -1, -1
}
