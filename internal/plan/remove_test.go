package plan

import (
	"testing"

	"github.com/bernard-linux/bernard/internal/aptrepo"
	"github.com/bernard-linux/bernard/internal/inventory"
)

func zorinPair() (*inventory.Inventory, Target) {
	inv := &inventory.Inventory{
		Schema: inventory.Schema,
		Source: inventory.Source{OS: "linux", Distro: "zorin", Version: "17", Desktop: "gnome", Keyboard: "fr", GPUs: []string{"intel"}},
		Users:  []inventory.User{{ID: "u1", Login: "arnaud", Home: "/home/arnaud"}},
		Apps: []inventory.App{
			{ID: "a1", SourceID: "apt:gimp", Name: "gimp", Origin: inventory.OriginApt},
			{ID: "a2", SourceID: "apt:nvidia-driver-550", Name: "nvidia-driver-550", Origin: inventory.OriginApt},
		},
		Packages:        []string{"gimp", "libc6", "nautilus", "libreoffice-core"},
		PackagesRemoved: map[string]string{"rhythmbox": "2025-11-02"},
	}
	t := Target{
		Distro: "zorin", Version: "17", Desktop: "gnome", Keyboard: "be", GPUs: []string{"intel", "nvidia"},
		HomeRoot: "/home", FreeBytes: 100 << 30, ExistingUsers: map[string]bool{"arnaud": true},
		AptInstalled: map[string]bool{"gimp": true},
		AptManual: map[string]bool{
			"rhythmbox": true, "cheese": true, "libreoffice-writer": true, "nvidia-settings": true,
			"nautilus": true, "gnome-mahjongg": true, "libc6": true, "aisleriot": true,
		},
		AptApps: map[string]string{
			"rhythmbox": "Rhythmbox", "cheese": "Cheese", "libreoffice-writer": "LibreOffice Writer",
			"nvidia-settings": "Réglages NVIDIA", "nautilus": "Fichiers", "gnome-mahjongg": "Mahjongg",
			"aisleriot": "Aisleriot",
		},
		AptSize: map[string]int64{"rhythmbox": 5 << 20, "rhythmbox-data": 3 << 20, "cheese": 1 << 20, "aisleriot": 4 << 20, "aisleriot-data": 1 << 20},
		RemovalImpact: func(pkg string) ([]string, error) {
			switch pkg {
			case "libreoffice-writer": // emporterait un paquet présent sur l'ancien
				return []string{"libreoffice-writer", "libreoffice-core"}, nil
			case "aisleriot":
				return []string{"aisleriot", "aisleriot-data"}, nil
			}
			return []string{pkg}, nil
		},
		FlatpakInstalled: map[string]bool{"org.gnome.Extensions": true},
		FlatpakNames:     map[string]string{"org.gnome.Extensions": "Extensions"},
	}
	return inv, t
}

func removalsByPkg(p *Plan) map[string]Action {
	m := map[string]Action{}
	for _, a := range p.Actions {
		if a.Op == OpRemove {
			m[a.Package] = a
		}
	}
	return m
}

func TestRemovals(t *testing.T) {
	inv, tg := zorinPair()
	p, err := Build(inv, tg)
	if err != nil {
		t.Fatal(err)
	}
	rm := removalsByPkg(p)
	if a := rm["rhythmbox"]; !a.Selected || a.Reason != ReasonRemovedOnSource || a.Date != "2025-11-02" || a.Label != "Rhythmbox" {
		t.Errorf("rhythmbox : %+v", a)
	}
	if a := rm["cheese"]; !a.Selected || a.Reason != ReasonAbsentOnSource {
		t.Errorf("cheese : %+v", a)
	}
	if a := rm["aisleriot"]; a.Selected || len(a.Also) != 1 || a.Bytes != 5<<20 {
		t.Errorf("aisleriot (emporte un autre paquet : non coché) : %+v", a)
	}
	for _, never := range []string{"libreoffice-writer", "nvidia-settings", "nautilus", "libc6"} {
		if _, ok := rm[never]; ok {
			t.Errorf("%s ne doit jamais être proposé au retrait", never)
		}
	}
	if a := rm["org.gnome.Extensions"]; !a.Selected || a.Via != "flatpak" {
		t.Errorf("flatpak : %+v", a)
	}
	if p.Freed != (5<<20)+(1<<20) {
		t.Errorf("place libérée = %d", p.Freed)
	}

	// Pilote de l'ancien ordinateur : jamais installé ici.
	for _, a := range p.Actions {
		if a.From == "a2" && (a.Op != OpSkip || a.Reason != ReasonHardware || a.Selected) {
			t.Errorf("pilote : %+v", a)
		}
	}

	// Claviers différents : option proposée, non cochée.
	found := false
	for _, a := range p.Actions {
		if a.Op == OpKeyboard {
			found = true
			if a.Selected || a.Date != "fr" {
				t.Errorf("clavier : %+v", a)
			}
		}
	}
	if !found {
		t.Error("option clavier absente")
	}
}

func TestRemovalsGuards(t *testing.T) {
	inv, tg := zorinPair()
	tg.Distro = "ubuntu"
	p, _ := Build(inv, tg)
	if len(removalsByPkg(p)) != 0 {
		t.Error("distributions différentes : aucun retrait")
	}

	inv, tg = zorinPair()
	inv.Packages = nil // ancien agent
	p, _ = Build(inv, tg)
	if len(removalsByPkg(p)) != 0 {
		t.Error("agent sans liste complète : aucun retrait")
	}

	inv, tg = zorinPair()
	tg.Version = "18"
	p, _ = Build(inv, tg)
	rm := removalsByPkg(p)
	if a := rm["cheese"]; a.Selected || a.Reason != ReasonAbsentOlder {
		t.Errorf("versions différentes, cheese : %+v", a)
	}
	if !rm["rhythmbox"].Selected {
		t.Error("retrait attesté par le journal : coché même entre versions")
	}

	inv, tg = zorinPair()
	tg.Keyboard = "fr"
	p, _ = Build(inv, tg)
	for _, a := range p.Actions {
		if a.Op == OpKeyboard {
			t.Error("même clavier : pas d'option")
		}
	}
}

func TestRepoMakesAppInstallable(t *testing.T) {
	inv, tg := zorinPair()
	inv.Apps = append(inv.Apps, inventory.App{ID: "a9", SourceID: "apt:brave-browser", Name: "brave-browser",
		Origin: inventory.OriginApt, Repo: "brave-browser-apt-release.s3.brave.com"})
	inv.AptSources = []aptrepo.Source{{File: "brave-browser-release.list", URIs: []string{"brave-browser-apt-release.s3.brave.com"}}}
	tg.KnownRepos = map[string]bool{}
	p, _ := Build(inv, tg)
	var repo, app *Action
	for i, a := range p.Actions {
		if a.Op == OpAddRepo {
			repo = &p.Actions[i]
		}
		if a.From == "a9" {
			app = &p.Actions[i]
		}
	}
	if repo == nil || !repo.Selected || repo.Label != "brave-browser-apt-release.s3.brave.com" {
		t.Fatalf("dépôt : %+v", repo)
	}
	if app == nil || app.Op != OpInstall || app.Via != "apt" || !app.Selected {
		t.Fatalf("Brave : %+v", app)
	}
	tg.KnownRepos = map[string]bool{"brave-browser-apt-release.s3.brave.com": true}
	p, _ = Build(inv, tg)
	for _, a := range p.Actions {
		if a.Op == OpAddRepo {
			t.Error("dépôt déjà connu : pas d'ajout")
		}
	}
}

func TestDatabaseNeedsSameRelease(t *testing.T) {
	inv, tg := zorinPair()
	inv.Source.Codename, tg.Codename = "noble", "noble"
	inv.System = []inventory.SystemItem{{ID: "s1", Kind: inventory.SysDatabase, Label: "Bases MySQL", Paths: []string{"/var/lib/mysql"},
		Bytes: 1 << 20, Used: 1 << 20, Service: "mysql", Advice: inventory.AdviceCopy}}
	inv.DataSets = append(inv.DataSets, inventory.DataSet{ID: "x1", Kind: "system", System: "s1", Dest: "/var/lib/mysql", Service: "mysql"})
	find := func(p *Plan) Action {
		for _, a := range p.Actions {
			if a.Op == OpSystemData {
				return a
			}
		}
		return Action{}
	}
	p, _ := Build(inv, tg)
	if a := find(p); !a.Selected || a.Package != "x1" || a.Note == "" {
		t.Errorf("même version : copie avec arrêt du service : %+v", a)
	}
	tg.Codename = "plucky"
	p, _ = Build(inv, tg)
	if a := find(p); a.Selected || a.Package != "" {
		t.Errorf("versions différentes : pas de copie directe : %+v", a)
	}
}
