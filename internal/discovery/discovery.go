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

func sortRoutes(rs []Route) {
	sort.SliceStable(rs, func(i, j int) bool { return rank(rs[i]) > rank(rs[j]) })
}

// ---------------------------------------------------------------- cible

// Iface est une interface capable d'émettre.
type Iface struct {
	Name      string
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
			out = append(out, Iface{Name: itf.Name, Broadcast: bc, Link: link, Speed: speed})
		}
	}
	return out
}

// Announce émet les balises jusqu'à l'annulation du contexte. extra permet
// d'ajouter des destinations (tests, adresse saisie à la main).
func Announce(ctx context.Context, b Beacon, extra ...*net.UDPAddr) error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return err
	}
	defer conn.Close()
	b.Bernard = 1
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		for _, itf := range Interfaces() {
			bb := b
			bb.Link, bb.Speed = itf.Link, itf.Speed
			msg, _ := json.Marshal(bb)
			conn.WriteToUDP(msg, &net.UDPAddr{IP: itf.Broadcast, Port: Port})
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
