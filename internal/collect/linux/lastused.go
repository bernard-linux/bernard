package linux

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/bernard-linux/bernard/internal/inventory"
)

// Date de dernière utilisation des applications.
//
// Linux note la date du dernier accès à chaque fichier (au plus une fois par
// jour, option « relatime »). Pour une application du menu, c'est la date du
// dernier lancement de son programme. Par prudence, la date n'est retenue
// que si le programme a été lu après sa dernière mise à jour, et jamais sur
// un disque monté sans ces dates (« noatime ») : une date inconnue laisse
// l'application cochée.

var binDirs = []string{"usr/bin", "usr/local/bin", "bin", "usr/games", "usr/sbin", "snap/bin"}

// atimeReliable indique que le disque racine enregistre les dates d'accès.
func atimeReliable(mountsFile string) bool {
	b, err := os.ReadFile(mountsFile)
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) >= 4 && f[1] == "/" {
			return !strings.Contains(f[3], "noatime")
		}
	}
	return false
}

// accessTime renvoie la date du dernier accès, si elle est postérieure à la
// dernière modification.
func accessTime(p string) (time.Time, bool) {
	fi, err := os.Stat(p)
	if err != nil {
		return time.Time{}, false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, false
	}
	at := time.Unix(st.Atim.Sec, st.Atim.Nsec)
	if !at.After(fi.ModTime().Add(time.Minute)) {
		return time.Time{}, false
	}
	return at, true
}

// desktopExec lit le programme lancé par un fichier .desktop.
func desktopExec(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	in := false
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(l, "[") {
			in = l == "[Desktop Entry]"
			continue
		}
		if v, ok := strings.CutPrefix(l, "Exec="); ok && in {
			fl := strings.Fields(v)
			for len(fl) > 0 && (fl[0] == "env" || strings.Contains(fl[0], "=")) {
				fl = fl[1:]
			}
			if len(fl) > 0 {
				return strings.Trim(fl[0], `"`)
			}
		}
	}
	return ""
}

// resolveBin trouve le programme sous root.
func resolveBin(root, exe string) string {
	if strings.HasPrefix(exe, "/") {
		return filepath.Join(root, exe)
	}
	for _, d := range binDirs {
		p := filepath.Join(root, d, exe)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// pkgDesktops lit, dans la base de dpkg, les fichiers .desktop d'un paquet.
func pkgDesktops(root, pkg string) []string {
	var out []string
	for _, name := range []string{pkg + ".list", pkg + ":amd64.list", pkg + ":arm64.list"} {
		f, err := os.Open(filepath.Join(root, "var/lib/dpkg/info", name))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			l := sc.Text()
			if strings.HasPrefix(l, "/usr/share/applications/") && strings.HasSuffix(l, ".desktop") {
				out = append(out, filepath.Join(root, l))
			}
		}
		f.Close()
	}
	return out
}

// newest garde la plus récente de deux dates.
func newest(a *time.Time, t time.Time) *time.Time {
	if a == nil || t.After(*a) {
		return &t
	}
	return a
}

// fillLastUsed complète la date de dernière utilisation des applications.
func fillLastUsed(root, mountsFile string, apps []inventory.App, users []inventory.User) {
	reliable := mountsFile != "" && atimeReliable(mountsFile)
	for i, app := range apps {
		switch app.Origin {
		case inventory.OriginApt:
			if !reliable {
				continue
			}
			for _, d := range pkgDesktops(root, app.Name) {
				if bin := resolveBin(root, desktopExec(d)); bin != "" {
					if t, ok := accessTime(bin); ok {
						apps[i].LastUsed = newest(apps[i].LastUsed, t)
					}
				}
			}
		case inventory.OriginFlatpak, inventory.OriginSnap:
			// Données de l'application dans chaque dossier personnel : leur
			// date de modification suit l'usage.
			id := strings.TrimPrefix(strings.TrimPrefix(app.SourceID, "flatpak:"), "snap:")
			for _, u := range users {
				dir := filepath.Join(root, u.Home, ".var/app", id)
				if app.Origin == inventory.OriginSnap {
					dir = filepath.Join(root, u.Home, "snap", id)
				}
				if t, ok := newestMTime(dir); ok {
					apps[i].LastUsed = newest(apps[i].LastUsed, t)
				}
			}
		}
	}
}

// newestMTime : date de modification la plus récente d'un dossier et de ses
// sous-dossiers directs.
func newestMTime(dir string) (time.Time, bool) {
	fi, err := os.Stat(dir)
	if err != nil {
		return time.Time{}, false
	}
	t := fi.ModTime()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.ModTime().After(t) {
			t = info.ModTime()
		}
	}
	return t, true
}
