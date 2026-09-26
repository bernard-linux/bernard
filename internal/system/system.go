// Package system regroupe les SEULES opérations qui modifient le système de
// la cible : comptes, paquets apt, Flatpak. Liste fermée, paramètres validés
// par expressions régulières strictes, aucune commande construite par
// concaténation de texte, secrets passés par l'entrée standard.
package system

import (
	"context"
	"errors"
	"fmt"
	"os/user"
	"regexp"
	"strconv"
	"strings"

	"github.com/bernard-linux/bernard/internal/sysexec"
)

var (
	loginRe   = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	aptRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]+$`)
	flatpakRe = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+){2,}$`)
	hashRe    = regexp.MustCompile(`^\$[0-9a-z]+\$[./A-Za-z0-9$=,-]+$`)
)

// ErrInvalid signale un paramètre refusé par la validation.
var ErrInvalid = errors.New("paramètre refusé")

// AllowedGroups sont les groupes secondaires repris de la source, s'ils
// existent sur la cible. Les autres (groupes de services, groupes créés par
// des logiciels absents) sont ignorés.
var AllowedGroups = map[string]bool{
	"sudo": true, "adm": true, "lpadmin": true, "plugdev": true, "dialout": true,
	"cdrom": true, "audio": true, "video": true, "netdev": true, "bluetooth": true,
	"scanner": true, "sambashare": true, "docker": true, "libvirt": true, "kvm": true,
	"vboxusers": true, "wireshark": true, "users": true,
}

// FlathubURL est l'adresse officielle du dépôt Flathub.
const FlathubURL = "https://dl.flathub.org/repo/flathub.flatpakrepo"

// System exécute les opérations. Exec est remplacé dans les tests.
type System struct {
	Exec sysexec.Executor
}

// New renvoie le System réel.
func New() *System { return &System{Exec: sysexec.Run} }

func (s *System) run(ctx context.Context, name string, args ...string) (string, error) {
	return s.Exec(ctx, sysexec.Cmd{Name: name, Args: args})
}

// UserSpec décrit un compte à créer.
type UserSpec struct {
	Login        string
	FullName     string
	UID          int      // repris si libre sur la cible, sinon attribué
	Groups       []string // groupes de la source
	PasswordHash string   // Linux → Linux : hachage repris tel quel
	Password     string   // sinon : mot de passe saisi par l'utilisateur
}

// UserExists indique si un compte existe.
func (s *System) UserExists(ctx context.Context, login string) bool {
	_, err := s.run(ctx, "getent", "passwd", login)
	return err == nil
}

func (s *System) groupExists(ctx context.Context, g string) bool {
	_, err := s.run(ctx, "getent", "group", g)
	return err == nil
}

func (s *System) uidFree(ctx context.Context, uid int) bool {
	_, err := s.run(ctx, "getent", "passwd", strconv.Itoa(uid))
	return err != nil
}

// CreateUser crée le compte, son dossier personnel, ses groupes et son mot
// de passe. Renvoie les groupes effectivement attribués.
func (s *System) CreateUser(ctx context.Context, u UserSpec) ([]string, error) {
	if !loginRe.MatchString(u.Login) {
		return nil, fmt.Errorf("%w : identifiant %q", ErrInvalid, u.Login)
	}
	if s.UserExists(ctx, u.Login) {
		return nil, fmt.Errorf("le compte %s existe déjà", u.Login)
	}
	if u.PasswordHash == "" && u.Password == "" {
		return nil, fmt.Errorf("%w : aucun mot de passe pour %s", ErrInvalid, u.Login)
	}
	if u.PasswordHash != "" && !hashRe.MatchString(u.PasswordHash) {
		return nil, fmt.Errorf("%w : hachage de mot de passe", ErrInvalid)
	}
	if strings.ContainsAny(u.Password, "\n\r") {
		return nil, fmt.Errorf("%w : mot de passe", ErrInvalid)
	}
	gecos := strings.Map(func(r rune) rune {
		if r == ':' || r == ',' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, u.FullName)

	args := []string{"--create-home", "--shell", "/bin/bash"}
	if gecos != "" {
		args = append(args, "--comment", gecos)
	}
	if u.UID >= 1000 && u.UID < 60000 && s.uidFree(ctx, u.UID) {
		args = append(args, "--uid", strconv.Itoa(u.UID))
	}
	args = append(args, "--", u.Login)
	if _, err := s.run(ctx, "useradd", args...); err != nil {
		return nil, err
	}

	var groups []string
	for _, g := range u.Groups {
		if AllowedGroups[g] && s.groupExists(ctx, g) {
			groups = append(groups, g)
		}
	}
	if len(groups) > 0 {
		if _, err := s.run(ctx, "usermod", "--append", "--groups", strings.Join(groups, ","), "--", u.Login); err != nil {
			return groups, err
		}
	}

	c := sysexec.Cmd{Name: "chpasswd"}
	if u.PasswordHash != "" {
		c.Args, c.Stdin = []string{"--encrypted"}, u.Login+":"+u.PasswordHash+"\n"
	} else {
		c.Stdin = u.Login + ":" + u.Password + "\n"
	}
	if _, err := s.Exec(ctx, c); err != nil {
		return groups, fmt.Errorf("mot de passe de %s non défini : %w", u.Login, err)
	}
	return groups, nil
}

// DeleteUser supprime un compte créé par Bernard. Le dossier personnel est
// conservé : il peut contenir des fichiers ajoutés depuis.
func (s *System) DeleteUser(ctx context.Context, login string) error {
	if !loginRe.MatchString(login) {
		return fmt.Errorf("%w : identifiant %q", ErrInvalid, login)
	}
	_, err := s.run(ctx, "userdel", "--", login)
	return err
}

// Owner renvoie l'UID, le GID et le dossier personnel d'un compte.
func Owner(login string) (uid, gid int, home string, err error) {
	u, err := user.Lookup(login)
	if err != nil {
		return 0, 0, "", err
	}
	uid, _ = strconv.Atoi(u.Uid)
	gid, _ = strconv.Atoi(u.Gid)
	return uid, gid, u.HomeDir, nil
}

var aptEnv = []string{"DEBIAN_FRONTEND=noninteractive", "APT_LISTCHANGES_FRONTEND=none"}

func (s *System) apt(ctx context.Context, args ...string) error {
	_, err := s.Exec(ctx, sysexec.Cmd{Name: "apt-get", Args: args, Env: aptEnv})
	return err
}

func (s *System) dpkgInstalled(ctx context.Context) map[string]bool {
	out, err := s.run(ctx, "dpkg-query", "-W", "-f", "${Package}\t${db:Status-Abbrev}\n")
	set := map[string]bool{}
	if err != nil {
		return set
	}
	for _, l := range sysexec.Lines(out) {
		name, st, _ := strings.Cut(l, "\t")
		if strings.HasPrefix(st, "ii") {
			set[name] = true
		}
	}
	return set
}

// AptUpdate rafraîchit la liste des paquets.
func (s *System) AptUpdate(ctx context.Context) error { return s.apt(ctx, "update") }

// AptInstall installe des paquets. Elle essaie d'abord en une fois, puis un
// par un si un paquet bloque, pour isoler les échecs. Elle renvoie les
// paquets réellement ajoutés par Bernard (absents avant), pour l'annulation.
func (s *System) AptInstall(ctx context.Context, pkgs []string) (added []string, failed map[string]error) {
	failed = map[string]error{}
	var valid []string
	for _, p := range pkgs {
		if !aptRe.MatchString(p) {
			failed[p] = ErrInvalid
			continue
		}
		valid = append(valid, p)
	}
	if len(valid) == 0 {
		return nil, failed
	}
	before := s.dpkgInstalled(ctx)
	args := append([]string{"install", "--yes", "--"}, valid...)
	if err := s.apt(ctx, args...); err != nil {
		for _, p := range valid {
			if before[p] {
				continue
			}
			if err := s.apt(ctx, "install", "--yes", "--", p); err != nil {
				failed[p] = err
			}
		}
	}
	after := s.dpkgInstalled(ctx)
	for _, p := range valid {
		switch {
		case after[p] && !before[p]:
			added = append(added, p)
		case !after[p] && failed[p] == nil:
			failed[p] = errors.New("non installé")
		}
	}
	return added, failed
}

// AptRemove désinstalle des paquets ajoutés par Bernard.
func (s *System) AptRemove(ctx context.Context, pkgs []string) error {
	var valid []string
	for _, p := range pkgs {
		if aptRe.MatchString(p) {
			valid = append(valid, p)
		}
	}
	if len(valid) == 0 {
		return nil
	}
	return s.apt(ctx, append([]string{"remove", "--yes", "--"}, valid...)...)
}

// FlatpakSetup installe Flatpak si besoin et ajoute Flathub pour tout le
// système. Renvoie ce qui a été ajouté, pour l'annulation.
func (s *System) FlatpakSetup(ctx context.Context) (addedFlatpak, addedRemote bool, err error) {
	if _, err := s.run(ctx, "flatpak", "--version"); err != nil {
		added, failed := s.AptInstall(ctx, []string{"flatpak"})
		if f := failed["flatpak"]; f != nil {
			return false, false, fmt.Errorf("installation de Flatpak : %w", f)
		}
		addedFlatpak = len(added) > 0
	}
	out, _ := s.run(ctx, "flatpak", "remotes", "--system", "--columns=name")
	for _, l := range sysexec.Lines(out) {
		if l == "flathub" {
			return addedFlatpak, false, nil
		}
	}
	if _, err := s.run(ctx, "flatpak", "remote-add", "--system", "--if-not-exists", "flathub", FlathubURL); err != nil {
		return addedFlatpak, false, err
	}
	return addedFlatpak, true, nil
}

// FlatpakInstall installe une application Flathub pour tout le système.
func (s *System) FlatpakInstall(ctx context.Context, id string) error {
	if !flatpakRe.MatchString(id) {
		return fmt.Errorf("%w : identifiant Flatpak %q", ErrInvalid, id)
	}
	_, err := s.run(ctx, "flatpak", "install", "--system", "--noninteractive", "--assumeyes", "flathub", id)
	return err
}

// FlatpakUninstall retire une application installée par Bernard.
func (s *System) FlatpakUninstall(ctx context.Context, id string) error {
	if !flatpakRe.MatchString(id) {
		return fmt.Errorf("%w : identifiant Flatpak %q", ErrInvalid, id)
	}
	_, err := s.run(ctx, "flatpak", "uninstall", "--system", "--noninteractive", "--assumeyes", id)
	return err
}

// FlatpakRemoveRemote retire Flathub s'il a été ajouté par Bernard.
func (s *System) FlatpakRemoveRemote(ctx context.Context) error {
	_, err := s.run(ctx, "flatpak", "remote-delete", "--system", "flathub")
	return err
}
