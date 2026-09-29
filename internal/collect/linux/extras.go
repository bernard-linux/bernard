package linux

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/bernard-linux/bernard/internal/i18n"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/settings"
	"github.com/bernard-linux/bernard/internal/sysexec"
)

// CollectExtras lit les réglages qui exigent les droits administrateur :
// connexions Wi-Fi (avec leur mot de passe), tâches planifiées, imprimantes
// et préférences du bureau de chaque compte. Lecture seule.
func CollectExtras(ctx context.Context, root, desktop string, users []inventory.User, exec sysexec.Executor) settings.Extras {
	if root == "" {
		root = "/"
	}
	ex := settings.Extras{Desktop: desktop, Dconf: map[string]string{}, Crontabs: map[string]string{}}
	warn := func(s string) { ex.Warnings = append(ex.Warnings, s) }

	files, _ := filepath.Glob(filepath.Join(root, "etc/NetworkManager/system-connections/*"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		s := string(b)
		if _, _, _, err := settings.SanitizeWifi(s, nil); err == nil {
			ex.Wifi = append(ex.Wifi, settings.NMConnection{File: filepath.Base(f), Content: s})
		}
	}

	for _, u := range users {
		b, err := os.ReadFile(filepath.Join(root, "var/spool/cron/crontabs", u.Login))
		if err != nil {
			continue
		}
		var keep []string
		for _, l := range strings.Split(string(b), "\n") {
			// En-têtes ajoutés par l'outil crontab lui-même.
			if strings.HasPrefix(l, "# DO NOT EDIT") || strings.HasPrefix(l, "# (") {
				continue
			}
			keep = append(keep, l)
		}
		if c := strings.TrimSpace(strings.Join(keep, "\n")); c != "" {
			ex.Crontabs[u.Login] = c + "\n"
		}
	}

	if root != "/" {
		warn(i18n.T("préférences du bureau et imprimantes non lues : système monté depuis un autre disque"))
		return ex
	}

	if out, err := exec(ctx, sysexec.Cmd{Name: "lpstat", Args: []string{"-v"}}); err == nil {
		def := ""
		if d, err := exec(ctx, sysexec.Cmd{Name: "lpstat", Args: []string{"-d"}}); err == nil {
			if _, name, ok := strings.Cut(strings.TrimSpace(d), ": "); ok {
				def = name
			}
		}
		for _, l := range sysexec.Lines(out) {
			rest, ok := strings.CutPrefix(l, "device for ")
			if !ok {
				continue
			}
			name, uri, ok := strings.Cut(rest, ": ")
			if ok {
				ex.Printers = append(ex.Printers, settings.Printer{Name: name, URI: uri, Default: name == def})
			}
		}
	}

	for _, u := range users {
		out, err := exec(ctx, sysexec.Cmd{Name: "runuser", Args: []string{"-u", u.Login, "--", "env",
			"HOME=" + u.Home, "XDG_CONFIG_HOME=" + filepath.Join(u.Home, ".config"), "dconf", "dump", "/"}})
		if err != nil {
			warn(i18n.Tf("préférences du bureau de %s non lues", u.Login))
			continue
		}
		ex.Dconf[u.Login] = out
	}
	return ex
}
