package plan

import (
	"bufio"
	"context"
	"os"
	"strings"
	"syscall"

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
	return t, warnings
}
