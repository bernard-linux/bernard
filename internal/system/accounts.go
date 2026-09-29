package system

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bernard-linux/bernard/internal/i18n"
)

// Comptes provisoires.
//
// On installe souvent le nouvel ordinateur avec un compte provisoire
// (« tmp »), puis Bernard y crée le vrai compte. Une fois la migration
// vérifiée, Bernard propose de supprimer ce compte provisoire. Comme on ne
// peut pas supprimer le compte dont la session est ouverte, la suppression
// est programmée au prochain démarrage, avant l'écran de connexion.

// Account est un compte humain de la cible.
type Account struct {
	Login string `json:"login"`
	UID   int    `json:"uid"`
	Home  string `json:"home"`
	Admin bool   `json:"admin"`
	// Files et Bytes mesurent les fichiers personnels du dossier (hors
	// fichiers et dossiers cachés), pour que l'utilisateur sache ce qu'il
	// perdrait.
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
	// Current : c'est le compte dont la session a lancé Bernard.
	Current bool `json:"current,omitempty"`
	// Scheduled : suppression programmée au prochain démarrage.
	Scheduled bool `json:"scheduled,omitempty"`
}

// Racine et dossier des unités systemd (remplacés dans les tests).
var (
	Root     = "/"
	UnitDir  = "etc/systemd/system"
	adminGrp = map[string]bool{"sudo": true, "admin": true, "wheel": true}
)

func rootPath(p string) string { return filepath.Join(Root, p) }

// HumanAccounts lit les comptes humains (UID 1000 à 59999, avec un shell de
// connexion) de /etc/passwd, et note ceux qui sont administrateurs.
func HumanAccounts() ([]Account, error) {
	admins := map[string]bool{}
	if f, err := os.Open(rootPath("etc/group")); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			p := strings.Split(sc.Text(), ":")
			if len(p) >= 4 && adminGrp[p[0]] {
				for _, m := range strings.Split(p[3], ",") {
					if m != "" {
						admins[m] = true
					}
				}
			}
		}
		f.Close()
	}
	f, err := os.Open(rootPath("etc/passwd"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Account
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Split(sc.Text(), ":")
		if len(p) < 7 {
			continue
		}
		uid, err := strconv.Atoi(p[2])
		if err != nil || uid < 1000 || uid > 59999 || strings.HasSuffix(p[6], "nologin") || strings.HasSuffix(p[6], "false") {
			continue
		}
		out = append(out, Account{Login: p[0], UID: uid, Home: p[5], Admin: admins[p[0]]})
	}
	return out, nil
}

// MeasureHome compte les fichiers personnels visibles d'un dossier.
func MeasureHome(home string) (files, bytes int64) {
	filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path != home && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				files++
				bytes += fi.Size()
			}
		}
		return nil
	})
	return files, bytes
}

func unitName(login string) string { return "bernard-supprimer-compte-" + login + ".service" }

func unitPath(login string) string { return rootPath(filepath.Join(UnitDir, unitName(login))) }

// RemovalScheduled indique une suppression programmée pour ce compte.
func RemovalScheduled(login string) bool {
	_, err := os.Stat(unitPath(login))
	return err == nil
}

// ErrLastAdmin : supprimer ce compte ne laisserait aucun administrateur.
var ErrLastAdmin = i18n.NewError("aucun autre compte administrateur : ce compte ne peut pas être supprimé")

// CheckRemovable vérifie qu'un compte peut être supprimé : compte humain,
// et au moins un AUTRE administrateur restera (sinon plus personne ne
// pourrait installer de logiciel ni gérer l'ordinateur).
func CheckRemovable(login string) (Account, error) {
	if !loginRe.MatchString(login) {
		return Account{}, i18n.Errorf("%w : identifiant %q", ErrInvalid, login)
	}
	accs, err := HumanAccounts()
	if err != nil {
		return Account{}, err
	}
	var target *Account
	otherAdmin := false
	for i, a := range accs {
		if a.Login == login {
			target = &accs[i]
		} else if a.Admin {
			otherAdmin = true
		}
	}
	if target == nil {
		return Account{}, i18n.Errorf("%s n'est pas un compte utilisateur de cet ordinateur", login)
	}
	if !otherAdmin {
		return *target, ErrLastAdmin
	}
	return *target, nil
}

// ScheduleRemoval programme la suppression du compte (et de son dossier
// personnel) au prochain démarrage, avant l'écran de connexion.
func (s *System) ScheduleRemoval(ctx context.Context, login, bernardPath string) error {
	if _, err := CheckRemovable(login); err != nil {
		return err
	}
	unit := fmt.Sprintf(`# Écrit par Bernard : suppression du compte provisoire %[1]s,
# demandée à la fin de la migration. Ce fichier s'efface après usage.
[Unit]
Description=Bernard : suppression du compte provisoire %[1]s
After=local-fs.target
Before=display-manager.service systemd-user-sessions.service

[Service]
Type=oneshot
ExecStart=%[2]s remove-account %[1]s

[Install]
WantedBy=multi-user.target
`, login, bernardPath)
	if err := os.MkdirAll(filepath.Dir(unitPath(login)), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(unitPath(login), []byte(unit), 0o644); err != nil {
		return err
	}
	if _, err := s.run(ctx, "systemctl", "enable", unitName(login)); err != nil {
		os.Remove(unitPath(login))
		return err
	}
	return nil
}

// CancelRemoval annule une suppression programmée.
func (s *System) CancelRemoval(ctx context.Context, login string) error {
	if !loginRe.MatchString(login) {
		return i18n.Errorf("%w : identifiant %q", ErrInvalid, login)
	}
	if !RemovalScheduled(login) {
		return nil
	}
	s.run(ctx, "systemctl", "disable", unitName(login))
	return os.Remove(unitPath(login))
}

// RemoveAccountNow supprime le compte et son dossier personnel, puis retire
// l'unité qui l'a demandé. Appelée au démarrage par l'unité systemd ; refuse
// si une session du compte est ouverte ou s'il est le dernier administrateur.
func (s *System) RemoveAccountNow(ctx context.Context, login string) error {
	defer s.CancelRemoval(ctx, login) // une seule tentative, réussie ou non
	if _, err := CheckRemovable(login); err != nil {
		return err
	}
	if out, err := s.run(ctx, "loginctl", "list-sessions", "--no-legend"); err == nil {
		for _, l := range strings.Split(out, "\n") {
			if f := strings.Fields(l); len(f) >= 3 && f[2] == login {
				return i18n.Errorf("une session de %s est ouverte : suppression abandonnée", login)
			}
		}
	}
	_, err := s.run(ctx, "userdel", "--remove", "--", login)
	if err != nil && !s.UserExists(ctx, login) {
		return nil // compte supprimé ; simple avertissement (boîte aux lettres absente…)
	}
	return err
}
