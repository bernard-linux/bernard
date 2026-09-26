package settings

import (
	"os"
	"path/filepath"
	"strings"
)

// NormalizeDesktop ramène XDG_CURRENT_DESKTOP (« zorin:GNOME »,
// « X-Cinnamon »…) à un identifiant simple.
func NormalizeDesktop(v string) string {
	v = strings.ToLower(v)
	switch {
	case strings.Contains(v, "cinnamon"):
		return "cinnamon"
	case strings.Contains(v, "gnome") || strings.Contains(v, "zorin") || strings.Contains(v, "ubuntu") || strings.Contains(v, "unity"):
		return "gnome"
	case strings.Contains(v, "kde") || strings.Contains(v, "plasma"):
		return "kde"
	case strings.Contains(v, "xfce"):
		return "xfce"
	case strings.Contains(v, "mate"):
		return "mate"
	}
	return v
}

// DetectDesktop trouve le bureau de la machine. La variable d'environnement
// disparaît sous sudo ou pkexec ; on regarde alors les sessions installées.
func DetectDesktop(root string) string {
	if v := NormalizeDesktop(os.Getenv("XDG_CURRENT_DESKTOP")); v != "" {
		return v
	}
	if root == "" {
		root = "/"
	}
	found := map[string]bool{}
	for _, dir := range []string{"usr/share/xsessions", "usr/share/wayland-sessions"} {
		entries, _ := os.ReadDir(filepath.Join(root, dir))
		for _, e := range entries {
			found[NormalizeDesktop(strings.TrimSuffix(e.Name(), ".desktop"))] = true
		}
	}
	// Cinnamon d'abord : Mint installe aussi des fichiers de session GNOME.
	for _, d := range []string{"cinnamon", "gnome", "kde", "xfce", "mate"} {
		if found[d] {
			return d
		}
	}
	return ""
}
