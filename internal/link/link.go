// Package link maintient la liaison entre l'agent et le moteur malgré les
// coupures : câble débranché, passage du câble au Wi-Fi, changement
// d'adresse IP, mise en veille courte.
//
// Côté source, ServeWithReconnect relance la découverte et rétablit la
// session (sans nouveau code) par la meilleure liaison disponible. Côté cible,
// ReceiveWithReconnect attend la reconnexion et reprend la copie grâce au
// journal. Rien n'est recopié de ce qui était déjà vérifié.
package link

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/bernard-linux/bernard/internal/remote"
	"github.com/bernard-linux/bernard/internal/session"
	"github.com/bernard-linux/bernard/internal/source"
)

// GiveUp est la durée pendant laquelle on cherche à rétablir la liaison.
const GiveUp = 15 * time.Minute

// IsLinkError indique une coupure de liaison (et non une erreur disque ou
// une erreur limitée à un fichier).
func IsLinkError(err error) bool {
	if err == nil {
		return false
	}
	var fe *source.FileError
	if errors.As(err, &fe) {
		return false
	}
	var ne *net.OpError
	return errors.As(err, &ne) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) || errors.Is(err, os.ErrDeadlineExceeded) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ETIMEDOUT) || errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH)
}

// Routes renvoie les adresses possibles de la cible, la meilleure d'abord.
type Routes func(ctx context.Context) []string

// ServeWithReconnect sert le moteur et, en cas de coupure, rétablit la
// session par une autre liaison. Elle se termine quand le moteur dit au revoir.
func ServeWithReconnect(ctx context.Context, conn *session.Conn, srv *remote.Server, routes Routes, name string, log func(string)) error {
	return Serve(ctx, conn, srv, Options{Routes: routes, Name: name, Log: log})
}

// Options règle Serve.
type Options struct {
	Routes Routes
	Name   string
	Log    func(string)
	// Better, s'il est fourni, propose une liaison nettement plus rapide que
	// celle en cours (adresse hôte:port), par exemple un câble réseau
	// branché pendant un transfert en Wi-Fi.
	Better func(current string) (addr, label string, ok bool)
	// CheckEvery est l'intervalle entre deux recherches d'une meilleure
	// liaison (5 s par défaut).
	CheckEvery time.Duration
	// OnConnect est appelé à chaque liaison établie (adresse de la cible).
	OnConnect func(addr string)
}

// Serve sert le moteur, rétablit la session après une coupure et bascule
// d'elle-même sur une liaison plus rapide quand il en apparaît une : la
// session en cours est fermée proprement, puis reprise sur la nouvelle
// liaison (sans nouveau code). Rien de ce qui est vérifié n'est renvoyé.
func Serve(ctx context.Context, conn *session.Conn, srv *remote.Server, o Options) error {
	key := conn.ResumeKey
	if o.Log == nil {
		o.Log = func(string) {}
	}
	if o.CheckEvery <= 0 {
		o.CheckEvery = 5 * time.Second
	}
	var prefer string // liaison à essayer d'abord après une bascule
	if o.OnConnect != nil {
		o.OnConnect(conn.RemoteAddr().String())
	}
	for {
		stop := make(chan struct{})
		switched := make(chan string, 1)
		if o.Better != nil {
			go watchBetter(conn, o, stop, switched)
		}
		// Un arrêt demandé coupe tout de suite, même au milieu d'un gros fichier.
		go func(c *session.Conn) {
			select {
			case <-ctx.Done():
				c.Close()
			case <-stop:
			}
		}(conn)
		err := srv.Serve(ctx, conn)
		close(stop)
		conn.Close()
		prefer = ""
		select {
		case addr := <-switched:
			prefer = addr
		default:
		}
		if err == nil || ctx.Err() != nil || (!IsLinkError(err) && prefer == "") {
			return err
		}
		if prefer == "" {
			o.Log("Liaison perdue. Recherche d'une autre liaison (câble, Wi-Fi)…")
		}
		deadline := time.Now().Add(GiveUp)
		conn = nil
		for conn == nil {
			if time.Now().After(deadline) {
				return errors.New("liaison non rétablie : relancez la commande pour reprendre")
			}
			addrs := o.Routes(ctx)
			if prefer != "" {
				addrs = append([]string{prefer}, addrs...)
			}
			for _, addr := range addrs {
				c, rerr := session.Resume(ctx, addr, key, o.Name)
				if rerr == nil {
					conn = c
					o.Log("Reconnecté via " + addr + ". Le transfert reprend.")
					if o.OnConnect != nil {
						o.OnConnect(addr)
					}
					break
				}
				if errors.Is(rerr, session.ErrNoSession) {
					return rerr
				}
			}
			if conn == nil {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(2 * time.Second):
				}
			}
		}
	}
}

// watchBetter ferme la session quand une liaison nettement plus rapide est
// vue deux fois de suite (un câble à peine branché met quelques secondes à
// obtenir son adresse).
func watchBetter(conn *session.Conn, o Options, stop <-chan struct{}, switched chan<- string) {
	tick := time.NewTicker(o.CheckEvery)
	defer tick.Stop()
	current := conn.RemoteAddr().String()
	streak, last := 0, ""
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		addr, label, ok := o.Better(current)
		if !ok {
			streak, last = 0, ""
			continue
		}
		if addr == last {
			streak++
		} else {
			streak, last = 1, addr
		}
		if streak >= 2 {
			o.Log("Liaison plus rapide détectée (" + label + ") : bascule en cours…")
			switched <- addr
			conn.Close()
			return
		}
	}
}

// Accepter fournit la prochaine session appairée ou reprise.
type Accepter func(ctx context.Context) (*session.Conn, error)

// NewAccepter accepte les connexions entrantes sur ln et réalise l'appairage
// ou la reprise. onRefused est appelé à chaque tentative refusée.
func NewAccepter(ln net.Listener, p *session.Pairer, cfg *tls.Config, wrap func(net.Conn) net.Conn, onRefused func(error)) Accepter {
	return func(ctx context.Context) (*session.Conn, error) {
		for {
			raw, err := ln.Accept()
			if err != nil {
				return nil, err
			}
			if wrap != nil {
				raw = wrap(raw)
			}
			c, err := p.Accept(ctx, raw, cfg)
			if err == nil {
				return c, nil
			}
			if onRefused != nil {
				onRefused(err)
			}
		}
	}
}

// ReceiveWithReconnect exécute run avec la session courante et, en cas de
// coupure, attend la reconnexion puis relance run (qui reprend via le
// journal).
func ReceiveWithReconnect(ctx context.Context, first *session.Conn, accept Accepter, run func(context.Context, *remote.Client) error, log func(string)) error {
	conn := first
	for {
		cli := &remote.Client{RW: conn}
		err := run(ctx, cli)
		if err == nil {
			cli.Close()
			return nil
		}
		conn.Close()
		if ctx.Err() != nil || !IsLinkError(err) {
			return err
		}
		log("Liaison perdue. En attente de l'ancien ordinateur ; le transfert reprendra seul…")
		wctx, cancel := context.WithTimeout(ctx, GiveUp)
		type res struct {
			c   *session.Conn
			err error
		}
		ch := make(chan res, 1)
		go func() {
			c, err := accept(wctx)
			ch <- res{c, err}
		}()
		select {
		case r := <-ch:
			cancel()
			if r.err != nil {
				return r.err
			}
			conn = r.c
			log("Reconnecté. Reprise du transfert.")
		case <-wctx.Done():
			cancel()
			return errors.New("l'ancien ordinateur ne s'est pas reconnecté : relancez les deux commandes pour reprendre")
		}
	}
}
