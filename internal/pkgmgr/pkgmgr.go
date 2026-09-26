// Package pkgmgr abstrait les gestionnaires de paquets de la machine cible.
//
// Le moteur ne parle jamais directement à apt ou à Flatpak : il passe par
// l'interface PackageManager. Ajouter dnf ou pacman (V2) revient à écrire une
// nouvelle implémentation, sans toucher au moteur.
//
// V1.0, étape 2 : consultation seule. L'installation sera ajoutée avec
// l'assistant privilégié (étape 4).
package pkgmgr

import (
	"context"
	"errors"
	"strings"

	"github.com/bernard-linux/bernard/internal/sysexec"
)

// PackageManager consulte un gestionnaire de paquets.
type PackageManager interface {
	// Name renvoie "apt", "flatpak", "snap"…
	Name() string
	// Ready indique si le gestionnaire est utilisable sur la cible.
	Ready(ctx context.Context) bool
	// Installed renvoie l'ensemble des paquets installés.
	Installed(ctx context.Context) (map[string]bool, error)
	// Available indique si un paquet peut être installé depuis les sources
	// configurées.
	Available(ctx context.Context, pkg string) (bool, error)
}

// Apt est l'implémentation Debian, Ubuntu, Zorin et Mint.
type Apt struct {
	Run       sysexec.Runner
	available map[string]bool
}

func (a *Apt) Name() string { return "apt" }

func (a *Apt) Ready(ctx context.Context) bool {
	_, err := a.Run(ctx, "dpkg-query", "--version")
	return err == nil
}

func (a *Apt) Installed(ctx context.Context) (map[string]bool, error) {
	out, err := a.Run(ctx, "dpkg-query", "-W", "-f", "${Package}\t${db:Status-Abbrev}\n")
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, l := range sysexec.Lines(out) {
		name, status, _ := strings.Cut(l, "\t")
		if strings.HasPrefix(status, "ii") {
			set[name] = true
		}
	}
	return set, nil
}

func (a *Apt) Available(ctx context.Context, pkg string) (bool, error) {
	if a.available == nil {
		out, err := a.Run(ctx, "apt-cache", "pkgnames")
		if err != nil {
			return false, err
		}
		a.available = map[string]bool{}
		for _, l := range sysexec.Lines(out) {
			a.available[l] = true
		}
	}
	return a.available[pkg], nil
}

// Flatpak gère les applications Flathub.
type Flatpak struct {
	Run sysexec.Runner
}

func (f *Flatpak) Name() string { return "flatpak" }

// Ready est vrai si Flatpak est installé ET que le dépôt Flathub est configuré.
func (f *Flatpak) Ready(ctx context.Context) bool {
	out, err := f.Run(ctx, "flatpak", "remotes", "--columns=name")
	if err != nil {
		return false
	}
	for _, l := range sysexec.Lines(out) {
		if l == "flathub" {
			return true
		}
	}
	return false
}

func (f *Flatpak) Installed(ctx context.Context) (map[string]bool, error) {
	out, err := f.Run(ctx, "flatpak", "list", "--app", "--columns=application")
	if errors.Is(err, sysexec.ErrMissingCommand) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, l := range sysexec.Lines(out) {
		set[l] = true
	}
	return set, nil
}

// Available suppose Flathub joignable une fois configuré ; la disponibilité
// réelle est vérifiée à l'installation, et un échec est reporté au rapport.
func (f *Flatpak) Available(_ context.Context, pkg string) (bool, error) {
	return pkg != "", nil
}

// Snap consulte les Snap installés sur la cible (Ubuntu, Zorin).
type Snap struct {
	Run sysexec.Runner
}

func (s *Snap) Name() string { return "snap" }

func (s *Snap) Ready(ctx context.Context) bool {
	_, err := s.Run(ctx, "snap", "version")
	return err == nil
}

func (s *Snap) Installed(ctx context.Context) (map[string]bool, error) {
	out, err := s.Run(ctx, "snap", "list")
	if errors.Is(err, sysexec.ErrMissingCommand) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for i, l := range sysexec.Lines(out) {
		if i == 0 {
			continue
		}
		if f := strings.Fields(l); len(f) > 0 {
			set[f[0]] = true
		}
	}
	return set, nil
}

// Available est toujours faux : en V1, Snap n'est jamais une cible
// d'installation (décision « Flatpak d'abord »).
func (s *Snap) Available(context.Context, string) (bool, error) { return false, nil }
