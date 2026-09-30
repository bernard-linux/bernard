package apply

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bernard-linux/bernard/internal/engine"
	"github.com/bernard-linux/bernard/internal/journal"
	"github.com/bernard-linux/bernard/internal/services"
	"github.com/bernard-linux/bernard/internal/sysexec"
)

// Dossier de base de données de la cible : mis de côté en entier avant la
// copie, remis en entier à l'annulation (jamais de mélange de deux bases).
func TestServiceDataSwap(t *testing.T) {
	var cmds []string
	newServices = func() *services.Controller {
		return &services.Controller{Exec: func(_ context.Context, c sysexec.Cmd) (string, error) {
			cmds = append(cmds, c.Name+" "+strings.Join(c.Args, " "))
			return "", nil
		}}
	}
	defer func() { newServices = services.New }()

	root := t.TempDir()
	dest := filepath.Join(root, "var/lib/mysql")
	os.MkdirAll(filepath.Join(dest, "mysql"), 0o750)
	os.Chmod(dest, 0o750)
	os.WriteFile(filepath.Join(dest, "ibdata1"), []byte("base neuve de la cible"), 0o640)
	state := filepath.Join(root, "var/lib/bernard/x")
	os.MkdirAll(state, 0o700)
	j, st, err := engine.Begin(filepath.Join(state, "journal.jsonl"), "inv")
	if err != nil {
		t.Fatal(err)
	}
	r := &engine.Receiver{Journal: j, State: st}
	aside := filepath.Join(state, "avant-migration")
	if err := setAsideWhole(r, aside, dest, "mysql"); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatalf("dossier non vidé : %v", entries)
	}
	if fi, _ := os.Stat(dest); fi == nil || fi.Mode().Perm() != 0o750 {
		t.Error("dossier recréé sans ses droits d'origine")
	}
	// Copie de l'ancien ordinateur, puis la base sert encore.
	os.WriteFile(filepath.Join(dest, "ibdata1"), []byte("base de l'ancien PC, modifiée depuis"), 0o640)
	// Reprise : pas de seconde mise de côté.
	if err := setAsideWhole(r, aside, dest, "mysql"); err != nil {
		t.Fatal(err)
	}
	j.Close()

	st2, _ := journal.Load(filepath.Join(state, "journal.jsonl"))
	restored, errs := UndoServiceData(context.Background(), st2, state)
	if len(errs) > 0 || len(restored) != 1 {
		t.Fatalf("%v %v", restored, errs)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "ibdata1")); string(b) != "base neuve de la cible" {
		t.Errorf("base d'origine non remise : %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(state, "apres-migration", dest, "ibdata1")); !strings.Contains(string(b), "ancien PC") {
		t.Error("version migrée non gardée")
	}
	if got := strings.Join(cmds, "|"); got != "systemctl stop mysql|systemctl start mysql" {
		t.Errorf("services : %s", got)
	}
}
