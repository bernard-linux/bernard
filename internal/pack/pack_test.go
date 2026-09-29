package pack

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bernard-linux/bernard/internal/engine"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/transfer"
)

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
}

func random(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

const pass = "cheval-batterie-agrafe"

func buildPack(t *testing.T) (string, string, *inventory.Inventory) {
	home := t.TempDir()
	write(t, filepath.Join(home, "Documents/rapport été.odt"), []byte("rapport"))
	write(t, filepath.Join(home, "Musique/😀 morceau.flac"), random(2<<20+123))
	write(t, filepath.Join(home, "exact.bin"), random(1<<20)) // pile un bloc
	write(t, filepath.Join(home, "vide.txt"), nil)
	write(t, filepath.Join(home, ".cache/x"), random(10))
	os.Symlink("Documents", filepath.Join(home, "docs"))
	os.MkdirAll(filepath.Join(home, "copie"), 0o755)
	os.Link(filepath.Join(home, "exact.bin"), filepath.Join(home, "copie/exact.bin")) // lien dur : contenu écrit une fois
	inv := &inventory.Inventory{
		Schema:   inventory.Schema,
		Source:   inventory.Source{OS: "linux", Hostname: "ancien-pc"},
		Users:    []inventory.User{{ID: "u1", Login: "arnaud", Home: home}},
		DataSets: []inventory.DataSet{{ID: "d1", User: "u1", Path: home, Excluded: []string{".cache"}}},
	}
	dir := filepath.Join(t.TempDir(), "paquet")
	// Limite de 1,5 Mo par fichier data pour forcer le découpage.
	rep, err := Write(context.Background(), inv, dir, pass, WriteOptions{ObjectLimit: 1_500_000, Iterations: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Errors) > 0 || rep.Files != 4 {
		t.Fatalf("écriture : %+v", rep)
	}
	return dir, home, inv
}

func TestPackRoundTrip(t *testing.T) {
	dir, home, _ := buildPack(t)
	objs, _ := filepath.Glob(filepath.Join(dir, "data-*.bin"))
	if len(objs) < 2 {
		t.Fatalf("les données auraient dû être découpées : %v", objs)
	}
	for _, o := range objs {
		if fi, _ := os.Stat(o); fi.Size() > 1_500_000+(1<<20)+64 {
			t.Errorf("%s dépasse la limite", o)
		}
	}
	p, err := Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	inv, _ := p.Inventory(context.Background())
	j, st, err := engine.Begin(filepath.Join(t.TempDir(), "j.jsonl"), inv.Identity())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	dst := filepath.Join(t.TempDir(), "arnaud")
	r := &engine.Receiver{Src: p, Journal: j, State: st}
	rep, err := r.CopyDataSet(context.Background(), inv.DataSets[0], dst)
	if err != nil || !rep.OK() {
		t.Fatalf("lecture : %v %+v", err, rep)
	}
	for _, rel := range []string{"Documents/rapport été.odt", "Musique/😀 morceau.flac", "exact.bin", "vide.txt"} {
		a, _, _ := transfer.HashFile(filepath.Join(home, rel))
		b, _, err := transfer.HashFile(filepath.Join(dst, rel))
		if err != nil || a != b {
			t.Errorf("%s différent après passage par le paquet", rel)
		}
		sa, _ := os.Stat(filepath.Join(home, rel))
		sb, _ := os.Stat(filepath.Join(dst, rel))
		if sa.Mode() != sb.Mode() || !sa.ModTime().Equal(sb.ModTime()) {
			t.Errorf("%s : droits ou date perdus", rel)
		}
	}
	fa, _ := os.Stat(filepath.Join(dst, "exact.bin"))
	fb, _ := os.Stat(filepath.Join(dst, "copie/exact.bin"))
	if fa == nil || fb == nil || !os.SameFile(fa, fb) {
		t.Error("lien dur non recréé depuis le paquet")
	}
	if l, _ := os.Readlink(filepath.Join(dst, "docs")); l != "Documents" {
		t.Error("lien perdu")
	}
	if _, err := os.Stat(filepath.Join(dst, ".cache")); err == nil {
		t.Error("exclusion non respectée")
	}
}

func TestPackWrongPassphrase(t *testing.T) {
	dir, _, _ := buildPack(t)
	if _, err := Open(dir, "mauvaise-phrase"); !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("attendu ErrBadPassphrase, obtenu %v", err)
	}
}

func TestPackDetectsTampering(t *testing.T) {
	dir, _, _ := buildPack(t)
	obj := filepath.Join(dir, "data-000001.bin")
	b, _ := os.ReadFile(obj)
	b[len(b)/2] ^= 0xff
	os.WriteFile(obj, b, 0o600)
	p, err := Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	inv, _ := p.Inventory(context.Background())
	j, st, _ := engine.Begin(filepath.Join(t.TempDir(), "j.jsonl"), inv.Identity())
	defer j.Close()
	dst := filepath.Join(t.TempDir(), "arnaud")
	rep, err := (&engine.Receiver{Src: p, Journal: j, State: st}).CopyDataSet(context.Background(), inv.DataSets[0], dst)
	if err != nil {
		t.Fatalf("un bloc altéré ne doit pas arrêter toute la migration : %v", err)
	}
	if rep.OK() {
		t.Fatal("l'altération n'a pas été détectée")
	}
	if b, err := os.ReadFile(filepath.Join(dst, "Documents/rapport été.odt")); err != nil || string(b) != "rapport" {
		t.Error("les fichiers intacts auraient dû être copiés")
	}
	// Aucun fichier altéré ne doit apparaître sous son nom final.
	a, _, _ := transfer.HashFile(filepath.Join(inv.DataSets[0].Path, "Musique/😀 morceau.flac"))
	if b, _, err := transfer.HashFile(filepath.Join(dst, "Musique/😀 morceau.flac")); err == nil && a != b {
		t.Fatal("un fichier altéré a été placé")
	}
}

func TestIncompletePackRefused(t *testing.T) {
	dir, _, _ := buildPack(t)
	os.Remove(filepath.Join(dir, manifestName))
	b, _ := os.ReadFile(filepath.Join(dir, headerName))
	os.WriteFile(filepath.Join(dir, headerName), []byte(string(b[:len(b)-1])+`,"complete":false}`), 0o600)
	if _, err := Open(dir, pass); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("attendu ErrIncomplete, obtenu %v", err)
	}
}

func TestPBKDF2Vector(t *testing.T) {
	// RFC 7914 §11 : PBKDF2-HMAC-SHA256("passwd", "salt", 1), 32 premiers octets.
	got := pbkdf2([]byte("passwd"), []byte("salt"), 1)
	want := "55ac046e56e3089fec1691c22544b605f94185216dde0465e68b9d57c20dacbc"
	if hexs := hexString(got); hexs != want {
		t.Fatalf("PBKDF2 incorrect : %s", hexs)
	}
}

func hexString(b []byte) string {
	const d = "0123456789abcdef"
	out := make([]byte, 0, 2*len(b))
	for _, c := range b {
		out = append(out, d[c>>4], d[c&15])
	}
	return string(out)
}
