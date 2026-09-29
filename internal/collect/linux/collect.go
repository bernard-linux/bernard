package linux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/bernard-linux/bernard/internal/aptrepo"
	"github.com/bernard-linux/bernard/internal/inventory"
)

// Options paramètre la collecte.
type Options struct {
	// Root permet de lire un système monté ailleurs (tests, disque externe).
	// Vide ou "/" pour la machine courante.
	Root string
	// Runner exécute les commandes de consultation ; ExecRunner par défaut.
	Runner Runner
	// SkipData désactive la mesure des dossiers personnels (rapide, pour tests).
	SkipData bool
	// AgentVersion est inscrite dans l'inventaire.
	AgentVersion string
	// Offline lit les applications et imprimantes dans les fichiers de Root
	// au lieu d'interroger les commandes : Root est un système qui ne tourne
	// pas (disque d'un ancien PC monté ailleurs).
	Offline bool
}

// Collect produit l'inventaire de la machine source. Une information
// impossible à obtenir n'arrête jamais la collecte : elle devient un
// avertissement, repris dans le rapport final.
func Collect(ctx context.Context, opt Options) (*inventory.Inventory, error) {
	if opt.Root == "" {
		opt.Root = "/"
	}
	if opt.Runner == nil {
		opt.Runner = ExecRunner
	}
	inv := &inventory.Inventory{
		Schema:    inventory.Schema,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		Agent:     opt.AgentVersion,
		Source:    readSource(opt.Root),
	}
	warn := func(format string, a ...any) { inv.Warnings = append(inv.Warnings, fmt.Sprintf(format, a...)) }

	users, err := readUsers(opt.Root)
	if err != nil {
		return nil, fmt.Errorf("lecture des comptes impossible : %w", err)
	}
	inv.Users = users

	offline := opt.Offline
	if offline {
		inv.Apps = offlineApps(opt.Root)
	}
	for _, src := range []struct {
		label string
		fn    func(context.Context, Runner) ([]inventory.App, error)
	}{
		{"apt", aptApps}, {"Flatpak", flatpakApps}, {"Snap", snapApps},
	} {
		if offline {
			break
		}
		apps, err := src.fn(ctx, opt.Runner)
		switch {
		case errors.Is(err, ErrMissingCommand):
			// Normal : Flatpak ou Snap non installé sur la source.
		case err != nil:
			warn("inventaire %s incomplet : %v", src.label, err)
		}
		inv.Apps = append(inv.Apps, apps...)
	}
	for i := range inv.Apps {
		inv.Apps[i].ID = "a" + strconv.Itoa(i+1)
	}
	if !opt.SkipData {
		mountsFile := ""
		if opt.Root == "/" {
			mountsFile = "/proc/self/mounts"
		}
		var sys []inventory.DataSet
		inv.System, inv.Disks, sys = ScanSystem(opt.Root, mountsFile, inv.Users)
		inv.DataSets = append(inv.DataSets, sys...)
	}
	inv.AptSources = aptrepo.Read(opt.Root)
	inv.Packages = allPackages(opt.Root)
	inv.PackagesRemoved = removedPackages(opt.Root, inv.Packages)

	if wifi := wifiFromFiles(opt.Root); len(wifi) > 0 || offline {
		inv.Network.Wifi = wifi
	} else if wifi, err := wifiNetworks(ctx, opt.Runner); err == nil {
		inv.Network.Wifi = wifi
	} else if !errors.Is(err, ErrMissingCommand) {
		warn("réseaux Wi-Fi non inventoriés : %v", err)
	}
	if offline {
		inv.Network.Printers = offlinePrinters(opt.Root)
	} else if pr, err := printers(ctx, opt.Runner); err == nil {
		inv.Network.Printers = pr
	} else if !errors.Is(err, ErrMissingCommand) {
		warn("imprimantes non inventoriées : %v", err)
	}

	if !opt.SkipData {
		for i, u := range inv.Users {
			home := filepath.Join(opt.Root, u.Home)
			if _, err := os.Lstat(home); err != nil {
				warn("dossier personnel de %s introuvable (%s)", u.Login, u.Home)
				continue
			}
			st, err := measure(home, DefaultExcludes)
			if err != nil {
				warn("mesure de %s incomplète : %v", u.Home, err)
			}
			if st.Unreadable > 0 {
				warn("%d éléments illisibles dans %s (droits insuffisants ?)", st.Unreadable, u.Home)
			}
			inv.DataSets = append(inv.DataSets, inventory.DataSet{
				ID:        "d" + strconv.Itoa(i+1),
				User:      u.ID,
				Kind:      "home",
				Path:      home,
				Files:     st.Files,
				SizeBytes: st.Bytes,
				Excluded:  DefaultExcludes,
			})
		}
	}
	return inv, nil
}
