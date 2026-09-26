package engine

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/remote"
	"github.com/bernard-linux/bernard/internal/session"
)

// slowConn ajoute une latence à chaque lecture, comme un Wi-Fi domestique.
type slowConn struct {
	net.Conn
	delay time.Duration
}

func (c *slowConn) Read(p []byte) (int, error) {
	time.Sleep(c.delay)
	return c.Conn.Read(p)
}

// TestSmallFilesThroughput mesure le débit sur beaucoup de petits fichiers,
// avec 2 ms de latence réseau. Lancé seulement avec BERNARD_BENCH=1.
func TestSmallFilesThroughput(t *testing.T) {
	if os.Getenv("BERNARD_BENCH") == "" {
		t.Skip("BERNARD_BENCH=1 pour mesurer")
	}
	home := t.TempDir()
	const n = 2000
	for i := 0; i < n; i++ {
		write(t, filepath.Join(home, fmt.Sprintf("d%02d/f%04d.txt", i%40, i)), random(3000))
	}
	inv := &inventory.Inventory{
		Schema:   inventory.Schema,
		Source:   inventory.Source{OS: "linux", Hostname: "ancien-pc"},
		Users:    []inventory.User{{ID: "u1", Login: "a", Home: home}},
		DataSets: []inventory.DataSet{{ID: "d1", User: "u1", Kind: "home", Path: home}},
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	pairer, _ := session.NewPairer("cible")
	cfg, _ := session.ServerConfig()
	acc := make(chan *session.Conn, 1)
	go func() {
		raw, _ := ln.Accept()
		c, err := pairer.Accept(context.Background(), &slowConn{raw, benchDelay()}, cfg)
		if err != nil {
			t.Error(err)
		}
		acc <- c
	}()
	go func() {
		c, err := session.Dial(context.Background(), ln.Addr().String(), pairer.Code(), "source")
		if err != nil {
			return
		}
		defer c.Close()
		(&remote.Server{Inv: inv}).Serve(context.Background(), c)
	}()
	cli := &remote.Client{RW: <-acc}
	defer cli.Close()
	dst := filepath.Join(t.TempDir(), "a")
	start := time.Now()
	rep, err := runCopy(t, cli, filepath.Join(t.TempDir(), "j.jsonl"), dst)
	if err != nil || !rep.OK() || rep.Files != n {
		t.Fatalf("copie : %v %+v", err, rep)
	}
	d := time.Since(start)
	t.Logf("%d petits fichiers en %v : %.0f fichiers/s", n, d.Round(time.Millisecond), float64(n)/d.Seconds())
}

func benchDelay() time.Duration {
	d, err := time.ParseDuration(os.Getenv("BERNARD_BENCH_DELAY"))
	if err != nil {
		return 2 * time.Millisecond
	}
	return d
}
