package migrate

import (
	"strings"
	"testing"
	"time"

	"github.com/bernard-linux/bernard/internal/apply"
	"github.com/bernard-linux/bernard/internal/i18n"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/plan"
	"github.com/bernard-linux/bernard/internal/transfer"
)

func TestBilanHTML(t *testing.T) {
	res := &Result{
		System:   &apply.Report{Installed: []string{"gimp"}, Failed: map[string]string{"Application x": "introuvable"}},
		Settings: &apply.SettingsReport{Applied: []string{"Réglages du bureau de arnaud"}, Skipped: map[string]string{"Imprimante USB": "non appliqué : à rebrancher"}},
		Data: map[string]*transfer.TreeReport{"d1": {Files: 10, Bytes: 2_500_000, Links: 2,
			Errors: []transfer.FileError{{Path: "Documents/<script>.txt", Err: "illisible"}}}},
		JournalPath: "/var/lib/bernard/x/journal.jsonl",
	}
	s := &Session{
		Inv: &inventory.Inventory{Source: inventory.Source{Hostname: "ancien-pc", Distro: "zorin", Version: "17"}},
		Plan: &plan.Plan{Actions: []plan.Action{
			{Op: plan.OpInstall, Label: "Brave", Package: "brave-browser", Selected: true},
			{Op: plan.OpImportVPN, Label: "Bureau", Selected: true},
			{Op: plan.OpSystemData, Label: "Sites web (/var/www)", To: "/var/www", Selected: true},
		}},
	}
	b, err := bilanHTML(res, s, time.Date(2026, 9, 29, 21, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	h := string(b)
	for _, want := range []string{"29/09/2026 à 21:05", "ancien-pc", "2,5 Mo", "2 liens durs", "gimp", "introuvable",
		"à rebrancher", "Ouvrez Brave", "VPN « Bureau »", "/var/www", "&lt;script&gt;", "Imprimer ou enregistrer en PDF"} {
		if !strings.Contains(h, want) {
			t.Errorf("bilan sans %q", want)
		}
	}
	if strings.Contains(h, "<script>.txt") || strings.Contains(h, "Tout est arrivé") {
		t.Error("échappement ou état erroné")
	}
}

func TestBilanHTMLEnglish(t *testing.T) {
	i18n.Set("en")
	t.Cleanup(func() { i18n.Set("fr") })
	res := &Result{
		System: &apply.Report{Installed: []string{"gimp"}, UsersCreated: []string{"alice"}},
		Data: map[string]*transfer.TreeReport{"d1": {Files: 10, Bytes: 2_500_000, Links: 2,
			Renamed: []transfer.FileResult{{Dst: "Documents/a (bernard 1).txt"}}}},
		JournalPath: "/var/lib/bernard/x/journal.jsonl",
	}
	s := &Session{
		Inv: &inventory.Inventory{Source: inventory.Source{Hostname: "old-pc", Distro: "zorin", Version: "17"}},
		Plan: &plan.Plan{Target: plan.Target{Distro: "ubuntu", Version: "24.04"}, Actions: []plan.Action{
			{Op: plan.OpInstall, Label: "Brave", Package: "brave-browser", Selected: true},
			{Op: plan.OpImportWifi, Label: "Home", Selected: true},
		}},
	}
	b, err := bilanHTML(res, s, time.Date(2026, 9, 29, 21, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	h := string(b)
	for _, want := range []string{`<html lang="en">`, "Migration report", "29/09/2026 at 21:05", "from old-pc", "to ubuntu 24.04",
		"Everything arrived", "10 files checked (2.5 MB copied), including 2 hard links recreated", "Account alice created",
		"Apps installed: gimp", "Open Brave", "Suggested checks", "Renamed files", "Undo the migration", "Print or save as PDF"} {
		if !strings.Contains(h, want) {
			t.Errorf("bilan anglais sans %q", want)
		}
	}
	for _, fr := range []string{"Bilan", "Vérifications", "fichiers vérifiés", "Ouvrez", " à ", " Mo", "Compte", "depuis"} {
		if strings.Contains(h, fr) {
			t.Errorf("bilan anglais avec du français : %q", fr)
		}
	}
	if bilanName() != BilanNameEN {
		t.Error("nom du fichier en anglais")
	}
}
