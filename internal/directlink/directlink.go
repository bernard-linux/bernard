// Package directlink rend utilisable un câble branché directement entre les
// deux ordinateurs (réseau RJ45 d'un PC à l'autre, câble Thunderbolt).
//
// Sans box ni serveur DHCP, NetworkManager attend une adresse qui ne vient
// jamais, puis abandonne (« l'activation de la connexion a échoué ») : le
// câble reste inutilisé. Bernard repère alors l'interface (câble branché,
// aucune adresse IPv4 depuis quelques secondes) et y active une connexion
// provisoire en « lien local » : chaque ordinateur prend seul une adresse en
// 169.254.x.x, comme le font macOS et Windows. La connexion est retirée à la
// fin de la migration ; les réglages de l'utilisateur ne sont jamais
// modifiés.
package directlink

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bernard-linux/bernard/internal/sysexec"
)

// Prefix nomme les connexions provisoires de Bernard.
const Prefix = "bernard-cable-"

// Réglages (remplacés dans les tests).
var (
	SysNet   = "/sys/class/net"
	Interval = 3 * time.Second
	// Grace laisse le temps à un vrai DHCP (câble vers une box) de répondre.
	Grace = 12 * time.Second
	// HasIPv4 indique si une interface a déjà une adresse IPv4.
	HasIPv4 = func(name string) bool {
		ifc, err := net.InterfaceByName(name)
		if err != nil {
			return false
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				return true
			}
		}
		return false
	}
)

// Watcher surveille les câbles pendant une migration.
type Watcher struct {
	Exec sysexec.Executor
	Log  func(string)

	mu      sync.Mutex
	since   map[string]time.Time // câble branché sans adresse depuis…
	created map[string]bool      // interface → connexion provisoire créée
	failed  map[string]bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// Start lance la surveillance. Sans NetworkManager, ne fait rien.
func Start(log func(string)) *Watcher {
	w := &Watcher{Exec: sysexec.Run, Log: log}
	w.init()
	if _, err := w.Exec(context.Background(), sysexec.Cmd{Name: "nmcli", Args: []string{"-v"}}); err != nil {
		return w
	}
	w.removeStale()
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel, w.done = cancel, make(chan struct{})
	go func() {
		defer close(w.done)
		t := time.NewTicker(Interval)
		defer t.Stop()
		for {
			w.Scan(ctx, time.Now())
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	return w
}

func (w *Watcher) init() {
	w.since, w.created, w.failed = map[string]time.Time{}, map[string]bool{}, map[string]bool{}
}

func (w *Watcher) log(s string) {
	if w.Log != nil {
		w.Log(s)
	}
}

// Stop arrête la surveillance et retire les connexions provisoires.
func (w *Watcher) Stop() {
	if w == nil {
		return
	}
	if w.cancel != nil {
		w.cancel()
		<-w.done
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for dev := range w.created {
		w.Exec(context.Background(), sysexec.Cmd{Name: "nmcli", Args: []string{"connection", "delete", Prefix + dev}})
	}
	w.created = map[string]bool{}
}

// removeStale retire les connexions provisoires laissées par une session
// interrompue (arrêt brutal).
func (w *Watcher) removeStale() {
	out, err := w.Exec(context.Background(), sysexec.Cmd{Name: "nmcli", Args: []string{"-t", "-f", "NAME", "connection", "show"}})
	if err != nil {
		return
	}
	for _, l := range sysexec.Lines(out) {
		if strings.HasPrefix(l, Prefix) {
			w.Exec(context.Background(), sysexec.Cmd{Name: "nmcli", Args: []string{"connection", "delete", l}})
		}
	}
}

// wired liste les interfaces filaires physiques (RJ45, Thunderbolt) dont le
// câble est branché.
func wired() []string {
	entries, _ := os.ReadDir(SysNet)
	var out []string
	for _, e := range entries {
		name := e.Name()
		base := filepath.Join(SysNet, name)
		if _, err := os.Stat(filepath.Join(base, "device")); err != nil {
			continue // virtuelle : lo, ponts, docker, veth…
		}
		if _, err := os.Stat(filepath.Join(base, "wireless")); err == nil {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(base, "type")); err != nil || strings.TrimSpace(string(b)) != "1" {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(base, "carrier")); err != nil || strings.TrimSpace(string(b)) != "1" {
			continue
		}
		out = append(out, name)
	}
	return out
}

var devRe = func(s string) bool {
	if s == "" || len(s) > 15 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// Scan fait un passage : un câble branché resté sans adresse IPv4 plus de
// Grace reçoit une connexion en lien local.
func (w *Watcher) Scan(ctx context.Context, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	plugged := map[string]bool{}
	for _, dev := range wired() {
		plugged[dev] = true
		if w.created[dev] || w.failed[dev] || !devRe(dev) {
			continue
		}
		if HasIPv4(dev) {
			delete(w.since, dev)
			continue
		}
		first, ok := w.since[dev]
		if !ok {
			w.since[dev] = now
			continue
		}
		if now.Sub(first) < Grace {
			continue
		}
		name := Prefix + dev
		_, err := w.Exec(ctx, sysexec.Cmd{Name: "nmcli", Args: []string{"connection", "add", "type", "ethernet",
			"con-name", name, "ifname", dev, "ipv4.method", "link-local", "ipv6.method", "link-local",
			"connection.autoconnect", "no"}})
		if err == nil {
			_, err = w.Exec(ctx, sysexec.Cmd{Name: "nmcli", Args: []string{"connection", "up", name}})
			w.created[dev] = true // créée : à retirer même si l'activation échoue
		}
		if err != nil {
			w.failed[dev] = true
			w.log("Câble direct sur " + dev + " : adresse automatique impossible (" + err.Error() + ")")
			continue
		}
		w.log("Câble direct détecté sur " + dev + " : adresse automatique en lien local (169.254.x.x)")
	}
	for dev := range w.since {
		if !plugged[dev] {
			delete(w.since, dev)
		}
	}
}
