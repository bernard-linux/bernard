// Package settings migre les réglages qui ne sont pas de simples fichiers :
// préférences du bureau (dconf), réseaux Wi-Fi, tâches planifiées,
// imprimantes réseau, et prépare les dossiers personnels neufs.
package settings

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Extras sont les réglages lus sur la source par l'agent administrateur.
// Ils contiennent des secrets (mots de passe Wi-Fi) : ils ne transitent que
// par la session chiffrée et ne sont jamais écrits dans l'inventaire.
type Extras struct {
	Desktop  string            `json:"desktop"`
	Dconf    map[string]string `json:"dconf,omitempty"`    // identifiant → sortie de « dconf dump / »
	Crontabs map[string]string `json:"crontabs,omitempty"` // identifiant → crontab
	Wifi     []NMConnection    `json:"wifi,omitempty"`
	Printers []Printer         `json:"printers,omitempty"`
	Warnings []string          `json:"warnings,omitempty"`
}

// NMConnection est un fichier de connexion NetworkManager.
type NMConnection struct {
	File    string `json:"file"`
	Content string `json:"content"`
}

// Printer est une file d'impression CUPS.
type Printer struct {
	Name    string `json:"name"`
	URI     string `json:"uri"`
	Default bool   `json:"default,omitempty"`
}

// Dump est une sortie « dconf dump » : chemin → clé → valeur (texte GVariant).
type Dump map[string]map[string]string

// ParseDump lit le format INI de « dconf dump / ».
func ParseDump(s string) Dump {
	d := Dump{}
	var cur string
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			cur = strings.Trim(line[1:len(line)-1], "/")
		case cur != "":
			if k, v, ok := strings.Cut(line, "="); ok {
				if d[cur] == nil {
					d[cur] = map[string]string{}
				}
				d[cur][k] = v
			}
		}
	}
	return d
}

// String réécrit le format INI, trié pour être stable.
func (d Dump) String() string {
	var paths []string
	for p := range d {
		if len(d[p]) > 0 {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	var b strings.Builder
	for _, p := range paths {
		b.WriteString("[" + p + "]\n")
		var keys []string
		for k := range d[p] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(k + "=" + d[p][k] + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (d Dump) set(path, key, val string) {
	if d[path] == nil {
		d[path] = map[string]string{}
	}
	d[path][key] = val
}

// Réglages repris entre bureaux identiques. Liste fermée : on reprend ce
// qui fait « son » bureau (fond, clavier, souris, polices, dock, veille…),
// pas les réglages internes qui pourraient casser une autre version.
var gnomeKeep = []string{
	"org/gnome/desktop/background",
	"org/gnome/desktop/screensaver",
	"org/gnome/desktop/interface",
	"org/gnome/desktop/input-sources",
	"org/gnome/desktop/peripherals",
	"org/gnome/desktop/wm/preferences",
	"org/gnome/desktop/wm/keybindings",
	"org/gnome/desktop/privacy",
	"org/gnome/desktop/sound",
	"org/gnome/desktop/calendar",
	"org/gnome/desktop/a11y",
	"org/gnome/desktop/session",
	"org/gnome/settings-daemon/plugins/power",
	"org/gnome/settings-daemon/plugins/media-keys",
	"org/gnome/shell/extensions", // réglages des extensions (dont le tableau de bord de Zorin)
	"org/gnome/mutter",
	"org/gnome/desktop/notifications",
	"org/gnome/desktop/search-providers",
	"org/gnome/desktop/datetime",
	"org/gnome/system/location",
	"org/gnome/nautilus/preferences",
	"org/gnome/nautilus/list-view",
	"org/gnome/nautilus/icon-view",
	"org/gnome/terminal/legacy",
	"org/gnome/gedit/preferences",
	"org/gnome/TextEditor",
}

var cinnamonKeep = []string{
	"org/cinnamon/desktop/background",
	"org/cinnamon/desktop/screensaver",
	"org/cinnamon/desktop/interface",
	"org/cinnamon/desktop/peripherals",
	"org/cinnamon/desktop/wm/preferences",
	"org/cinnamon/desktop/keybindings",
	"org/cinnamon/desktop/privacy",
	"org/cinnamon/desktop/sound",
	"org/cinnamon/desktop/a11y",
	"org/cinnamon/desktop/session",
	"org/cinnamon/settings-daemon/plugins/power",
	"org/gnome/libgnomekbd/keyboard",
	"org/nemo/preferences",
	"org/gnome/terminal/legacy",
	"org/x/editor/preferences",
}

// Correspondances entre bureaux (préfixe de chemin GNOME ↔ Cinnamon).
var gnomeToCinnamon = map[string]string{
	"org/gnome/desktop/background":            "org/cinnamon/desktop/background",
	"org/gnome/desktop/screensaver":           "org/cinnamon/desktop/screensaver",
	"org/gnome/desktop/peripherals":           "org/cinnamon/desktop/peripherals",
	"org/gnome/desktop/wm/preferences":        "org/cinnamon/desktop/wm/preferences",
	"org/gnome/desktop/privacy":               "org/cinnamon/desktop/privacy",
	"org/gnome/desktop/a11y":                  "org/cinnamon/desktop/a11y",
	"org/gnome/settings-daemon/plugins/power": "org/cinnamon/settings-daemon/plugins/power",
	"org/gnome/terminal/legacy":               "org/gnome/terminal/legacy",
}

// Clés d'interface communes aux deux bureaux (même nom, même sens).
var sharedInterfaceKeys = map[string]bool{
	"text-scaling-factor": true, "cursor-size": true, "font-name": true,
	"monospace-font-name": true, "gtk-theme": true, "icon-theme": true, "cursor-theme": true,
}

// Clés isolées reprises dans des chemins dont le reste est interne au bureau
// (historique, versions vues…) : dock, extensions et applets actifs.
var gnomeKeepKeys = map[string][]string{
	"org/gnome/shell": {"favorite-apps", "enabled-extensions", "disabled-extensions", "disable-user-extensions"},
}

var cinnamonKeepKeys = map[string][]string{
	"org/cinnamon": {"favorite-apps", "enabled-applets", "enabled-desklets", "enabled-extensions",
		"panels-enabled", "panels-height", "panels-autohide", "panels-hide-delay", "panels-show-delay", "panel-zone-icon-sizes"},
}

// keepKeys copie les clés retenues de chaque chemin.
func keepKeys(in, out Dump, keys map[string][]string) {
	for p, ks := range keys {
		for _, k := range ks {
			if v, ok := in[p][k]; ok {
				out.set(p, k, v)
			}
		}
	}
}

func keep(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// Fidelity décrit la qualité attendue d'une reprise de réglages.
func Fidelity(src, dst string) string {
	switch {
	case src == dst && (src == "gnome" || src == "cinnamon"):
		return "full"
	case (src == "gnome" && dst == "cinnamon") || (src == "cinnamon" && dst == "gnome"):
		return "substitute"
	}
	return "none"
}

// Chemins propres au clavier physique : repris seulement si l'utilisateur
// le demande, ou si les deux ordinateurs ont la même disposition.
var keyboardPaths = []string{"org/gnome/desktop/input-sources", "org/gnome/libgnomekbd/keyboard"}

// Chemins propres au matériel, jamais repris : profils de couleur des écrans.
var hardwarePaths = []string{"org/gnome/settings-daemon/plugins/color", "org/cinnamon/settings-daemon/plugins/color"}

// Hardware dit comment traiter ce qui tient au matériel de la cible.
type Hardware struct {
	// KeepKeyboard reprend la disposition du clavier de la source.
	KeepKeyboard bool
	// TargetKeyboard est la disposition du système cible (« be »,
	// « be,fr+bepo »), imposée au compte quand KeepKeyboard est faux.
	TargetKeyboard string
}

// Resets renvoie les chemins dconf à vider avant de charger les réglages.
// Nécessaire parce que le dossier personnel copié contient déjà la base
// dconf de l'ancien ordinateur (~/.config/dconf/user), avec sa disposition
// de clavier et ses profils d'écran.
func (h Hardware) Resets() []string {
	out := append([]string{}, hardwarePaths...)
	if !h.KeepKeyboard {
		out = append(out, keyboardPaths...)
	}
	return out
}

// Translate filtre (et traduit si besoin) une sortie dconf de la source pour
// le bureau de la cible. themeExists écarte les thèmes absents de la cible.
// Sauf si hw.KeepKeyboard, la disposition du clavier est celle de la cible.
func Translate(in Dump, src, dst string, themeExists func(kind, name string) bool, hw Hardware) Dump {
	out := translate(in, src, dst, themeExists)
	for p := range out {
		if keep(p, hardwarePaths) {
			delete(out, p)
		}
	}
	if hw.KeepKeyboard {
		return out
	}
	opts := out["org/gnome/desktop/input-sources"]["xkb-options"]
	for p := range out {
		if keep(p, keyboardPaths) {
			delete(out, p)
		}
	}
	if opts != "" {
		out.set("org/gnome/desktop/input-sources", "xkb-options", opts) // options (touche compose…) : préférences de l'utilisateur
	}
	var layouts []string
	for _, l := range strings.Split(hw.TargetKeyboard, ",") {
		if l = strings.TrimSpace(l); l != "" {
			layouts = append(layouts, l)
		}
	}
	if len(layouts) == 0 {
		return out // inconnue : le bureau prendra celle du système
	}
	switch dst {
	case "gnome":
		var srcs []string
		for _, l := range layouts {
			srcs = append(srcs, "('xkb', '"+escapeGV(l)+"')")
		}
		v := "[" + strings.Join(srcs, ", ") + "]"
		out.set("org/gnome/desktop/input-sources", "sources", v)
		out.set("org/gnome/desktop/input-sources", "mru-sources", v)
	case "cinnamon":
		var ls []string
		for _, l := range layouts {
			ls = append(ls, strings.ReplaceAll(l, "+", `\t`))
		}
		out.set("org/gnome/libgnomekbd/keyboard", "layouts", gvStrings(ls))
	}
	return out
}

func translate(in Dump, src, dst string, themeExists func(kind, name string) bool) Dump {
	out := Dump{}
	switch {
	case src == dst && src == "gnome":
		for p, kv := range in {
			if keep(p, gnomeKeep) {
				out[p] = copyMap(kv)
			}
		}
		keepKeys(in, out, gnomeKeepKeys)
	case src == dst && src == "cinnamon":
		for p, kv := range in {
			if keep(p, cinnamonKeep) {
				out[p] = copyMap(kv)
			}
		}
		keepKeys(in, out, cinnamonKeepKeys)
	case src == "gnome" && dst == "cinnamon":
		for p, kv := range in {
			for from, to := range gnomeToCinnamon {
				if p == from || strings.HasPrefix(p, from+"/") {
					out[to+strings.TrimPrefix(p, from)] = copyMap(kv)
				}
			}
		}
		for k, v := range in["org/gnome/desktop/interface"] {
			if sharedInterfaceKeys[k] {
				out.set("org/cinnamon/desktop/interface", k, v)
			}
		}
		if layouts := gnomeLayouts(in); len(layouts) > 0 {
			out.set("org/gnome/libgnomekbd/keyboard", "layouts", gvStrings(layouts))
		}
		if fav, ok := in["org/gnome/shell"]["favorite-apps"]; ok {
			out.set("org/cinnamon", "favorite-apps", fav)
		}
	case src == "cinnamon" && dst == "gnome":
		for p, kv := range in {
			for from, to := range gnomeToCinnamon {
				if p == to || strings.HasPrefix(p, to+"/") {
					out[from+strings.TrimPrefix(p, to)] = copyMap(kv)
				}
			}
		}
		for k, v := range in["org/cinnamon/desktop/interface"] {
			if sharedInterfaceKeys[k] {
				out.set("org/gnome/desktop/interface", k, v)
			}
		}
		if layouts := parseGVStrings(in["org/gnome/libgnomekbd/keyboard"]["layouts"]); len(layouts) > 0 {
			var srcs []string
			for _, l := range layouts {
				srcs = append(srcs, "('xkb', '"+escapeGV(strings.ReplaceAll(l, `\t`, "+"))+"')")
			}
			out.set("org/gnome/desktop/input-sources", "sources", "["+strings.Join(srcs, ", ")+"]")
		}
		if fav, ok := in["org/cinnamon"]["favorite-apps"]; ok {
			out.set("org/gnome/shell", "favorite-apps", fav)
		}
	}
	dropMissingThemes(out, themeExists)
	return out
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

var themeKeys = map[string]string{"gtk-theme": "gtk", "icon-theme": "icons", "cursor-theme": "icons"}

func dropMissingThemes(d Dump, exists func(kind, name string) bool) {
	if exists == nil {
		return
	}
	for p, kv := range d {
		if !strings.HasSuffix(p, "desktop/interface") {
			continue
		}
		for k, kind := range themeKeys {
			if v, ok := kv[k]; ok {
				if names := parseGVStrings("[" + v + "]"); len(names) == 1 && !exists(kind, names[0]) {
					delete(kv, k)
				}
			}
		}
	}
}

// ThemeExists cherche un thème dans les dossiers système et dans le dossier
// personnel (déjà copié à ce stade).
func ThemeExists(home string) func(kind, name string) bool {
	return func(kind, name string) bool {
		dirs := []string{"/usr/share/icons", filepath.Join(home, ".icons"), filepath.Join(home, ".local/share/icons")}
		if kind == "gtk" {
			dirs = []string{"/usr/share/themes", filepath.Join(home, ".themes"), filepath.Join(home, ".local/share/themes")}
		}
		for _, d := range dirs {
			if fi, err := os.Stat(filepath.Join(d, name)); err == nil && fi.IsDir() {
				return true
			}
		}
		return false
	}
}

var gvString = regexp.MustCompile(`'((?:[^'\\]|\\.)*)'`)

// parseGVStrings extrait les chaînes d'une liste GVariant ['a', 'b'].
func parseGVStrings(v string) []string {
	var out []string
	for _, m := range gvString.FindAllStringSubmatch(v, -1) {
		out = append(out, strings.ReplaceAll(m[1], `\'`, `'`))
	}
	return out
}

func escapeGV(s string) string { return strings.ReplaceAll(s, `'`, `\'`) }

func gvStrings(items []string) string {
	var q []string
	for _, s := range items {
		q = append(q, "'"+escapeGV(s)+"'")
	}
	return "[" + strings.Join(q, ", ") + "]"
}

var xkbSource = regexp.MustCompile(`\('xkb',\s*'([^']+)'\)`)

// gnomeLayouts lit les dispositions de clavier GNOME ([('xkb', 'be'), …]) et
// les renvoie au format libgnomekbd de Cinnamon ('be', 'fr\tbepo').
func gnomeLayouts(in Dump) []string {
	var out []string
	for _, m := range xkbSource.FindAllStringSubmatch(in["org/gnome/desktop/input-sources"]["sources"], -1) {
		// Variante « fr+bepo » (GNOME) → « fr\tbepo » (libgnomekbd), écrit
		// avec la séquence d'échappement GVariant.
		out = append(out, strings.ReplaceAll(m[1], "+", `\t`))
	}
	return out
}
