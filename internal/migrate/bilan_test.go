package migrate

import (
	"strings"
	"testing"
	"time"

	"github.com/bernard-linux/bernard/internal/apply"
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
