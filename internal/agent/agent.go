// Package agent regroupe ce que fait l'ancien ordinateur (la source), pour
// la commande bernard-agent comme pour l'assistant graphique : inventaire,
// appairage, envoi des données avec reprise et bascule de liaison.
// L'ancien ordinateur n'est jamais modifié.
package agent

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/bernard-linux/bernard/internal/collect/linux"
	"github.com/bernard-linux/bernard/internal/discovery"
	"github.com/bernard-linux/bernard/internal/i18n"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/link"
	"github.com/bernard-linux/bernard/internal/remote"
	"github.com/bernard-linux/bernard/internal/services"
	"github.com/bernard-linux/bernard/internal/session"
	"github.com/bernard-linux/bernard/internal/sysexec"
	"github.com/bernard-linux/bernard/internal/version"
)

// Collect fait l'inventaire (lecture seule). root vaut "/" pour cet
// ordinateur, ou le point de montage d'un système qui ne tourne pas.
func Collect(ctx context.Context, root string, noData bool) (*inventory.Inventory, error) {
	if root == "" {
		root = "/"
	}
	return linux.Collect(ctx, linux.Options{
		Root: root, SkipData: noData, AgentVersion: version.Version,
		Offline: filepath.Clean(root) != "/",
	})
}

// NewServer prépare les réponses au nouvel ordinateur. En administrateur,
// les mots de passe et les réglages système (Wi-Fi, bureau) sont servis.
// onFile est appelé après chaque fichier, onBytes à chaque bloc envoyé.
func NewServer(ctx context.Context, inv *inventory.Inventory, root string, onFile, onBytes func(string, int64), onStatus func(remote.Status)) *remote.Server {
	if root == "" {
		root = "/"
	}
	srv := &remote.Server{Inv: inv, OnFile: onFile, OnBytes: onBytes, OnStatus: onStatus}
	if os.Geteuid() == 0 {
		var logins []string
		for _, u := range inv.Users {
			logins = append(logins, u.Login)
		}
		srv.Secrets = func() (map[string]string, error) { return linux.ReadPasswordHashes(root, logins) }
		srv.Extras = func() (any, error) {
			return linux.CollectExtras(ctx, root, inv.Source.Desktop, inv.Users, sysexec.Run), nil
		}
		if filepath.Clean(root) == "/" {
			svc := services.New()
			srv.Prepare = func(ds inventory.DataSet) (func(), error) { return svc.Pause(ctx, ds.Service) }
		}
	}
	return srv
}

// Pair s'appaire au nouvel ordinateur avec le code qu'il affiche.
func Pair(ctx context.Context, addr, code string) (*session.Conn, error) {
	host, _ := os.Hostname()
	return session.Dial(ctx, addr, code, host)
}

// Serve envoie les données jusqu'à la fin, en rétablissant la liaison après
// une coupure et en basculant d'elle-même sur une liaison plus rapide (câble
// branché en cours de route). tr peut être nul (adresse saisie à la main).
func Serve(ctx context.Context, conn *session.Conn, srv *remote.Server, tr *discovery.Tracker, targetID, addr string, log func(string), onConnect func(addr string)) error {
	host, _ := os.Hostname()
	o := link.Options{
		Name: host, Log: log, OnConnect: onConnect,
		Routes: func(ctx context.Context) []string {
			var out []string
			if tr != nil {
				if t, ok := tr.Target(targetID); ok {
					for _, r := range t.Routes {
						out = append(out, r.Addr)
					}
				}
			}
			return append(out, addr)
		},
	}
	if tr != nil {
		o.Better = func(current string) (string, string, bool) {
			r, ok := tr.BetterRoute(targetID, current)
			return r.Addr, Describe(r), ok
		}
	}
	return link.Serve(ctx, conn, srv, o)
}

var linkNames = map[string]string{
	discovery.LinkThunderbolt: i18n.N("câble Thunderbolt / USB4"),
	discovery.LinkEthernet:    i18n.N("câble réseau (RJ45)"),
	discovery.LinkWifi:        i18n.N("Wi-Fi"),
	discovery.LinkOther:       i18n.N("réseau"),
}

// LinkName nomme le type de liaison en français.
func LinkName(link string) string {
	if n, ok := linkNames[link]; ok {
		return i18n.T(n)
	}
	return i18n.T("réseau")
}

// Describe nomme une liaison avec son débit, s'il est connu.
func Describe(r discovery.Route) string {
	s := LinkName(r.Link)
	if r.Speed > 0 {
		s += fmt.Sprintf(", %d Mb/s", r.Speed)
	}
	return s
}

// RouteFor retrouve la liaison d'une adresse parmi celles de la cible.
func RouteFor(tr *discovery.Tracker, targetID, addr string) (discovery.Route, bool) {
	if tr == nil {
		return discovery.Route{}, false
	}
	t, ok := tr.Target(targetID)
	if !ok {
		return discovery.Route{}, false
	}
	host, _, _ := net.SplitHostPort(addr)
	for _, r := range t.Routes {
		if h, _, _ := net.SplitHostPort(r.Addr); h == host {
			return r, true
		}
	}
	return discovery.Route{}, false
}
