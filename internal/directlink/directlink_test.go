package directlink

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bernard-linux/bernard/internal/sysexec"
)

func fakeNet(t *testing.T) {
	root := t.TempDir()
	mk := func(name, typ, carrier string, dev, wireless bool) {
		d := filepath.Join(root, name)
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "type"), []byte(typ+"\n"), 0o644)
		os.WriteFile(filepath.Join(d, "carrier"), []byte(carrier+"\n"), 0o644)
		if dev {
			os.MkdirAll(filepath.Join(d, "device"), 0o755)
		}
		if wireless {
			os.MkdirAll(filepath.Join(d, "wireless"), 0o755)
		}
	}
	mk("enp0s25", "1", "1", true, false)  // câble direct branché
	mk("enp3s0", "1", "0", true, false)   // câble débranché
	mk("wlp2s0", "1", "1", true, true)    // Wi-Fi
	mk("docker0", "1", "1", false, false) // virtuelle
	mk("lo", "772", "1", false, false)
	old := SysNet
	SysNet = root
	t.Cleanup(func() { SysNet = old })
}

func TestScanCreatesLinkLocalAfterGrace(t *testing.T) {
	fakeNet(t)
	oldHas := HasIPv4
	HasIPv4 = func(string) bool { return false }
	t.Cleanup(func() { HasIPv4 = oldHas })

	var cmds []string
	w := &Watcher{Exec: func(_ context.Context, c sysexec.Cmd) (string, error) {
		cmds = append(cmds, c.Name+" "+strings.Join(c.Args, " "))
		return "", nil
	}}
	w.init()
	t0 := time.Now()
	w.Scan(context.Background(), t0)
	w.Scan(context.Background(), t0.Add(5*time.Second))
	if len(cmds) != 0 {
		t.Fatalf("trop tôt : %v", cmds)
	}
	w.Scan(context.Background(), t0.Add(Grace+time.Second))
	if len(cmds) != 2 || !strings.Contains(cmds[0], "ifname enp0s25 ipv4.method link-local") || !strings.HasPrefix(cmds[1], "nmcli connection up bernard-cable-enp0s25") {
		t.Fatalf("commandes : %v", cmds)
	}
	w.Scan(context.Background(), t0.Add(Grace+10*time.Second))
	if len(cmds) != 2 {
		t.Fatalf("créée deux fois : %v", cmds)
	}
	w.Stop()
	if len(cmds) != 3 || cmds[2] != "nmcli connection delete bernard-cable-enp0s25" {
		t.Fatalf("nettoyage : %v", cmds)
	}
}

func TestScanLeavesDHCPAlone(t *testing.T) {
	fakeNet(t)
	oldHas := HasIPv4
	HasIPv4 = func(string) bool { return true } // la box a donné une adresse
	t.Cleanup(func() { HasIPv4 = oldHas })
	var n int
	w := &Watcher{Exec: func(context.Context, sysexec.Cmd) (string, error) { n++; return "", nil }}
	w.init()
	t0 := time.Now()
	w.Scan(context.Background(), t0)
	w.Scan(context.Background(), t0.Add(time.Minute))
	if n != 0 {
		t.Fatal("aucune connexion ne doit être créée quand le DHCP répond")
	}
}
