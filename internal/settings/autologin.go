package settings

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Ouverture de session automatique.
//
// Un nouvel ordinateur est souvent installé avec un compte provisoire
// (« tmp ») qui ouvre sa session tout seul. Après la migration, cette
// automatisation ramènerait l'utilisateur dans le compte provisoire au lieu
// de son compte migré : Bernard la désactive, pour que l'écran de connexion
// montre clairement les comptes. Les fichiers modifiés sont sauvegardés,
// l'annulation les remet en place.

// Fichiers de configuration des gestionnaires de connexion, relatifs à la
// racine : GDM (Ubuntu, Zorin, Debian GNOME) et LightDM (Linux Mint).
var (
	gdmFiles     = []string{"etc/gdm3/custom.conf", "etc/gdm3/daemon.conf", "etc/gdm/custom.conf"}
	lightdmGlobs = []string{"etc/lightdm/lightdm.conf", "etc/lightdm/lightdm.conf.d/*.conf"}
)

// AutoLogin décrit une ouverture de session automatique active.
type AutoLogin struct {
	User  string   `json:"user"`
	Files []string `json:"files"` // chemins absolus sous la racine
}

func iniLines(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out, nil
}

func truthy(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "true" || v == "1" || v == "yes"
}

// gdmAuto lit un fichier GDM : compte ouvert automatiquement (ou après un
// délai), section [daemon].
func gdmAuto(lines []string) string {
	sec := ""
	kv := map[string]string{}
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			sec = strings.ToLower(t)
			continue
		}
		if sec != "[daemon]" || strings.HasPrefix(t, "#") {
			continue
		}
		if k, v, ok := strings.Cut(t, "="); ok {
			kv[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	if truthy(kv["AutomaticLoginEnable"]) && kv["AutomaticLogin"] != "" {
		return kv["AutomaticLogin"]
	}
	if truthy(kv["TimedLoginEnable"]) && kv["TimedLogin"] != "" {
		return kv["TimedLogin"]
	}
	return ""
}

// lightdmAuto lit un fichier LightDM : clé autologin-user active.
func lightdmAuto(lines []string) string {
	user := ""
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") {
			continue
		}
		if k, v, ok := strings.Cut(t, "="); ok && strings.TrimSpace(k) == "autologin-user" {
			user = strings.TrimSpace(v) // la dernière valeur l'emporte
		}
	}
	return user
}

func lightdmFiles(root string) []string {
	var out []string
	for _, g := range lightdmGlobs {
		m, _ := filepath.Glob(filepath.Join(root, g))
		out = append(out, m...)
	}
	return out
}

// DetectAutoLogin renvoie l'ouverture de session automatique configurée,
// ou nil.
func DetectAutoLogin(root string) *AutoLogin {
	if root == "" {
		root = "/"
	}
	var al AutoLogin
	for _, f := range gdmFiles {
		if lines, err := iniLines(filepath.Join(root, f)); err == nil {
			if u := gdmAuto(lines); u != "" {
				al.User = u
				al.Files = append(al.Files, filepath.Join(root, f))
			}
		}
	}
	for _, f := range lightdmFiles(root) {
		if lines, err := iniLines(f); err == nil {
			if u := lightdmAuto(lines); u != "" {
				al.User = u
				al.Files = append(al.Files, f)
			}
		}
	}
	if al.User == "" {
		return nil
	}
	return &al
}

// disableLines renvoie le contenu du fichier sans ouverture automatique :
// GDM passe les interrupteurs à False, LightDM voit ses lignes
// autologin-user mises en commentaire. Le reste est laissé tel quel.
func disableLines(lines []string, gdm bool) []string {
	out := make([]string, 0, len(lines))
	sec := ""
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			sec = strings.ToLower(t)
		}
		k, _, isKV := strings.Cut(t, "=")
		k = strings.TrimSpace(k)
		switch {
		case !isKV || strings.HasPrefix(t, "#"):
		case gdm && sec == "[daemon]" && (k == "AutomaticLoginEnable" || k == "TimedLoginEnable"):
			l = k + "=False"
		case !gdm && k == "autologin-user":
			l = "#" + l + "  # désactivé par Bernard après la migration"
		}
		out = append(out, l)
	}
	return out
}

// DisableAutoLogin coupe l'ouverture de session automatique. Chaque fichier
// modifié est d'abord sauvegardé dans StateDir ; la fonction renvoie les
// paires (fichier, sauvegarde) pour l'annulation.
func (a *Applier) DisableAutoLogin(root string) ([][2]string, error) {
	al := DetectAutoLogin(root)
	if al == nil {
		return nil, ErrSkipped
	}
	if err := os.MkdirAll(a.StateDir, 0o700); err != nil {
		return nil, err
	}
	var done [][2]string
	for i, f := range al.Files {
		lines, err := iniLines(f)
		if err != nil {
			return done, err
		}
		fi, err := os.Stat(f)
		if err != nil {
			return done, err
		}
		orig, _ := os.ReadFile(f)
		backup := filepath.Join(a.StateDir, "autologin-"+strconv.Itoa(i)+"-"+filepath.Base(f))
		if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(backup, orig, 0o600); err != nil {
				return done, err
			}
		}
		isGDM := strings.Contains(f, "/gdm")
		content := strings.Join(disableLines(lines, isGDM), "\n") + "\n"
		tmp := f + ".bernard-tmp"
		if err := os.WriteFile(tmp, []byte(content), fi.Mode().Perm()); err != nil {
			return done, err
		}
		if err := os.Rename(tmp, f); err != nil {
			os.Remove(tmp)
			return done, err
		}
		done = append(done, [2]string{f, backup})
	}
	return done, nil
}

// RestoreFile remet un fichier de configuration sauvegardé.
func RestoreFile(file, backup string) error {
	b, err := os.ReadFile(backup)
	if err != nil {
		return err
	}
	fi, err := os.Stat(file)
	mode := os.FileMode(0o644)
	if err == nil {
		mode = fi.Mode().Perm()
	}
	return os.WriteFile(file, b, mode)
}
