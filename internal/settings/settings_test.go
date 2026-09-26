package settings

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bernard-linux/bernard/internal/sysexec"
)

const gnomeDump = `[org/gnome/desktop/background]
picture-uri='file:///home/alice/Images/plage.jpg'

[org/gnome/desktop/input-sources]
sources=[('xkb', 'be'), ('xkb', 'fr+bepo')]

[org/gnome/desktop/interface]
clock-format='24h'
text-scaling-factor=1.25
gtk-theme='Theme-Absent'
icon-theme='Adwaita'

[org/gnome/shell]
favorite-apps=['firefox.desktop', 'org.gnome.Nautilus.desktop']

[org/gnome/shell/extensions/dash-to-dock]
dock-position='BOTTOM'

[org/gnome/evolution-data-server]
migrated=true
`

func themes(kind, name string) bool { return name == "Adwaita" }

func TestGnomeToGnomeKeepsOnlyWhitelist(t *testing.T) {
	out := Translate(ParseDump(gnomeDump), "gnome", "gnome", themes)
	if out["org/gnome/desktop/background"]["picture-uri"] == "" {
		t.Error("fond d'écran perdu")
	}
	if out["org/gnome/desktop/input-sources"]["sources"] == "" {
		t.Error("clavier perdu")
	}
	if _, ok := out["org/gnome/shell/extensions/dash-to-dock"]; ok {
		t.Error("les réglages d'extensions ne doivent pas être repris (dock de Zorin)")
	}
	if _, ok := out["org/gnome/evolution-data-server"]; ok {
		t.Error("réglage interne repris à tort")
	}
	if _, ok := out["org/gnome/desktop/interface"]["gtk-theme"]; ok {
		t.Error("un thème absent de la cible ne doit pas être appliqué")
	}
	if out["org/gnome/desktop/interface"]["icon-theme"] != "'Adwaita'" {
		t.Error("un thème présent doit être gardé")
	}
}

func TestGnomeToCinnamon(t *testing.T) {
	out := Translate(ParseDump(gnomeDump), "gnome", "cinnamon", themes)
	if out["org/cinnamon/desktop/background"]["picture-uri"] != "'file:///home/alice/Images/plage.jpg'" {
		t.Errorf("fond d'écran non traduit : %v", out)
	}
	if got := out["org/gnome/libgnomekbd/keyboard"]["layouts"]; got != `['be', 'fr\tbepo']` {
		t.Errorf("claviers mal traduits : %s", got)
	}
	if out["org/cinnamon/desktop/interface"]["text-scaling-factor"] != "1.25" {
		t.Error("taille du texte perdue")
	}
	if out["org/cinnamon"]["favorite-apps"] == "" {
		t.Error("favoris perdus")
	}
	// Aller-retour : on retrouve les claviers GNOME.
	back := Translate(ParseDump(out.String()), "cinnamon", "gnome", themes)
	if got := back["org/gnome/desktop/input-sources"]["sources"]; got != "[('xkb', 'be'), ('xkb', 'fr+bepo')]" {
		t.Errorf("aller-retour des claviers : %s", got)
	}
}

func TestUnknownDesktopTransfersNothing(t *testing.T) {
	if out := Translate(ParseDump(gnomeDump), "gnome", "kde", nil); len(out) != 0 {
		t.Errorf("rien ne doit être appliqué vers un bureau non pris en charge : %v", out)
	}
	if Fidelity("gnome", "kde") != "none" || Fidelity("gnome", "cinnamon") != "substitute" {
		t.Error("fidélité inattendue")
	}
}

const wifiFile = `[connection]
id=Maison
uuid=0b5b7a3e-9f2a-4a38-9d6e-2f3c1a7b8e11
type=wifi
interface-name=wlp2s0
permissions=user:ancien;

[wifi]
ssid=Maison

[wifi-security]
key-mgmt=wpa-psk
psk=secret-du-wifi
`

func TestSanitizeWifi(t *testing.T) {
	out, id, uuid, err := SanitizeWifi(wifiFile, func(string) bool { return false })
	if err != nil || id != "Maison" || uuid == "" {
		t.Fatalf("%v %s %s", err, id, uuid)
	}
	if strings.Contains(out, "interface-name") || strings.Contains(out, "permissions") {
		t.Errorf("éléments liés à l'ancienne machine conservés :\n%s", out)
	}
	if !strings.Contains(out, "psk=secret-du-wifi") {
		t.Error("le mot de passe du réseau doit être repris")
	}
	if _, _, _, err := SanitizeWifi(strings.Replace(wifiFile, "type=wifi", "type=vpn", 1), nil); !errors.Is(err, ErrSkipped) {
		t.Error("une connexion non Wi-Fi doit être écartée")
	}
}

type fake struct {
	cmds []sysexec.Cmd
	out  map[string]string
}

func (f *fake) exec(_ context.Context, c sysexec.Cmd) (string, error) {
	f.cmds = append(f.cmds, c)
	key := c.Name + " " + strings.Join(c.Args, " ")
	for k, v := range f.out {
		if strings.HasSuffix(key, k) {
			if v == "ERR" {
				return "", errors.New("échec")
			}
			return v, nil
		}
	}
	return "", nil
}

func TestApplyDconfBacksUpFirst(t *testing.T) {
	f := &fake{out: map[string]string{"dconf dump /": "[org/gnome/desktop/interface]\nclock-format='12h'\n"}}
	a := &Applier{Exec: f.exec, StateDir: t.TempDir()}
	backup, err := a.ApplyDconf(context.Background(), "alice", "/home/alice", ParseDump(gnomeDump))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(backup)
	if !strings.Contains(string(b), "12h") {
		t.Error("les réglages d'avant doivent être sauvegardés")
	}
	load := f.cmds[len(f.cmds)-1]
	if load.Name != "runuser" || !strings.Contains(strings.Join(load.Args, " "), "-u alice -- env HOME=/home/alice") ||
		!strings.Contains(load.Stdin, "picture-uri") {
		t.Errorf("chargement inattendu : %+v", load)
	}
	// Seconde exécution (reprise) : la sauvegarde d'origine n'est pas écrasée.
	f.out["dconf dump /"] = "[x]\ny=1\n"
	a.ApplyDconf(context.Background(), "alice", "/home/alice", ParseDump(gnomeDump))
	if b2, _ := os.ReadFile(backup); string(b2) != string(b) {
		t.Error("la sauvegarde d'origine a été écrasée")
	}
}

func TestInstallAndRemoveWifi(t *testing.T) {
	f := &fake{out: map[string]string{}}
	a := &Applier{Exec: f.exec, NMDir: t.TempDir()}
	path, err := a.InstallWifi(context.Background(), NMConnection{Content: wifiFile}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Error("le fichier contient un mot de passe : droits 600 exigés")
	}
	if _, err := a.InstallWifi(context.Background(), NMConnection{Content: wifiFile}, nil); err == nil {
		t.Error("un doublon ne doit pas écraser le fichier existant")
	}
	if err := a.RemoveWifi(context.Background(), "/etc/passwd"); err == nil {
		t.Error("l'annulation ne doit supprimer que ses propres fichiers")
	}
	if err := a.RemoveWifi(context.Background(), path); err != nil {
		t.Fatal(err)
	}
}

func TestPrintersAndCrontab(t *testing.T) {
	f := &fake{out: map[string]string{"lpstat -p Bureau": "ERR", "crontab -u bob -l": "0 * * * * sauvegarde\n", "crontab -u alice -l": "ERR"}}
	a := &Applier{Exec: f.exec}
	ctx := context.Background()
	if err := a.AddPrinter(ctx, Printer{Name: "Bureau", URI: "ipp://192.168.1.20/ipp/print", Default: true}); err != nil {
		t.Fatal(err)
	}
	if err := a.AddPrinter(ctx, Printer{Name: "Salon", URI: "usb://HP/DeskJet"}); !errors.Is(err, ErrSkipped) {
		t.Error("une imprimante USB doit être laissée à l'utilisateur")
	}
	if err := a.AddPrinter(ctx, Printer{Name: "x;rm", URI: "ipp://a"}); err == nil || errors.Is(err, ErrSkipped) {
		t.Error("nom dangereux accepté")
	}
	if err := a.InstallCrontab(ctx, "bob", "5 * * * * x\n"); !errors.Is(err, ErrSkipped) {
		t.Error("une crontab existante ne doit pas être remplacée")
	}
	if err := a.InstallCrontab(ctx, "alice", "5 * * * * x\n"); err != nil {
		t.Fatal(err)
	}
}

func TestClearPristineSkeleton(t *testing.T) {
	skel, home := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(skel, ".bashrc"), []byte("modèle"), 0o644)
	os.WriteFile(filepath.Join(skel, ".profile"), []byte("modèle"), 0o644)
	os.WriteFile(filepath.Join(home, ".bashrc"), []byte("modèle"), 0o644)
	os.WriteFile(filepath.Join(home, ".profile"), []byte("modèle modifié"), 0o644)
	removed, _ := ClearPristineSkeleton(home, skel)
	if len(removed) != 1 || removed[0] != ".bashrc" {
		t.Fatalf("seul le modèle intact doit partir : %v", removed)
	}
	if _, err := os.Stat(filepath.Join(home, ".profile")); err != nil {
		t.Fatal("un fichier modifié a été supprimé")
	}
}
