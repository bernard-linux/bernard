package apply

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/journal"
	"github.com/bernard-linux/bernard/internal/plan"
	"github.com/bernard-linux/bernard/internal/settings"
	"github.com/bernard-linux/bernard/internal/sysexec"
	"github.com/bernard-linux/bernard/internal/system"
)

type fakeSys struct {
	cmds      []string
	users     map[string]bool
	installed map[string]bool
	flatpak   bool
}

func (f *fakeSys) exec(_ context.Context, c sysexec.Cmd) (string, error) {
	line := c.Name + " " + strings.Join(c.Args, " ")
	f.cmds = append(f.cmds, line)
	no := errors.New("non")
	switch c.Name {
	case "getent":
		if c.Args[0] == "passwd" && f.users[c.Args[1]] {
			return "x", nil
		}
		return "", no
	case "useradd":
		f.users[c.Args[len(c.Args)-1]] = true
	case "dpkg-query":
		var b strings.Builder
		for p := range f.installed {
			b.WriteString(p + "\tii \n")
		}
		return b.String(), nil
	case "apt-get":
		if c.Args[0] == "install" {
			for _, p := range c.Args[3:] {
				f.installed[p] = true
			}
		}
	case "flatpak":
		if c.Args[0] == "--version" && !f.installed["flatpak"] {
			return "", no
		}
	case "lpstat", "crontab":
		if c.Args[0] == "-p" || c.Args[len(c.Args)-1] == "-l" {
			return "", no
		}
	}
	return "", nil
}

func (f *fakeSys) ran(prefix string) bool {
	for _, c := range f.cmds {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func setup(t *testing.T) (*fakeSys, *Applier, *plan.Plan, *inventory.Inventory, string) {
	f := &fakeSys{users: map[string]bool{"root": true}, installed: map[string]bool{}}
	jp := filepath.Join(t.TempDir(), "j.jsonl")
	j, _ := journal.Open(jp)
	t.Cleanup(func() { j.Close() })
	st, _ := journal.Load(jp)
	inv := &inventory.Inventory{
		Schema: inventory.Schema, Source: inventory.Source{OS: "linux"},
		Users: []inventory.User{{ID: "u1", Login: "arnaud", UID: 1000}},
	}
	p := &plan.Plan{Actions: []plan.Action{
		{Op: plan.OpCreateUser, From: "u1", Login: "arnaud", Password: "copyHash", Selected: true},
		{Op: plan.OpSetupFlatpak, Selected: true},
		{Op: plan.OpInstall, Via: "apt", Package: "gimp", Selected: true},
		{Op: plan.OpInstall, Via: "apt", Package: "inkscape", Selected: false},
		{Op: plan.OpInstall, Via: "flatpak", Package: "com.spotify.Client", Selected: true},
	}}
	a := &Applier{Sys: &system.System{Exec: f.exec}, Journal: j, State: st,
		Secrets: map[string]string{"arnaud": "$6$sel$hash"}}
	return f, a, p, inv, jp
}

func TestApplyAndUndo(t *testing.T) {
	f, a, p, inv, jp := setup(t)
	rep, err := a.System(context.Background(), p, inv)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.UsersCreated) != 1 || len(rep.Failed) != 0 {
		t.Fatalf("rapport : %+v", rep)
	}
	if f.installed["inkscape"] {
		t.Error("une action décochée a été exécutée")
	}
	if !f.ran("flatpak install --system --noninteractive --assumeyes flathub com.spotify.Client") {
		t.Error("Spotify non installé via Flathub")
	}

	// Relance (après coupure) : rien n'est refait.
	before := len(f.cmds)
	if _, err := a.System(context.Background(), p, inv); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.cmds[before:] {
		if strings.HasPrefix(c, "useradd") || strings.HasPrefix(c, "flatpak install") {
			t.Errorf("refait à tort lors de la relance : %s", c)
		}
	}

	st, _ := journal.Load(jp)
	u := UndoSystem(context.Background(), st, &system.System{Exec: f.exec}, &settings.Applier{Exec: f.exec})
	if len(u.Errors) > 0 {
		t.Fatalf("annulation : %v", u.Errors)
	}
	for _, want := range []string{"flatpak uninstall --system --noninteractive --assumeyes com.spotify.Client",
		"flatpak remote-delete --system flathub", "apt-get remove --yes -- ", "userdel -- arnaud"} {
		if !f.ran(want) {
			t.Errorf("annulation incomplète, manque : %s", want)
		}
	}
}

func TestNonLinuxSourceAsksPassword(t *testing.T) {
	_, a, p, inv, _ := setup(t)
	p.Actions[0].Password = "ask"
	asked := ""
	a.AskPassword = func(login string) (string, error) { asked = login; return "nouveau-mdp", nil }
	if _, err := a.System(context.Background(), p, inv); err != nil {
		t.Fatal(err)
	}
	if asked != "arnaud" {
		t.Error("le mot de passe aurait dû être demandé")
	}
}

const wifi = `[connection]
id=Maison
uuid=0b5b7a3e-9f2a-4a38-9d6e-2f3c1a7b8e11
type=wifi

[wifi-security]
psk=secret
`

func TestSettingsAppliedAndUndone(t *testing.T) {
	f, a, _, inv, jp := setup(t)
	nm := t.TempDir()
	sa := &settings.Applier{Exec: f.exec, StateDir: t.TempDir(), NMDir: nm}
	p := &plan.Plan{Target: plan.Target{Desktop: "gnome"}, Actions: []plan.Action{
		{Op: plan.OpSettings, From: "u1", Login: "root", Label: "root", Selected: true},
		{Op: plan.OpImportWifi, Label: "Maison", Selected: true},
		{Op: plan.OpAddPrinter, Label: "Bureau", Selected: true},
		{Op: plan.OpAddPrinter, Label: "USB", Selected: true},
	}}
	ex := &settings.Extras{
		Desktop:  "gnome",
		Dconf:    map[string]string{"root": "[org/gnome/desktop/background]\npicture-uri='file:///x.jpg'\n"},
		Crontabs: map[string]string{"root": "0 3 * * * sauvegarde\n"},
		Wifi:     []settings.NMConnection{{File: "Maison.nmconnection", Content: wifi}},
		Printers: []settings.Printer{{Name: "Bureau", URI: "ipps://imprimante.local/ipp/print"}, {Name: "USB", URI: "usb://HP/X"}},
	}
	rep, err := a.Settings(context.Background(), p, inv, ex, sa)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Failed) != 0 || len(rep.Applied) != 4 || rep.Skipped["Imprimante USB"] == "" {
		t.Fatalf("rapport inattendu : %+v", rep)
	}
	// Relance : rien n'est refait.
	rep2, _ := a.Settings(context.Background(), p, inv, ex, sa)
	if len(rep2.Applied) != 0 {
		t.Errorf("réappliqué à la relance : %v", rep2.Applied)
	}
	st, _ := journal.Load(jp)
	u := UndoSystem(context.Background(), st, &system.System{Exec: f.exec}, sa)
	if len(u.Errors) != 0 {
		t.Fatalf("annulation : %v", u.Errors)
	}
	if files, _ := filepath.Glob(filepath.Join(nm, "*")); len(files) != 0 {
		t.Errorf("connexion Wi-Fi non retirée : %v", files)
	}
	for _, want := range []string{"lpadmin -x Bureau", "crontab -u root -r", "dconf reset -f /"} {
		found := false
		for _, c := range f.cmds {
			if strings.Contains(c, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("annulation incomplète, manque : %s", want)
		}
	}
}
