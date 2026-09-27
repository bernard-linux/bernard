package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bernard-linux/bernard/internal/journal"
	"github.com/bernard-linux/bernard/internal/transfer"
)

func TestPreferSourceSwapAndUndo(t *testing.T) {
	home := t.TempDir()
	kr := filepath.Join(home, ".local/share/keyrings")
	os.MkdirAll(kr, 0o700)
	// Trousseau neuf de la cible, et copie renommée venant de l'ancien.
	os.WriteFile(filepath.Join(kr, "login.keyring"), []byte("neuf"), 0o600)
	os.WriteFile(filepath.Join(kr, "login (bernard 1).keyring"), []byte("ancien"), 0o600)
	// Fichier renommé hors des dossiers de profil : ne bouge pas.
	os.WriteFile(filepath.Join(home, ".bashrc"), []byte("cible"), 0o600)
	os.WriteFile(filepath.Join(home, ".bashrc (bernard 1)"), []byte("source"), 0o600)

	jp := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := journal.Open(jp)
	if err != nil {
		t.Fatal(err)
	}
	for key, dst := range map[string]string{
		"d1/.local/share/keyrings/login.keyring": filepath.Join(kr, "login (bernard 1).keyring"),
		"d1/.bashrc":                             filepath.Join(home, ".bashrc (bernard 1)"),
	} {
		j.Append(journal.Record{T: journal.RecDone, Key: key, Dst: dst, Status: string(transfer.StatusRenamed)})
	}
	st, _ := journal.Load(jp)
	done, err := PreferSource(home, st, j)
	if err != nil || len(done) != 1 {
		t.Fatalf("échanges = %v, %v", done, err)
	}
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }
	if got := read(filepath.Join(kr, "login.keyring")); got != "ancien" {
		t.Fatalf("trousseau en place = %q", got)
	}
	if got := read(filepath.Join(home, AsideDir, ".local/share/keyrings/login.keyring")); got != "neuf" {
		t.Fatalf("trousseau mis de côté = %q", got)
	}
	if read(filepath.Join(home, ".bashrc")) != "cible" {
		t.Fatal(".bashrc ne devait pas être échangé")
	}
	// Relancer ne refait rien.
	st, _ = journal.Load(jp)
	if again, _ := PreferSource(home, st, j); len(again) != 0 {
		t.Fatalf("second passage : %v", again)
	}
	j.Close()

	st, _ = journal.Load(jp)
	if errs := UndoPreferSource(st); len(errs) > 0 {
		t.Fatal(errs)
	}
	if read(filepath.Join(kr, "login.keyring")) != "neuf" || read(filepath.Join(kr, "login (bernard 1).keyring")) != "ancien" {
		t.Fatal("annulation incomplète")
	}
}
