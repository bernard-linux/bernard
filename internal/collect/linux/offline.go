package linux

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bernard-linux/bernard/internal/inventory"
)

// Lecture d'un système qui ne tourne pas (--root : disque d'un ancien PC
// monté ailleurs). Les commandes (apt-mark, flatpak, snap, lpstat)
// décriraient la machine qui exécute l'agent : on lit donc directement les
// fichiers du système visé.

// offlineApps lit les applications installées sous root.
func offlineApps(root string) []inventory.App {
	var apps []inventory.App
	apps = append(apps, offlineApt(root)...)
	apps = append(apps, offlineFlatpak(root)...)
	apps = append(apps, offlineSnap(root)...)
	return apps
}

// offlineApt reproduit « apt-mark showmanual » : paquets installés (statut
// dpkg) qui ne sont pas marqués comme installés automatiquement.
func offlineApt(root string) []inventory.App {
	auto := map[string]bool{}
	eachStanza(filepath.Join(root, "var/lib/apt/extended_states"), func(f map[string]string) {
		if f["Auto-Installed"] == "1" {
			auto[f["Package"]] = true
		}
	})
	var apps []inventory.App
	eachStanza(filepath.Join(root, "var/lib/dpkg/status"), func(f map[string]string) {
		name := f["Package"]
		if name == "" || auto[name] || !strings.HasSuffix(f["Status"], " installed") {
			return
		}
		apps = append(apps, inventory.App{SourceID: "apt:" + name, Name: name, Version: f["Version"], Origin: inventory.OriginApt})
	})
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	return apps
}

// eachStanza parcourt un fichier au format deb822 (blocs séparés par une
// ligne vide ; les lignes de continuation sont ignorées).
func eachStanza(path string, fn func(map[string]string)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	cur := map[string]string{}
	for sc.Scan() {
		l := sc.Text()
		if strings.TrimSpace(l) == "" {
			if len(cur) > 0 {
				fn(cur)
			}
			cur = map[string]string{}
			continue
		}
		if l[0] == ' ' || l[0] == '\t' {
			continue
		}
		if k, v, ok := strings.Cut(l, ":"); ok {
			cur[k] = strings.TrimSpace(v)
		}
	}
	if len(cur) > 0 {
		fn(cur)
	}
}

// offlineFlatpak lit les applications Flatpak du système et des comptes.
func offlineFlatpak(root string) []inventory.App {
	seen := map[string]bool{}
	var apps []inventory.App
	dirs, _ := filepath.Glob(filepath.Join(root, "var/lib/flatpak/app/*"))
	user, _ := filepath.Glob(filepath.Join(root, "home/*/.local/share/flatpak/app/*"))
	for _, d := range append(dirs, user...) {
		id := filepath.Base(d)
		if seen[id] || !strings.Contains(id, ".") {
			continue
		}
		seen[id] = true
		apps = append(apps, inventory.App{SourceID: "flatpak:" + id, Name: id, Origin: inventory.OriginFlatpak})
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	return apps
}

// offlineSnap lit les Snap installés (dossiers /snap/<nom>/current).
func offlineSnap(root string) []inventory.App {
	cur, _ := filepath.Glob(filepath.Join(root, "snap/*/current"))
	var apps []inventory.App
	for _, c := range cur {
		name := filepath.Base(filepath.Dir(c))
		if name == "bin" || snapInfra(name) {
			continue
		}
		rev, _ := os.Readlink(c)
		apps = append(apps, inventory.App{SourceID: "snap:" + name, Name: name, Version: rev, Origin: inventory.OriginSnap})
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	return apps
}

// offlinePrinters lit les files d'impression dans printers.conf.
func offlinePrinters(root string) []string {
	b, err := os.ReadFile(filepath.Join(root, "etc/cups/printers.conf"))
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		for _, p := range []string{"<Printer ", "<DefaultPrinter "} {
			if name, ok := strings.CutPrefix(l, p); ok {
				out = append(out, strings.TrimSuffix(name, ">"))
			}
		}
	}
	return out
}
