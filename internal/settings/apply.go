package settings

import (
	"bufio"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bernard-linux/bernard/internal/i18n"
	"github.com/bernard-linux/bernard/internal/sysexec"
	"github.com/bernard-linux/bernard/internal/transfer"
)

// ErrSkipped signale un réglage volontairement non appliqué (déjà présent,
// ou à faire à la main) ; ce n'est pas une panne. Le bilan et l'interface
// retirent le préfixe « non appliqué : » (« not applied : ») des raisons.
var ErrSkipped = i18n.NewError("non appliqué")

var loginRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// Applier applique les réglages. Exec est remplacé dans les tests.
type Applier struct {
	Exec     sysexec.Executor
	StateDir string // sauvegardes pour l'annulation
	NMDir    string // /etc/NetworkManager/system-connections
}

// New renvoie l'Applier réel.
func New(stateDir string) *Applier {
	return &Applier{Exec: sysexec.Run, StateDir: stateDir, NMDir: "/etc/NetworkManager/system-connections"}
}

// asUser lance une commande avec l'identité d'un utilisateur, dans une
// session D-Bus privée (dconf écrit par son service D-Bus, même si
// l'utilisateur n'est pas connecté).
func (a *Applier) asUser(ctx context.Context, login, home, stdin string, args ...string) (string, error) {
	if !loginRe.MatchString(login) {
		return "", i18n.Errorf("identifiant refusé : %q", login)
	}
	full := append([]string{"-u", login, "--", "env", "HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"dbus-run-session", "--"}, args...)
	return a.Exec(ctx, sysexec.Cmd{Name: "runuser", Args: full, Stdin: stdin})
}

// ApplyDconf sauvegarde les réglages actuels du compte puis charge ceux de
// la source. Renvoie le chemin de la sauvegarde, pour l'annulation.
func (a *Applier) ApplyDconf(ctx context.Context, login, home string, d Dump, resets ...string) (string, error) {
	ini := d.String()
	if strings.TrimSpace(ini) == "" {
		return "", ErrSkipped
	}
	current, err := a.asUser(ctx, login, home, "", "dconf", "dump", "/")
	if err != nil {
		return "", i18n.Errorf("lecture des réglages actuels de %s : %w", login, err)
	}
	backup := filepath.Join(a.StateDir, "dconf-"+login+".ini")
	if err := os.MkdirAll(a.StateDir, 0o700); err != nil {
		return "", err
	}
	// Une sauvegarde existante (session précédente) est la vraie version
	// d'origine : on ne l'écrase pas.
	if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(backup, []byte(current), 0o600); err != nil {
			return "", err
		}
	}
	for _, p := range resets {
		if strings.Contains(p, "..") {
			continue
		}
		a.asUser(ctx, login, home, "", "dconf", "reset", "-f", "/"+strings.Trim(p, "/")+"/")
	}
	if _, err := a.asUser(ctx, login, home, ini, "dconf", "load", "/"); err != nil {
		return backup, err
	}
	return backup, nil
}

// RestoreDconf remet les réglages sauvegardés avant la migration.
func (a *Applier) RestoreDconf(ctx context.Context, login, home, backup string) error {
	b, err := os.ReadFile(backup)
	if err != nil {
		return err
	}
	if _, err := a.asUser(ctx, login, home, "", "dconf", "reset", "-f", "/"); err != nil {
		return err
	}
	if len(strings.TrimSpace(string(b))) > 0 {
		_, err = a.asUser(ctx, login, home, string(b), "dconf", "load", "/")
	}
	return err
}

// ---------------------------------------------------------------- Wi-Fi

// Types de connexions NetworkManager reprises : Wi-Fi et VPN (dont
// WireGuard). Les connexions filaires restent propres à chaque machine.
func ConnType(typ string) string {
	switch typ {
	case "wifi", "802-11-wireless":
		return "wifi"
	case "vpn", "wireguard":
		return "vpn"
	}
	return ""
}

// Greffons VPN de NetworkManager : type de service → paquet Debian/Ubuntu.
var vpnPlugins = map[string]string{
	"org.freedesktop.NetworkManager.openvpn":     "network-manager-openvpn-gnome",
	"org.freedesktop.NetworkManager.openconnect": "network-manager-openconnect-gnome",
	"org.freedesktop.NetworkManager.vpnc":        "network-manager-vpnc-gnome",
	"org.freedesktop.NetworkManager.pptp":        "network-manager-pptp-gnome",
	"org.freedesktop.NetworkManager.l2tp":        "network-manager-l2tp-gnome",
	"org.freedesktop.NetworkManager.strongswan":  "network-manager-strongswan",
	"org.freedesktop.NetworkManager.fortisslvpn": "network-manager-fortisslvpn-gnome",
	"org.freedesktop.NetworkManager.sstp":        "network-manager-sstp-gnome",
}

// VPNPlugin renvoie le paquet du greffon qu'une connexion VPN demande
// (vide pour WireGuard, géré par NetworkManager lui-même).
func VPNPlugin(content string) string {
	section := ""
	for _, l := range strings.Split(content, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			section = t
			continue
		}
		if v, ok := strings.CutPrefix(t, "service-type="); ok && section == "[vpn]" {
			return vpnPlugins[v]
		}
	}
	return ""
}

// SanitizeWifi vérifie qu'un fichier NetworkManager est bien une connexion
// Wi-Fi ou VPN et retire ce qui dépend de l'ancienne machine (nom de la
// carte Wi-Fi, restriction à un compte absent). Renvoie le contenu nettoyé,
// le nom et l'UUID de la connexion.
func SanitizeWifi(content string, userExists func(string) bool) (string, string, string, error) {
	var out []string
	section, typ, id, uuid := "", "", "", ""
	iface := -1
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		line := sc.Text()
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			section = t
			out = append(out, line)
			continue
		}
		k, v, _ := strings.Cut(t, "=")
		if section == "[connection]" {
			switch k {
			case "type":
				typ = v
			case "id":
				id = v
			case "uuid":
				uuid = v
			case "interface-name":
				// Nom de la carte Wi-Fi : différent ici. Pour WireGuard, c'est
				// le nom de l'interface créée : on le garde.
				iface = len(out)
			case "permissions":
				if !permissionsOK(v, userExists) {
					continue // réservée à un compte absent : ouverte à tous
				}
			}
		}
		out = append(out, line)
	}
	if ConnType(typ) == "" {
		return "", "", "", i18n.Errorf("%w : pas une connexion Wi-Fi ni VPN", ErrSkipped)
	}
	if iface >= 0 && ConnType(typ) == "wifi" {
		out = append(out[:iface], out[iface+1:]...)
	}
	if id == "" || !regexp.MustCompile(`^[0-9a-fA-F-]{36}$`).MatchString(uuid) {
		return "", "", "", errors.New(i18n.T("connexion réseau incomplète"))
	}
	return strings.Join(out, "\n") + "\n", id, uuid, nil
}

func permissionsOK(v string, userExists func(string) bool) bool {
	for _, p := range strings.Split(v, ";") {
		if u, ok := strings.CutPrefix(p, "user:"); ok && u != "" && (userExists == nil || !userExists(u)) {
			return false
		}
	}
	return true
}

var safeFile = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// InstallWifi ajoute une connexion Wi-Fi si aucune connexion de même UUID
// n'existe. Renvoie le fichier créé, pour l'annulation.
func (a *Applier) InstallWifi(ctx context.Context, c NMConnection, userExists func(string) bool) (string, error) {
	content, id, uuid, err := SanitizeWifi(c.Content, userExists)
	if err != nil {
		return "", err
	}
	if out, err := a.Exec(ctx, sysexec.Cmd{Name: "nmcli", Args: []string{"-t", "-f", "UUID", "connection", "show"}}); err == nil {
		for _, l := range sysexec.Lines(out) {
			if l == uuid {
				return "", i18n.Errorf("%w : « %s » existe déjà", ErrSkipped, id)
			}
		}
	}
	name := safeFile.ReplaceAllString(id, "_")
	path := filepath.Join(a.NMDir, "bernard-"+name+".nmconnection")
	if err := os.MkdirAll(a.NMDir, 0o755); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	f.Close()
	a.Exec(ctx, sysexec.Cmd{Name: "nmcli", Args: []string{"connection", "reload"}})
	return path, nil
}

// RemoveWifi retire une connexion ajoutée par Bernard.
func (a *Applier) RemoveWifi(ctx context.Context, path string) error {
	if filepath.Dir(path) != a.NMDir || !strings.HasPrefix(filepath.Base(path), "bernard-") {
		return i18n.Errorf("fichier refusé : %s", path)
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	a.Exec(ctx, sysexec.Cmd{Name: "nmcli", Args: []string{"connection", "reload"}})
	return nil
}

// ---------------------------------------------------------------- crontab

// InstallCrontab reprend les tâches planifiées d'un compte, s'il n'en a pas
// déjà sur la cible.
func (a *Applier) InstallCrontab(ctx context.Context, login, content string) error {
	if !loginRe.MatchString(login) {
		return i18n.Errorf("identifiant refusé : %q", login)
	}
	if strings.TrimSpace(content) == "" {
		return ErrSkipped
	}
	if out, err := a.Exec(ctx, sysexec.Cmd{Name: "crontab", Args: []string{"-u", login, "-l"}}); err == nil && strings.TrimSpace(out) != "" {
		return i18n.Errorf("%w : %s a déjà des tâches planifiées ici", ErrSkipped, login)
	}
	_, err := a.Exec(ctx, sysexec.Cmd{Name: "crontab", Args: []string{"-u", login, "-"}, Stdin: content})
	return err
}

// RemoveCrontab retire les tâches reprises par Bernard.
func (a *Applier) RemoveCrontab(ctx context.Context, login string) error {
	if !loginRe.MatchString(login) {
		return i18n.Errorf("identifiant refusé : %q", login)
	}
	_, err := a.Exec(ctx, sysexec.Cmd{Name: "crontab", Args: []string{"-u", login, "-r"}})
	return err
}

// ---------------------------------------------------------------- imprimantes

var printerRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,127}$`)

// Imprimantes réseau réinstallables sans pilote (IPP Everywhere / AirPrint).
var networkSchemes = []string{"ipp://", "ipps://", "dnssd://", "http://", "https://"}

// AddPrinter réinstalle une imprimante réseau en mode sans pilote. Les
// imprimantes USB ou à pilote propriétaire restent à faire à la main.
func (a *Applier) AddPrinter(ctx context.Context, p Printer) error {
	if !printerRe.MatchString(p.Name) {
		return i18n.Errorf("nom d'imprimante refusé : %q", p.Name)
	}
	network := false
	for _, s := range networkSchemes {
		if strings.HasPrefix(p.URI, s) {
			network = true
		}
	}
	if !network || strings.ContainsAny(p.URI, " \n") {
		return i18n.Errorf("%w : %s n'est pas une imprimante réseau, à installer depuis les réglages d'impression", ErrSkipped, p.Name)
	}
	if _, err := a.Exec(ctx, sysexec.Cmd{Name: "lpstat", Args: []string{"-p", p.Name}}); err == nil {
		return i18n.Errorf("%w : %s existe déjà", ErrSkipped, p.Name)
	}
	if _, err := a.Exec(ctx, sysexec.Cmd{Name: "lpadmin", Args: []string{"-p", p.Name, "-E", "-v", p.URI, "-m", "everywhere"}}); err != nil {
		return err
	}
	if p.Default {
		a.Exec(ctx, sysexec.Cmd{Name: "lpadmin", Args: []string{"-d", p.Name}})
	}
	return nil
}

// RemovePrinter retire une imprimante ajoutée par Bernard.
func (a *Applier) RemovePrinter(ctx context.Context, name string) error {
	if !printerRe.MatchString(name) {
		return i18n.Errorf("nom d'imprimante refusé : %q", name)
	}
	_, err := a.Exec(ctx, sysexec.Cmd{Name: "lpadmin", Args: []string{"-x", name}})
	return err
}

// ---------------------------------------------------------------- dossier neuf

// ClearPristineSkeleton retire d'un dossier personnel NEUF les fichiers
// modèles (.bashrc, .profile…) restés identiques à ceux de /etc/skel, pour
// que les versions de l'ancien ordinateur prennent leur place au lieu d'être
// renommées. Un fichier modifié, même d'un octet, est conservé. Ces modèles
// sont régénérables à tout moment depuis /etc/skel.
func ClearPristineSkeleton(home, skel string) ([]string, error) {
	var removed []string
	err := filepath.WalkDir(skel, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(skel, p)
		dst := filepath.Join(home, rel)
		fi, err := os.Lstat(dst)
		if err != nil || !fi.Mode().IsRegular() {
			return nil
		}
		a, _, err1 := transfer.HashFile(p)
		b, _, err2 := transfer.HashFile(dst)
		if err1 == nil && err2 == nil && a == b {
			if os.Remove(dst) == nil {
				removed = append(removed, rel)
			}
		}
		return nil
	})
	return removed, err
}

// ---------------------------------------------------------------- polices

// RebuildFontCache refait l'index des polices d'un compte migré. Cet index
// (~/.cache/fontconfig) est propre à chaque machine : un index qui ne
// correspond pas aux polices présentes fait planter l'affichage web de
// WebKit (fenêtre de Bernard, aide Yelp, Evolution restent blancs), alors que
// les autres applications le tolèrent. Il n'est jamais copié ; celui qui a
// pu se créer ici pendant la migration est jeté, puis reconstruit avec les
// polices réellement présentes (celles du système et celles du compte).
func (a *Applier) RebuildFontCache(ctx context.Context, login, home string) error {
	if !loginRe.MatchString(login) {
		return i18n.Errorf("identifiant refusé : %q", login)
	}
	for _, rel := range []string{".cache/fontconfig", ".fontconfig"} {
		p := filepath.Join(home, rel)
		// Jamais à travers un lien symbolique (~/.cache peut en être un).
		if fi, err := os.Lstat(filepath.Dir(p)); err != nil || fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if fi, err := os.Lstat(p); err == nil && fi.IsDir() {
			if rel == ".fontconfig" && !onlyFontCaches(p) {
				continue // ancien emplacement : on ne jette que des index
			}
			os.RemoveAll(p)
		}
	}
	_, err := a.Exec(ctx, sysexec.Cmd{Name: "runuser", Args: []string{"-u", login, "--", "env",
		"HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, ".cache"), "fc-cache", "-f"}})
	return err
}

// onlyFontCaches : le dossier ne contient que des index de fontconfig
// (*.cache-N, CACHEDIR.TAG), ancien emplacement ~/.fontconfig.
func onlyFontCaches(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !(strings.Contains(n, ".cache-") || n == "CACHEDIR.TAG") {
			return false
		}
	}
	return true
}
