package linux

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bernard-linux/bernard/internal/aptrepo"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/sysexec"
)

// Runner et ErrMissingCommand viennent de sysexec ; ces alias gardent
// l'API du package lisible.
type Runner = sysexec.Runner

var (
	ErrMissingCommand = sysexec.ErrMissingCommand
	ExecRunner        = sysexec.Exec
	lines             = sysexec.Lines
)

// aptApps croise les paquets installés manuellement avec leurs versions.
func aptApps(ctx context.Context, run Runner) ([]inventory.App, error) {
	manual, err := run(ctx, "apt-mark", "showmanual")
	if err != nil {
		return nil, err
	}
	versions := map[string]string{}
	if out, err := run(ctx, "dpkg-query", "-W", "-f", "${Package}\t${Version}\n"); err == nil {
		for _, l := range lines(out) {
			if name, ver, ok := strings.Cut(l, "\t"); ok {
				versions[name] = ver
			}
		}
	}
	pkgs := lines(manual)
	// Dépôt d'origine de chaque paquet, en une seule commande.
	origins := map[string]string{}
	if len(pkgs) > 0 {
		if out, err := run(ctx, "apt-cache", append([]string{"policy"}, pkgs...)...); err == nil {
			origins = aptrepo.Origins(out)
		}
	}
	var apps []inventory.App
	for _, pkg := range pkgs {
		apps = append(apps, inventory.App{
			SourceID: "apt:" + pkg,
			Name:     pkg,
			Version:  versions[pkg],
			Origin:   inventory.OriginApt,
			Repo:     origins[pkg],
		})
	}
	return apps, nil
}

// flatpakApps liste les applications Flatpak (pas les runtimes).
func flatpakApps(ctx context.Context, run Runner) ([]inventory.App, error) {
	out, err := run(ctx, "flatpak", "list", "--app", "--columns=application,name,version")
	if err != nil {
		return nil, err
	}
	var apps []inventory.App
	for _, l := range lines(out) {
		f := strings.Split(l, "\t")
		app := inventory.App{SourceID: "flatpak:" + f[0], Name: f[0], Origin: inventory.OriginFlatpak}
		if len(f) > 1 && f[1] != "" {
			app.Name = f[1]
		}
		if len(f) > 2 {
			app.Version = f[2]
		}
		apps = append(apps, app)
	}
	return apps, nil
}

// snapInfra regroupe les Snap techniques, qui ne sont pas des applications.
func snapInfra(name string) bool {
	switch {
	case name == "snapd", name == "bare", name == "gtk-common-themes",
		strings.HasPrefix(name, "core"),
		strings.HasPrefix(name, "gnome-") && strings.Contains(name, "-2"),
		strings.HasPrefix(name, "kf5-"), strings.HasPrefix(name, "mesa-"):
		return true
	}
	return false
}

// snapApps lit la sortie tabulaire de `snap list`.
func snapApps(ctx context.Context, run Runner) ([]inventory.App, error) {
	out, err := run(ctx, "snap", "list")
	if err != nil {
		return nil, err
	}
	var apps []inventory.App
	for i, l := range lines(out) {
		if i == 0 { // en-tête
			continue
		}
		f := strings.Fields(l)
		if len(f) < 2 || snapInfra(f[0]) {
			continue
		}
		apps = append(apps, inventory.App{
			SourceID: "snap:" + f[0], Name: f[0], Version: f[1], Origin: inventory.OriginSnap,
		})
	}
	return apps, nil
}

// wifiNetworks liste les connexions Wi-Fi enregistrées (noms seulement :
// les secrets ne sont lus qu'au moment du transfert, jamais inventoriés).
func wifiNetworks(ctx context.Context, run Runner) ([]string, error) {
	out, err := run(ctx, "nmcli", "-t", "-f", "NAME,TYPE", "connection", "show")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, l := range lines(out) {
		// nmcli échappe les ':' du nom en '\:' ; le type est après le dernier ':'.
		i := strings.LastIndex(l, ":")
		if i < 0 || !strings.Contains(l[i+1:], "wireless") {
			continue
		}
		names = append(names, strings.ReplaceAll(l[:i], `\:`, ":"))
	}
	sort.Strings(names)
	return names, nil
}

// printers liste les files d'impression CUPS.
func printers(ctx context.Context, run Runner) ([]string, error) {
	out, err := run(ctx, "lpstat", "-e")
	if err != nil {
		return nil, err
	}
	return lines(out), nil
}

// wifiFromFiles lit les noms des connexions Wi-Fi directement dans les
// fichiers de NetworkManager (droits administrateur requis). Contrairement à
// nmcli, cela fonctionne aussi pour un système monté depuis un autre disque.
func wifiFromFiles(root string) []string {
	files, _ := filepath.Glob(filepath.Join(root, "etc/NetworkManager/system-connections/*"))
	var names []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var id, typ string
		for _, l := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(l), "id="); ok && id == "" {
				id = v
			}
			if v, ok := strings.CutPrefix(strings.TrimSpace(l), "type="); ok && typ == "" {
				typ = v
			}
		}
		if id != "" && (typ == "wifi" || typ == "802-11-wireless") {
			names = append(names, id)
		}
	}
	sort.Strings(names)
	return names
}
