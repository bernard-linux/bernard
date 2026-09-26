// Package discovery permet à l'agent de trouver la cible sans saisir
// d'adresse. La cible émet chaque seconde une balise UDP sur chacune de ses
// interfaces réseau (Wi-Fi, câble RJ45, Thunderbolt/USB4), en indiquant le
// type et la vitesse de la liaison. L'agent écoute, regroupe les balises par
// cible et choisit la liaison la plus rapide.
//
// Une balise maison plutôt que mDNS : aucun service à installer sous Windows
// ou macOS, et elle fonctionne aussi sur un câble direct (adresses 169.254).
// La balise ne contient aucun secret : la sécurité repose sur l'appairage.
package discovery

import (
	"context"
	"encoding/json"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Port UDP des balises.
const Port = 51515

// Types de liaisons, du plus rapide au plus lent à vitesse égale.
const (
	LinkThunderbolt = "thunderbolt"
	LinkEthernet    = "ethernet"
	LinkWifi        = "wifi"
	LinkOther       = "other"
)

// Beacon est le contenu d'une balise.
type Beacon struct {
	Bernard int    `json:"bernard"` // version du protocole de balise
	ID      string `json:"id"`      // identifiant aléatoire de la session cible
	Name    string `json:"name"`    // nom affiché (nom de la machine)
	Port    int    `json:"port"`    // port TCP d'appairage
	Link    string `json:"link"`
	Speed   int    `json:"speed,omitempty"` // Mb/s, si connue
}

// Route est une façon de joindre une cible.
type Route struct {
	Addr  string `json:"addr"` // hôte:port TCP
	Link  string `json:"link"`
	Speed int    `json:"speed"`
}

// Target est une cible découverte, avec toutes ses liaisons.
type Target struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Routes []Route `json:"routes"` // triées, la meilleure en premier
}

// Best renvoie la meilleure liaison.
func (t Target) Best() Route { return t.Routes[0] }

func rank(r Route) int {
	speed := r.Speed
	if speed <= 0 {
		speed = map[string]int{LinkThunderbolt: 10000, LinkEthernet: 1000, LinkWifi: 100}[r.Link]
	}
	bonus := map[string]int{LinkThunderbolt: 3, LinkEthernet: 2, LinkWifi: 1}[r.Link]
	return speed*10 + bonus
}

// Faster indique que la liaison a est nettement préférable à b : un câble
// plutôt que le Wi-Fi, Thunderbolt plutôt qu'un câble réseau.
func Faster(a, b Route) bool {
	return rank(a)/10 > rank(b)/10 || (rank(a)/10 == rank(b)/10 && rank(a) > rank(b))
}

func sortRoutes(rs []Route) {
	sort.SliceStable(rs, func(i, j int) bool { return rank(rs[i]) > rank(rs[j]) })
}

// ---------------------------------------------------------------- cible

// Iface est une interface capable d'émettre.
type Iface struct {
	Name      string
	IP        net.IP
	Broadcast net.IP
	Link      string
	Speed     int
}

// Interfaces liste les interfaces IPv4 actives, hors boucle locale, avec leur
// adresse de diffusion.
func Interfaces() []Iface {
	var out []Iface
	ifs, _ := net.Interfaces()
	for _, itf := range ifs {
		if itf.Flags&net.FlagUp == 0 || itf.Flags&net.FlagLoopback != 0 || itf.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrs, _ := itf.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil {
				continue
			}
			ip, mask := ipn.IP.To4(), ipn.Mask
			if len(mask) == 16 {
				mask = mask[12:]
			}
			bc := make(net.IP, 4)
			for i := range bc {
				bc[i] = ip[i] | ^mask[i]
			}
			link, speed := linkInfo(itf.Name)
			out = append(out, Iface{Name: itf.Name, IP: ip, Broadcast: bc, Link: link, Speed: speed})
		}
	}
	return out
}

// Announce émet les balises jusqu'à l'annulation du contexte. extra permet
// d'ajouter des destinations (tests, adresse saisie à la main).
//
// Chaque balise part de sa propre interface, avec l'adresse de celle-ci :
// quand le Wi-Fi et un câble sont sur le même réseau (même box), l'agent
// voit ainsi deux adresses distinctes et peut choisir le câble.
func Announce(ctx context.Context, b Beacon, extra ...*net.UDPAddr) error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return err
	}
	defer conn.Close()
	socks := map[string]*net.UDPConn{} // par interface et adresse
	defer func() {
		for _, c := range socks {
			c.Close()
		}
	}()
	b.Bernard = 1
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		live := map[string]bool{}
		for _, itf := range Interfaces() {
			bb := b
			bb.Link, bb.Speed = itf.Link, itf.Speed
			msg, _ := json.Marshal(bb)
			id := itf.Name + "|" + itf.IP.String()
			live[id] = true
			c := socks[id]
			if c == nil {
				c = ifaceSocket(itf)
				socks[id] = c
			}
			dst := &net.UDPAddr{IP: itf.Broadcast, Port: Port}
			if c == nil || func() error { _, err := c.WriteToUDP(msg, dst); return err }() != nil {
				conn.WriteToUDP(msg, dst) // repli : socket non liée
			}
		}
		for id, c := range socks { // interfaces disparues
			if !live[id] {
				if c != nil {
					c.Close()
				}
				delete(socks, id)
			}
		}
		for _, dst := range extra {
			bb := b
			bb.Link = LinkOther
			msg, _ := json.Marshal(bb)
			conn.WriteToUDP(msg, dst)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// ifaceSocket ouvre une socket liée à l'adresse de l'interface (et, en
// administrateur, à l'interface elle-même). nil si impossible.
func ifaceSocket(itf Iface) *net.UDPConn {
	lc := net.ListenConfig{Control: bindToDevice(itf.Name)}
	pc, err := lc.ListenPacket(context.Background(), "udp4", net.JoinHostPort(itf.IP.String(), "0"))
	if err != nil {
		return nil
	}
	return pc.(*net.UDPConn)
}

// ---------------------------------------------------------------- agent

// Listen écoute les balises pendant wait et renvoie les cibles trouvées.
// port = 0 utilise Port.
func Listen(ctx context.Context, port int, wait time.Duration) ([]Target, error) {
	if port == 0 {
		port = Port
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: port})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline := time.Now().Add(wait)
	found := map[string]*Target{}
	seen := map[string]bool{}
	buf := make([]byte, 2048)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		conn.SetReadDeadline(minTime(deadline, time.Now().Add(200*time.Millisecond)))
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		var b Beacon
		if json.Unmarshal(buf[:n], &b) != nil || b.Bernard != 1 || b.ID == "" || b.Port <= 0 || b.Port > 65535 {
			continue
		}
		addr := net.JoinHostPort(from.IP.String(), strconv.Itoa(b.Port))
		if seen[b.ID+addr] {
			continue
		}
		seen[b.ID+addr] = true
		t := found[b.ID]
		if t == nil {
			t = &Target{ID: b.ID, Name: b.Name}
			found[b.ID] = t
		}
		t.Routes = append(t.Routes, Route{Addr: addr, Link: b.Link, Speed: b.Speed})
	}
	var out []Target
	for _, t := range found {
		sortRoutes(t.Routes)
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// Wait écoute les balises jusqu'à trouver au moins une cible, sans limite de
// durée (seule l'annulation de ctx l'interrompt). L'écoute est passive :
// aucun paquet n'est émis, attendre des heures ne coûte rien au réseau.
// every, s'il n'est pas nul, est appelé environ chaque minute avec la durée
// d'attente écoulée, pour rassurer l'utilisateur.
func Wait(ctx context.Context, port int, every func(time.Duration)) ([]Target, error) {
	start := time.Now()
	last := start
	for {
		targets, err := Listen(ctx, port, 3*time.Second)
		if err != nil {
			return nil, err
		}
		if len(targets) > 0 {
			return targets, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if every != nil && time.Since(last) >= time.Minute {
			last = time.Now()
			every(time.Since(start))
		}
	}
}

// ---------------------------------------------------------------- suivi

// Tracker écoute les balises en continu et garde, pour chaque cible, les
// liaisons vues récemment. Une seule écoute sert à la fois à attendre la
// cible, à se reconnecter après une coupure et à repérer un câble branché
// en cours de transfert.
type Tracker struct {
	mu    sync.Mutex
	names map[string]string
	seen  map[string]map[string]seenRoute // id → adresse → liaison
}

type seenRoute struct {
	Route
	at time.Time
}

// Track commence l'écoute ; elle s'arrête avec ctx. port = 0 utilise Port.
func Track(ctx context.Context, port int) (*Tracker, error) {
	if port == 0 {
		port = Port
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: port})
	if err != nil {
		return nil, err
	}
	t := &Tracker{names: map[string]string{}, seen: map[string]map[string]seenRoute{}}
	go func() { <-ctx.Done(); conn.Close() }()
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				time.Sleep(100 * time.Millisecond)
				continue
			}
			var b Beacon
			if json.Unmarshal(buf[:n], &b) != nil || b.Bernard != 1 || b.ID == "" || b.Port <= 0 || b.Port > 65535 {
				continue
			}
			addr := net.JoinHostPort(from.IP.String(), strconv.Itoa(b.Port))
			t.mu.Lock()
			t.names[b.ID] = b.Name
			if t.seen[b.ID] == nil {
				t.seen[b.ID] = map[string]seenRoute{}
			}
			t.seen[b.ID][addr] = seenRoute{Route{Addr: addr, Link: b.Link, Speed: b.Speed}, time.Now()}
			t.mu.Unlock()
		}
	}()
	return t, nil
}

// Fresh est la durée pendant laquelle une liaison vue reste valable.
const Fresh = 4 * time.Second

// Targets renvoie les cibles vues récemment, avec leurs liaisons triées.
func (t *Tracker) Targets() []Target {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []Target
	for id, routes := range t.seen {
		tg := Target{ID: id, Name: t.names[id]}
		for _, r := range routes {
			if time.Since(r.at) < Fresh {
				tg.Routes = append(tg.Routes, r.Route)
			}
		}
		if len(tg.Routes) > 0 {
			sortRoutes(tg.Routes)
			out = append(out, tg)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Target renvoie une cible précise, si elle a été vue récemment.
func (t *Tracker) Target(id string) (Target, bool) {
	for _, tg := range t.Targets() {
		if tg.ID == id {
			return tg, true
		}
	}
	return Target{}, false
}

// Wait attend au moins une cible, sans limite de durée (ctx l'interrompt).
// every, s'il n'est pas nul, est appelé environ chaque minute.
func (t *Tracker) Wait(ctx context.Context, every func(time.Duration)) ([]Target, error) {
	start, last := time.Now(), time.Now()
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	for {
		if tg := t.Targets(); len(tg) > 0 {
			// Laisser le temps aux balises des autres interfaces d'arriver.
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(1500 * time.Millisecond):
			}
			return t.Targets(), nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick.C:
		}
		if every != nil && time.Since(last) >= time.Minute {
			last = time.Now()
			every(time.Since(start))
		}
	}
}

// BetterRoute renvoie une liaison nettement plus rapide que celle utilisée
// (current = adresse hôte:port de la connexion en cours), s'il y en a une.
func (t *Tracker) BetterRoute(id, current string) (Route, bool) {
	tg, ok := t.Target(id)
	if !ok {
		return Route{}, false
	}
	curHost, _, _ := net.SplitHostPort(current)
	var cur *Route
	for i, r := range tg.Routes {
		if h, _, _ := net.SplitHostPort(r.Addr); h == curHost {
			cur = &tg.Routes[i]
		}
	}
	if cur == nil {
		return Route{}, false // liaison inconnue (adresse saisie à la main)
	}
	if best := tg.Best(); best.Addr != cur.Addr && Faster(best, *cur) {
		return best, true
	}
	return Route{}, false
}
