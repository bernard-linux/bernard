// Package services arrête et relance les services dont les données sont
// copiées (bases de données, Docker, serveur FileMaker…), pour que leurs
// fichiers soient cohérents : on ne copie jamais une base en train d'écrire.
//
// Sur l'ancien ordinateur, le service est arrêté le temps de sa copie puis
// relancé tel qu'il était ; ses données ne sont pas touchées.
package services

import (
	"context"
	"strings"

	"github.com/bernard-linux/bernard/internal/i18n"
	"github.com/bernard-linux/bernard/internal/sysexec"
)

// Libvirt n'est pas arrêté : ce sont les machines virtuelles qui doivent
// être éteintes, pour que leurs disques ne changent pas pendant la copie.
const Libvirt = "libvirtd"

// Units découpe la liste d'unités d'un jeu de données (« docker.socket docker »).
func Units(s string) []string {
	var out []string
	for _, u := range strings.Fields(s) {
		if validUnit(u) {
			out = append(out, u)
		}
	}
	return out
}

func validUnit(u string) bool {
	if u == "" || len(u) > 64 || strings.HasPrefix(u, "-") {
		return false
	}
	for _, r := range u {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.@", r)) {
			return false
		}
	}
	return true
}

// Controller pilote les services (Exec remplacé dans les tests).
type Controller struct{ Exec sysexec.Executor }

// New renvoie le contrôleur réel.
func New() *Controller { return &Controller{Exec: sysexec.Run} }

func (c *Controller) active(ctx context.Context, unit string) bool {
	out, err := c.Exec(ctx, sysexec.Cmd{Name: "systemctl", Args: []string{"is-active", unit}})
	return err == nil && strings.TrimSpace(out) == "active"
}

// Pause arrête les unités actives et renvoie de quoi les relancer. Pour
// libvirt, vérifie seulement qu'aucune machine virtuelle ne tourne.
func (c *Controller) Pause(ctx context.Context, spec string) (func(), error) {
	units := Units(spec)
	if len(units) == 1 && units[0] == Libvirt {
		out, err := c.Exec(ctx, sysexec.Cmd{Name: "virsh", Args: []string{"-c", "qemu:///system", "list", "--state-running", "--name"}})
		if err == nil {
			if names := sysexec.Lines(out); len(names) > 0 {
				return nil, i18n.Errorf("machines virtuelles allumées (%s) : éteignez-les, puis relancez la migration pour les copier", strings.Join(names, ", "))
			}
		}
		return func() {}, nil
	}
	// États relevés AVANT tout arrêt : arrêter docker.socket arrête aussi
	// docker.service, qui ne serait sinon plus vu actif ni relancé.
	var active []string
	for _, u := range units {
		if c.active(ctx, u) {
			active = append(active, u)
		}
	}
	var stopped []string
	for _, u := range active {
		if _, err := c.Exec(ctx, sysexec.Cmd{Name: "systemctl", Args: []string{"stop", u}}); err != nil {
			c.resume(stopped)
			return nil, i18n.Errorf("arrêt du service %s impossible : %w", u, err)
		}
		stopped = append(stopped, u)
	}
	return func() { c.resume(stopped) }, nil
}

func (c *Controller) resume(units []string) {
	for i := len(units) - 1; i >= 0; i-- {
		c.Exec(context.Background(), sysexec.Cmd{Name: "systemctl", Args: []string{"start", units[i]}})
	}
}

// Stop arrête les unités sur la cible avant d'y déposer leurs données (sans
// erreur si le service n'est pas installé).
func (c *Controller) Stop(ctx context.Context, spec string) {
	for _, u := range Units(spec) {
		if u != Libvirt {
			c.Exec(ctx, sysexec.Cmd{Name: "systemctl", Args: []string{"stop", u}})
		}
	}
}

// Start relance les unités sur la cible après la copie (libvirt est
// redémarré pour relire les définitions des machines virtuelles).
func (c *Controller) Start(ctx context.Context, spec string) {
	units := Units(spec)
	for i := len(units) - 1; i >= 0; i-- {
		verb := "start"
		if units[i] == Libvirt {
			verb = "try-restart"
		}
		c.Exec(ctx, sysexec.Cmd{Name: "systemctl", Args: []string{verb, units[i]}})
	}
}

// PrepareFor renvoie la fonction Prepare des jeux de données pour la
// machine courante (nil sur un système monté ailleurs ou sans droits).
func PrepareFor(ctx context.Context, live bool) func(string) (func(), error) {
	if !live {
		return nil
	}
	c := New()
	return func(spec string) (func(), error) { return c.Pause(ctx, spec) }
}
