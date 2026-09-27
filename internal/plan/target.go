package plan

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/bernard-linux/bernard/internal/hardware"
	"github.com/bernard-linux/bernard/internal/pkgmgr"
	"github.com/bernard-linux/bernard/internal/settings"
	"github.com/bernard-linux/bernard/internal/sysexec"
)

// DetectTarget relève l'état de la machine courante, considérée comme cible.
// Lecture seule.
func DetectTarget(ctx context.Context, run sysexec.Runner) (Target, []string) {
	var warnings []string
	t := Target{HomeRoot: "/home", ExistingUsers: map[string]bool{}}

	if f, err := os.Open("/etc/os-release"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			k, v, _ := strings.Cut(sc.Text(), "=")
			v = strings.Trim(v, `"'`)
			switch k {
			case "ID":
				t.Distro = v
			case "VERSION_ID":
				t.Version = v
			}
		}
		f.Close()
	}
	t.Desktop = settings.DetectDesktop("/")

	var st syscall.Statfs_t
	if err := syscall.Statfs(t.HomeRoot, &st); err == nil {
		t.FreeBytes = int64(st.Bavail) * int64(st.Bsize)
	} else {
		warnings = append(warnings, "espace libre inconnu : "+err.Error())
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
		warnings = append(warnings, "paquets apt installés inconnus : "+err.Error())
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

	t.Keyboard = hardware.Keyboard("/")
	t.GPUs = hardware.GPUs("/")
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
// existe) ; faux pour une entrée cachée du menu.
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
	if nameFR != "" {
		name = nameFR
	}
	return name, name != ""
}
