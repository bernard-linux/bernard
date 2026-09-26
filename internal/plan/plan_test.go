package plan

import (
	"testing"

	"github.com/bernard-linux/bernard/internal/inventory"
)

func sampleInventory() *inventory.Inventory {
	return &inventory.Inventory{
		Schema: inventory.Schema,
		Source: inventory.Source{OS: "linux", Distro: "ubuntu", Version: "24.04"},
		Users: []inventory.User{
			{ID: "u1", Login: "arnaud", Home: "/home/arnaud"},
			{ID: "u2", Login: "invite", Home: "/home/invite"},
		},
		Apps: []inventory.App{
			{ID: "a1", SourceID: "apt:vlc", Name: "vlc", Origin: inventory.OriginApt},
			{ID: "a2", SourceID: "apt:gimp", Name: "gimp", Origin: inventory.OriginApt},
			{ID: "a3", SourceID: "apt:vieux-logiciel", Name: "vieux-logiciel", Origin: inventory.OriginApt},
			{ID: "a4", SourceID: "flatpak:org.signal.Signal", Name: "Signal", Origin: inventory.OriginFlatpak},
			{ID: "a5", SourceID: "snap:spotify", Name: "spotify", Origin: inventory.OriginSnap},
			{ID: "a6", SourceID: "snap:snap-store", Name: "snap-store", Origin: inventory.OriginSnap},
			{ID: "a7", SourceID: "snap:firefox", Name: "firefox", Origin: inventory.OriginSnap},
			{ID: "a8", SourceID: "snap:outil-inconnu", Name: "outil-inconnu", Origin: inventory.OriginSnap},
		},
		DataSets: []inventory.DataSet{
			{ID: "d1", User: "u1", Kind: "home", Path: "/home/arnaud", Files: 100, SizeBytes: 40 << 30},
			{ID: "d2", User: "u2", Kind: "home", Path: "/home/invite", Files: 10, SizeBytes: 1 << 30},
		},
		Network: inventory.Network{Wifi: []string{"Maison"}, Printers: []string{"HP_Bureau"}},
	}
}

// Cible type : Linux Mint, sans Snap, Flathub configuré, Firefox en .deb.
func mintTarget() Target {
	return Target{
		Distro: "linuxmint", HomeRoot: "/home", FreeBytes: 200 << 30,
		ExistingUsers: map[string]bool{"root": true, "arnaud": true},
		AptInstalled:  map[string]bool{"vlc": true, "firefox": true},
		AptAvailable:  func(n string) bool { return n == "gimp" },
		FlatpakReady:  true,
	}
}

func byFrom(p *Plan) map[string]Action {
	m := map[string]Action{}
	for _, a := range p.Actions {
		if _, seen := m[a.From]; a.From != "" && !seen {
			m[a.From] = a
		}
	}
	return m
}

func TestPlanRules(t *testing.T) {
	p, err := Build(sampleInventory(), mintTarget())
	if err != nil {
		t.Fatal(err)
	}
	got := byFrom(p)
	check := func(from, op, via, pkg, reason string) {
		t.Helper()
		a := got[from]
		if a.Op != op || a.Via != via || a.Package != pkg || a.Reason != reason {
			t.Errorf("%s : obtenu op=%s via=%s pkg=%s reason=%s ; attendu %s %s %s %s",
				from, a.Op, a.Via, a.Package, a.Reason, op, via, pkg, reason)
		}
	}
	check("u1", OpUseUser, "", "", "")
	check("u2", OpCreateUser, "", "", "")
	check("a1", OpSkip, "", "", ReasonAlreadyInstalled)         // vlc déjà là
	check("a2", OpInstall, "apt", "gimp", "")                   // dépôt d'abord
	check("a3", OpReview, "", "", ReasonNotInRepos)             // introuvable
	check("a4", OpInstall, "flatpak", "org.signal.Signal", "")  // Flatpak reste Flatpak
	check("a5", OpInstall, "flatpak", "com.spotify.Client", "") // Snap → Flatpak
	check("a6", OpSkip, "", "", ReasonSnapInfra)                // Snap technique
	check("a7", OpSkip, "", "", ReasonAlreadyInstalled)         // Firefox .deb présent
	check("a8", OpReview, "", "", ReasonNoEquivalent)           // Snap inconnu

	if got["u2"].Password != "copyHash" {
		t.Errorf("Linux → Linux doit reprendre le hachage du mot de passe")
	}
	if got["a5"].Fidelity != FidelitySubstitute {
		t.Errorf("Snap converti en Flatpak = substitut")
	}
	if got["a3"].Selected {
		t.Errorf("une action manuelle ne doit pas être cochée par défaut")
	}
	if got["d1"].To != "/home/arnaud" || got["d1"].Bytes != 40<<30 {
		t.Errorf("copie mal planifiée : %+v", got["d1"])
	}
	if p.Totals.Bytes != 41<<30 || p.Blocked {
		t.Errorf("totaux ou blocage inattendus : %+v bloqué=%v", p.Totals, p.Blocked)
	}
	for _, a := range p.Actions {
		if a.Op == OpSetupFlatpak {
			t.Error("Flathub déjà prêt : aucune installation de Flatpak attendue")
		}
	}
}

func TestPlanSetsUpFlatpakWhenMissing(t *testing.T) {
	tgt := mintTarget()
	tgt.FlatpakReady = false
	p, _ := Build(sampleInventory(), tgt)
	if p.Actions[2].Op != OpSetupFlatpak {
		t.Fatalf("Flatpak doit être installé avant les applications : %+v", p.Actions[2])
	}
}

func TestPlanBlocksWhenSpaceIsShort(t *testing.T) {
	tgt := mintTarget()
	tgt.FreeBytes = 42 << 30 // 41 Go nécessaires, marge de 5 % → insuffisant
	p, _ := Build(sampleInventory(), tgt)
	if !p.Blocked || p.Reason != ReasonNotEnoughSpace {
		t.Fatalf("le plan aurait dû être bloqué : %+v", p)
	}
}

func TestPlanNonLinuxSourceAsksPassword(t *testing.T) {
	inv := sampleInventory()
	inv.Source.OS = "windows"
	p, _ := Build(inv, mintTarget())
	if byFrom(p)["u2"].Password != "ask" {
		t.Error("depuis Windows, le mot de passe doit être redemandé")
	}
}

func TestPlanSettingsAndPrinters(t *testing.T) {
	inv := sampleInventory()
	inv.Source.Desktop = "gnome"
	tgt := mintTarget()
	tgt.Desktop = "cinnamon"
	p, _ := Build(inv, tgt)
	var settings, printers int
	for _, a := range p.Actions {
		switch a.Op {
		case OpSettings:
			settings++
			if a.Fidelity != FidelitySubstitute || !a.Selected {
				t.Errorf("GNOME → Cinnamon : réglages traduits attendus : %+v", a)
			}
		case OpAddPrinter:
			printers++
		}
	}
	if settings != 2 || printers != 1 {
		t.Fatalf("attendu 2 actions de réglages et 1 imprimante : %d, %d", settings, printers)
	}
	tgt.Desktop = "kde"
	p, _ = Build(inv, tgt)
	for _, a := range p.Actions {
		if a.Op == OpSettings && a.Reason != ReasonDesktopMismatch {
			t.Errorf("vers KDE, seuls les éléments transposables doivent suivre : %+v", a)
		}
	}
}
