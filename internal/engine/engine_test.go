package engine

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/journal"
	"github.com/bernard-linux/bernard/internal/remote"
	"github.com/bernard-linux/bernard/internal/session"
	"github.com/bernard-linux/bernard/internal/source"
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

// sourceTree crée un dossier personnel de test et l'inventaire correspondant.
func sourceTree(t *testing.T) (*inventory.Inventory, string) {
	home := t.TempDir()
	write(t, filepath.Join(home, "Documents/rapport été.odt"), []byte("rapport"))
	write(t, filepath.Join(home, "Documents/😀 photo.jpg"), random(300_000))
	write(t, filepath.Join(home, "Vidéos/gros.mkv"), random(6<<20))
	write(t, filepath.Join(home, "vide.txt"), nil)
	write(t, filepath.Join(home, ".cache/jetable"), random(1000))
	os.Symlink("Documents/rapport été.odt", filepath.Join(home, "raccourci"))
	inv := &inventory.Inventory{
		Schema: inventory.Schema,
		Source: inventory.Source{OS: "linux", Hostname: "ancien-pc"},
		Users:  []inventory.User{{ID: "u1", Login: "arnaud", Home: home}},
		DataSets: []inventory.DataSet{
			{ID: "d1", User: "u1", Kind: "home", Path: home, Excluded: []string{".cache"}},
		},
	}
	return inv, home
}

// cutConn simule une coupure après limit octets reçus.
type cutConn struct {
	net.Conn
	limit int64
}

func (c *cutConn) Read(p []byte) (int, error) {
	if c.limit <= 0 {
		c.Conn.Close()
		return 0, io.ErrUnexpectedEOF
	}
	if int64(len(p)) > c.limit {
		p = p[:c.limit]
	}
	n, err := c.Conn.Read(p)
	c.limit -= int64(n)
	return n, err
}

// connect lance un agent sur l'inventaire et renvoie un client appairé.
// cut > 0 coupe la liaison côté cible après cut octets.
func connect(t *testing.T, inv *inventory.Inventory, cut int64) *remote.Client {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	pairer, _ := session.NewPairer("nouveau-pc")
	cfg, _ := session.ServerConfig()

	// Ici, pour tester, la cible (serveur TLS) est le moteur et l'agent se
	// connecte, exactement comme en vrai.
	type result struct {
		c   *session.Conn
		err error
	}
	accepted := make(chan result, 1)
	go func() {
		raw, err := ln.Accept()
		if err != nil {
			accepted <- result{nil, err}
			return
		}
		if cut > 0 {
			raw = &cutConn{Conn: raw, limit: cut}
		}
		c, err := pairer.Accept(context.Background(), raw, cfg)
		accepted <- result{c, err}
	}()
	go func() {
		c, err := session.Dial(context.Background(), ln.Addr().String(), pairer.Code(), "ancien-pc")
		if err != nil {
			return
		}
		defer c.Close()
		(&remote.Server{Inv: inv}).Serve(context.Background(), c)
	}()
	r := <-accepted
	if r.err != nil {
		t.Fatalf("appairage : %v", r.err)
	}
	return &remote.Client{RW: r.c}
}

func sameTree(t *testing.T, src, dst string, skip ...string) {
	t.Helper()
	filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		rel, _ := filepath.Rel(src, p)
		for _, s := range skip {
			if rel == s || strings.HasPrefix(rel, s+"/") {
				return nil
			}
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		hs, _, _ := transfer.HashFile(p)
		hd, _, err := transfer.HashFile(filepath.Join(dst, rel))
		if err != nil || hs != hd {
			t.Errorf("%s : absent ou différent sur la cible", rel)
		}
		return nil
	})
}

func runCopy(t *testing.T, cli *remote.Client, jpath, dst string) (*transfer.TreeReport, error) {
	inv, err := cli.Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	j, st, err := Begin(jpath, inv.Identity())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	r := &Receiver{Src: cli, Journal: j, State: st, SyncEvery: 256 << 10}
	return r.CopyDataSet(context.Background(), inv.DataSets[0], dst)
}

func TestNetworkMigration(t *testing.T) {
	inv, home := sourceTree(t)
	dst := filepath.Join(t.TempDir(), "arnaud")
	cli := connect(t, inv, 0)
	defer cli.Close()

	rep, err := runCopy(t, cli, filepath.Join(t.TempDir(), "journal.jsonl"), dst)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatalf("erreurs : %+v", rep.Errors)
	}
	sameTree(t, home, dst, ".cache")
	if _, err := os.Lstat(filepath.Join(dst, ".cache")); err == nil {
		t.Error("le cache exclu a été copié")
	}
	if l, _ := os.Readlink(filepath.Join(dst, "raccourci")); l != "Documents/rapport été.odt" {
		t.Errorf("lien mal recréé : %q", l)
	}
}

func TestResumeAfterCut(t *testing.T) {
	inv, home := sourceTree(t)
	dst := filepath.Join(t.TempDir(), "arnaud")
	jpath := filepath.Join(t.TempDir(), "journal.jsonl")

	// Première session : coupure au milieu du gros fichier.
	cli := connect(t, inv, 4<<20)
	if _, err := runCopy(t, cli, jpath, dst); err == nil {
		t.Fatal("la coupure aurait dû interrompre la session")
	}
	cli.RW.Close()
	st, _ := journal.Load(jpath)
	var resumeAt int64
	for _, p := range st.Progress {
		resumeAt = p.Offset
	}
	if resumeAt == 0 {
		t.Fatal("aucun point de reprise enregistré")
	}

	// Seconde session, nouveau code : reprise.
	cli2 := connect(t, inv, 0)
	defer cli2.Close()
	rep, err := runCopy(t, cli2, jpath, dst)
	if err != nil || !rep.OK() {
		t.Fatalf("reprise échouée : %v %+v", err, rep)
	}
	if rep.AlreadyPresent == 0 {
		t.Error("les fichiers terminés auraient dû être sautés")
	}
	sameTree(t, home, dst, ".cache")
	matches, _ := filepath.Glob(filepath.Join(dst, "*", ".bernard-*.part"))
	if len(matches) > 0 {
		t.Errorf("fichiers temporaires restants : %v", matches)
	}
	var dup []string
	filepath.Walk(dst, func(p string, _ os.FileInfo, _ error) error {
		if strings.Contains(p, "(bernard") {
			dup = append(dup, p)
		}
		return nil
	})
	if len(dup) > 0 {
		t.Errorf("la reprise a créé des doublons : %v", dup)
	}
}

func TestAgentRefusesPathsOutsideDatasets(t *testing.T) {
	inv, _ := sourceTree(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	write(t, outside, []byte("secret"))
	cli := connect(t, inv, 0)
	defer cli.Close()
	for _, rel := range []string{"../secret.txt", "/etc/passwd", ".cache/jetable", "raccourci"} {
		_, err := cli.Get(context.Background(), "d1", rel, 0)
		var fe *source.FileError
		if !errors.As(err, &fe) {
			t.Errorf("%q aurait dû être refusé, erreur : %v", rel, err)
		}
	}
	// La session reste utilisable après un refus.
	if _, err := cli.Inventory(context.Background()); err != nil {
		t.Fatalf("session désynchronisée après refus : %v", err)
	}
}

func TestUndoRemovesOnlyWhatBernardCreated(t *testing.T) {
	inv, _ := sourceTree(t)
	dst := filepath.Join(t.TempDir(), "arnaud")
	write(t, filepath.Join(dst, "Documents/deja-la.txt"), []byte("présent avant"))
	jpath := filepath.Join(t.TempDir(), "journal.jsonl")
	cli := connect(t, inv, 0)
	defer cli.Close()
	if _, err := runCopy(t, cli, jpath, dst); err != nil {
		t.Fatal(err)
	}
	// L'utilisateur modifie un fichier copié avant d'annuler.
	write(t, filepath.Join(dst, "vide.txt"), []byte("modifié après copie"))

	rep, err := Undo(jpath)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "Documents/deja-la.txt")); string(b) != "présent avant" {
		t.Fatal("un fichier présent avant la migration a été touché")
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "vide.txt")); string(b) != "modifié après copie" {
		t.Fatal("un fichier modifié depuis la copie a été supprimé")
	}
	if _, err := os.Stat(filepath.Join(dst, "Vidéos/gros.mkv")); err == nil {
		t.Error("un fichier copié par Bernard aurait dû être supprimé")
	}
	if _, err := os.Stat(filepath.Join(dst, "Vidéos")); err == nil {
		t.Error("le dossier vide créé par Bernard aurait dû être supprimé")
	}
	if len(rep.Kept) != 1 {
		t.Errorf("attendu 1 fichier conservé, obtenu %v", rep.Kept)
	}
}
