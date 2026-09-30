package plan

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/bernard-linux/bernard/internal/aptrepo"
	"github.com/bernard-linux/bernard/internal/hardware"
	"github.com/bernard-linux/bernard/internal/i18n"
	"github.com/bernard-linux/bernard/internal/pkgmgr"
	"github.com/bernard-linux/bernard/internal/settings"
	"github.com/bernard-linux/bernard/internal/sysexec"
)

// DetectTarget relève l'état de la machine courante, considérée comme cible.
// Lecture seule.
// parseOSRelease lit /etc/os-release : distribution, version et nom de code
// (celui d'Ubuntu d'abord : Zorin et Mint donnent aussi le leur, qui ne
// désigne pas leurs dépôts Ubuntu).
func parseOSRelease(r io.Reader) (distro, version, codename string) {
	kv := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), "=")
		kv[k] = strings.Trim(v, `"'`)
	}
	codename = kv["UBUNTU_CODENAME"]
	if codename == "" {
		codename = kv["VERSION_CODENAME"]
	}
	return kv["ID"], kv["VERSION_ID"], codename
}

func DetectTarget(ctx context.Context, run sysexec.Runner) (Target, []string) {
	var warnings []string
	t := Target{HomeRoot: "/home", ExistingUsers: map[string]bool{}}

	if f, err := os.Open("/etc/os-release"); err == nil {
		t.Distro, t.Version, t.Codename = parseOSRelease(f)
		f.Close()
	}
	t.Desktop = settings.DetectDesktop("/")

	var st syscall.Statfs_t
	if err := syscall.Statfs(t.HomeRoot, &st); err == nil {
		t.FreeBytes = int64(st.Bavail) * int64(st.Bsize)
	} else {
		warnings = append(warnings, i18n.Tf("espace libre inconnu : %v", err))
	}

	if f, err := os.Open("/etc/passwd"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if login, _, ok := strings.Cut(sc.Text(), ":"); ok {
				t.ExistingUsers[login] = true
			}
		}
		f.Close()
	}

	apt := &pkgmgr.Apt{Run: run}
	if set, err := apt.Installed(ctx); err == nil {
		t.AptInstalled = set
	} else {
		warnings = append(warnings, i18n.Tf("paquets apt installés inconnus : %v", err))
	}
	t.AptAvailable = func(name string) bool {
		ok, err := apt.Available(ctx, name)
		return err == nil && ok
	}

	fp := &pkgmgr.Flatpak{Run: run}
	t.FlatpakReady = fp.Ready(ctx)
	t.FlatpakInstalled, _ = fp.Installed(ctx)

	sn := &pkgmgr.Snap{Run: run}
	t.SnapInstalled, _ = sn.Installed(ctx)

	t.KnownRepos = aptrepo.Known("/")
	t.UUIDs, t.DataMounts = detectDisks()
	t.Keyboard = hardware.Keyboard("/")
	t.GPUs = hardware.GPUs("/")
	if al := settings.DetectAutoLogin("/"); al != nil {
		t.AutoLoginUser = al.User
	}
	detectRemovable(ctx, run, &t)
	return t, warnings
}

// detectRemovable relève ce qu'il faut pour proposer des retraits : paquets
// installés à la main, applications du menu et leur paquet, paquets
// protégés et tailles. Lecture seule ; en cas d'échec, aucun retrait n'est
// proposé.
func detectRemovable(ctx context.Context, run sysexec.Runner, t *Target) {
	out, err := run(ctx, "apt-mark", "showmanual")
	if err != nil {
		return
	}
	t.AptManual = map[string]bool{}
	for _, l := range sysexec.Lines(out) {
		t.AptManual[l] = true
	}

	t.AptProtected, t.AptSize = map[string]bool{}, map[string]int64{}
	if out, err := run(ctx, "dpkg-query", "-W", "-f", "${Package}\t${Essential}\t${Priority}\t${Installed-Size}\n"); err == nil {
		for _, l := range sysexec.Lines(out) {
			f := strings.Split(l, "\t")
			if len(f) < 4 {
				continue
			}
			if f[1] == "yes" || f[2] == "required" || f[2] == "important" {
				t.AptProtected[f[0]] = true
			}
			if kb, err := strconv.ParseInt(f[3], 10, 64); err == nil {
				t.AptSize[f[0]] = kb * 1024
			}
		}
	}

	// Applications du menu : fichiers .desktop visibles, et leur paquet.
	files, _ := filepath.Glob("/usr/share/applications/*.desktop")
	names := map[string]string{}
	var visible []string
	for _, f := range files {
		if name, ok := desktopName(f); ok {
			names[f] = name
			visible = append(visible, f)
		}
	}
	t.AptApps = map[string]string{}
	if len(visible) > 0 {
		// dpkg-query -S échoue si un fichier n'appartient à aucun paquet,
		// mais la sortie reste exploitable.
		out, _ := run(ctx, "dpkg-query", append([]string{"-S"}, visible...)...)
		for _, l := range sysexec.Lines(out) {
			pkgs, path, ok := strings.Cut(l, ": ")
			if !ok || strings.Contains(pkgs, ",") {
				continue // fichier partagé entre plusieurs paquets
			}
			pkg, _, _ := strings.Cut(pkgs, ":") // « paquet:amd64 »
			if _, seen := t.AptApps[pkg]; !seen {
				t.AptApps[pkg] = names[path]
			}
		}
	}

	t.RemovalImpact = func(pkg string) ([]string, error) {
		out, err := run(ctx, "apt-get", "-s", "-q", "remove", "--", pkg)
		if err != nil {
			return nil, err
		}
		var gone []string
		for _, l := range sysexec.Lines(out) {
			if rest, ok := strings.CutPrefix(l, "Remv "); ok {
				name, _, _ := strings.Cut(rest, " ")
				name, _, _ = strings.Cut(name, ":")
				gone = append(gone, name)
			}
		}
		return gone, nil
	}

	t.FlatpakNames = map[string]string{}
	if out, err := run(ctx, "flatpak", "list", "--app", "--columns=application,name"); err == nil {
		for _, l := range sysexec.Lines(out) {
			if id, name, ok := strings.Cut(l, "\t"); ok {
				t.FlatpakNames[id] = name
			}
		}
	}
}

// desktopName lit le nom affiché d'un fichier .desktop (en français s'il
// existe et que Bernard parle français) ; faux pour une entrée cachée du menu.
func desktopName(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	var name, nameFR string
	inEntry := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(l, "[") {
			inEntry = l == "[Desktop Entry]"
			continue
		}
		if !inEntry {
			continue
		}
		k, v, _ := strings.Cut(l, "=")
		switch k {
		case "Name":
			name = v
		case "Name[fr]":
			nameFR = v
		case "NoDisplay", "Hidden":
			if strings.EqualFold(v, "true") {
				return "", false
			}
		case "Type":
			if v != "Application" {
				return "", false
			}
		}
	}
	if nameFR != "" && i18n.Lang() == i18n.FR {
		name = nameFR
	}
	return name, name != ""
}

// detectDisks relève les systèmes de fichiers présents (par identifiant)
// et les disques de données montés, avec leur place libre.
func detectDisks() (map[string]bool, []Mount) {
	uuids := map[string]bool{}
	entries, _ := os.ReadDir("/dev/disk/by-uuid")
	for _, e := range entries {
		uuids[e.Name()] = true
	}
	var mounts []Mount
	b, _ := os.ReadFile("/proc/self/mounts")
	seen := map[string]bool{}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) < 3 || !strings.HasPrefix(f[0], "/dev/") || f[2] == "squashfs" || f[2] == "iso9660" || f[2] == "vfat" {
			continue
		}
		p := f[1]
		// Disques amovibles (clé USB, disque du paquet) : jamais une destination.
		if p == "/" || p == "/home" || strings.HasPrefix(p, "/boot") || strings.HasPrefix(p, "/snap") ||
			strings.HasPrefix(p, "/media/") || strings.HasPrefix(p, "/run/") || seen[p] {
			continue
		}
		seen[p] = true
		var st syscall.Statfs_t
		if syscall.Statfs(p, &st) == nil {
			mounts = append(mounts, Mount{Point: p, Free: int64(st.Bavail) * int64(st.Bsize)})
		}
	}
	return uuids, mounts
}
