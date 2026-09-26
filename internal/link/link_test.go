package link_test

import (
	"context"
	"crypto/rand"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bernard-linux/bernard/internal/engine"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/link"
	"github.com/bernard-linux/bernard/internal/remote"
	"github.com/bernard-linux/bernard/internal/session"
	"github.com/bernard-linux/bernard/internal/transfer"
)

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

type logger struct {
	mu    sync.Mutex
	lines []string
}

func (l *logger) log(s string) { l.mu.Lock(); l.lines = append(l.lines, s); l.mu.Unlock() }
func (l *logger) has(sub string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.lines {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// Scénario : transfert par « câble » (127.0.0.1), coupure au milieu d'un gros
// fichier, reprise automatique par le « Wi-Fi » (127.0.0.2), sans nouveau code.
func TestSwitchLinkMidTransfer(t *testing.T) {
	home := t.TempDir()
	big := make([]byte, 8<<20)
	rand.Read(big)
	os.WriteFile(filepath.Join(home, "film.mkv"), big, 0o644)
	os.WriteFile(filepath.Join(home, "notes.txt"), []byte("notes"), 0o644)
	inv := &inventory.Inventory{
		Schema:   inventory.Schema,
		Source:   inventory.Source{OS: "linux", Hostname: "ancien"},
		Users:    []inventory.User{{ID: "u1", Login: "arnaud", Home: home}},
		DataSets: []inventory.DataSet{{ID: "d1", User: "u1", Path: home}},
	}

	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	pairer, _ := session.NewPairer("nouveau")
	cfg, _ := session.ServerConfig()
	first := true
	accept := link.NewAccepter(ln, pairer, cfg, func(c net.Conn) net.Conn {
		if first {
			first = false
			return &cutConn{Conn: c, limit: 3 << 20}
		}
		return c
	}, nil)

	agentLog, targetLog := &logger{}, &logger{}
	agentDone := make(chan error, 1)
	go func() {
		conn, err := session.Dial(context.Background(), "127.0.0.1:"+port, pairer.Code(), "ancien")
		if err != nil {
			agentDone <- err
			return
		}
		routes := func(context.Context) []string { return []string{"127.0.0.2:" + port} }
		agentDone <- link.ServeWithReconnect(context.Background(), conn, &remote.Server{Inv: inv}, routes, "ancien", agentLog.log)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	firstConn, err := accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	jp := filepath.Join(dest, ".bernard", "journal.jsonl")
	err = link.ReceiveWithReconnect(ctx, firstConn, accept, func(ctx context.Context, cli *remote.Client) error {
		sum, err := engine.RunAll(ctx, cli, dest, jp, nil)
		if err == nil && !sum.OK() {
			t.Errorf("bilan incomplet : %+v", sum)
		}
		return err
	}, targetLog.log)
	if err != nil {
		t.Fatalf("la migration aurait dû survivre à la coupure : %v", err)
	}
	if err := <-agentDone; err != nil {
		t.Fatalf("agent : %v", err)
	}
	if !agentLog.has("127.0.0.2") || !targetLog.has("Reconnecté") {
		t.Fatalf("pas de bascule observée : agent=%v cible=%v", agentLog.lines, targetLog.lines)
	}
	for _, name := range []string{"film.mkv", "notes.txt"} {
		a, _, _ := transfer.HashFile(filepath.Join(home, name))
		b, _, err := transfer.HashFile(filepath.Join(dest, "arnaud", name))
		if err != nil || a != b {
			t.Errorf("%s différent après bascule", name)
		}
	}
}

func TestResumeRequiresTheSessionKey(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	pairer, _ := session.NewPairer("nouveau")
	cfg, _ := session.ServerConfig()
	accept := link.NewAccepter(ln, pairer, cfg, nil, nil)
	go accept(context.Background())
	// Aucune session appairée : une reprise, même avec une « clé », est refusée.
	if _, err := session.Resume(context.Background(), ln.Addr().String(), make([]byte, 32), "intrus"); err == nil {
		t.Fatal("reprise acceptée sans appairage préalable")
	}
}

// Transfert commencé en « Wi-Fi » (127.0.0.1) ; un « câble » (127.0.0.2)
// apparaît : l'agent bascule de lui-même, sans coupure ni nouveau code.
func TestSwitchToFasterLink(t *testing.T) {
	home := t.TempDir()
	big := make([]byte, 24<<20)
	rand.Read(big)
	os.WriteFile(filepath.Join(home, "film.mkv"), big, 0o644)
	for i := 0; i < 300; i++ {
		os.WriteFile(filepath.Join(home, "note"+strconv.Itoa(i)+".txt"), big[i*100:i*100+700], 0o644)
	}
	inv := &inventory.Inventory{
		Schema:   inventory.Schema,
		Source:   inventory.Source{OS: "linux", Hostname: "ancien"},
		Users:    []inventory.User{{ID: "u1", Login: "arnaud", Home: home}},
		DataSets: []inventory.DataSet{{ID: "d1", User: "u1", Path: home}},
	}
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	pairer, _ := session.NewPairer("nouveau")
	cfg, _ := session.ServerConfig()
	// Liaison « Wi-Fi » lente : chaque lecture côté cible est freinée.
	var n int
	accept := link.NewAccepter(ln, pairer, cfg, func(c net.Conn) net.Conn {
		n++
		if n == 1 {
			return &slow{Conn: c}
		}
		return c
	}, nil)

	agentLog, targetLog := &logger{}, &logger{}
	agentDone := make(chan error, 1)
	go func() {
		conn, err := session.Dial(context.Background(), "127.0.0.1:"+port, pairer.Code(), "ancien")
		if err != nil {
			agentDone <- err
			return
		}
		cable := "127.0.0.2:" + port
		agentDone <- link.Serve(context.Background(), conn, &remote.Server{Inv: inv}, link.Options{
			Routes: func(context.Context) []string { return []string{cable} },
			Name:   "ancien", Log: agentLog.log, CheckEvery: 50 * time.Millisecond,
			Better: func(cur string) (string, string, bool) {
				return cable, "câble réseau (RJ45)", !strings.HasPrefix(cur, "127.0.0.2")
			},
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	firstConn, err := accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	jp := filepath.Join(dest, ".bernard", "journal.jsonl")
	err = link.ReceiveWithReconnect(ctx, firstConn, accept, func(ctx context.Context, cli *remote.Client) error {
		sum, err := engine.RunAll(ctx, cli, dest, jp, nil)
		if err == nil && !sum.OK() {
			t.Errorf("bilan incomplet : %+v", sum)
		}
		return err
	}, targetLog.log)
	if err != nil {
		t.Fatalf("la bascule aurait dû être transparente : %v", err)
	}
	if err := <-agentDone; err != nil {
		t.Fatalf("agent : %v", err)
	}
	if !agentLog.has("plus rapide") || !agentLog.has("127.0.0.2") {
		t.Fatalf("pas de bascule : %v", agentLog.lines)
	}
	if agentLog.has("Liaison perdue") {
		t.Errorf("une bascule volontaire ne doit pas être présentée comme une perte : %v", agentLog.lines)
	}
	a, _, _ := transfer.HashFile(filepath.Join(home, "film.mkv"))
	b, _, err := transfer.HashFile(filepath.Join(dest, "arnaud", "film.mkv"))
	if err != nil || a != b {
		t.Error("film.mkv différent après bascule")
	}
}

type slow struct{ net.Conn }

func (s *slow) Read(p []byte) (int, error) {
	time.Sleep(3 * time.Millisecond)
	if len(p) > 32<<10 {
		p = p[:32<<10]
	}
	return s.Conn.Read(p)
}

// Un arrêt demandé côté source coupe immédiatement, même en plein gros
// fichier.
func TestStopIsImmediate(t *testing.T) {
	home := t.TempDir()
	big := make([]byte, 64<<20)
	rand.Read(big)
	os.WriteFile(filepath.Join(home, "gros.bin"), big, 0o644)
	inv := &inventory.Inventory{Schema: inventory.Schema, Source: inventory.Source{OS: "linux", Hostname: "a"},
		Users: []inventory.User{{ID: "u1", Login: "a", Home: home}}, DataSets: []inventory.DataSet{{ID: "d1", User: "u1", Path: home}}}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	pairer, _ := session.NewPairer("n")
	cfg, _ := session.ServerConfig()
	acc := make(chan *session.Conn, 1)
	go func() {
		raw, _ := ln.Accept()
		c, _ := pairer.Accept(context.Background(), &slow{Conn: raw}, cfg)
		acc <- c
	}()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		conn, err := session.Dial(context.Background(), ln.Addr().String(), pairer.Code(), "a")
		if err != nil {
			done <- err
			return
		}
		done <- link.Serve(ctx, conn, &remote.Server{Inv: inv}, link.Options{Routes: func(context.Context) []string { return nil }})
	}()
	cli := &remote.Client{RW: <-acc}
	st, err := cli.Get(context.Background(), "d1", "gros.bin", 0)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1<<20)
	st.Read(buf)
	start := time.Now()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("l'arrêt n'a pas coupé l'envoi en cours")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("arrêt trop lent : %v", d)
	}
}
