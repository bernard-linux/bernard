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
	key := conn.ResumeKey
	for {
		err := srv.Serve(ctx, conn)
		conn.Close()
		if err == nil || ctx.Err() != nil || !IsLinkError(err) {
			return err
		}
		log("Liaison perdue. Recherche d'une autre liaison (câble, Wi-Fi)…")
		deadline := time.Now().Add(GiveUp)
		conn = nil
		for conn == nil {
			if time.Now().After(deadline) {
				return errors.New("liaison non rétablie : relancez la commande pour reprendre")
			}
			for _, addr := range routes(ctx) {
				c, rerr := session.Resume(ctx, addr, key, name)
				if rerr == nil {
					conn = c
					log("Reconnecté via " + addr + ". Le transfert reprend.")
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
