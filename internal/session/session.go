// Package session établit le canal entre l'agent (source) et le moteur
// (cible) : TLS 1.3 avec certificats éphémères, authentifié par un échange
// SPAKE2 sur le code à 6 chiffres et lié à la session TLS.
//
// Pourquoi ce montage : TLS chiffre ; SPAKE2 prouve que l'autre extrémité
// connaît le code, sans jamais l'envoyer ; la liaison (clés exportées de TLS)
// garantit qu'aucun intermédiaire ne s'est glissé entre les deux.
package session

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/bernard-linux/bernard/internal/pake"
	"github.com/bernard-linux/bernard/internal/wire"
)

// Durées et limites de l'appairage.
const (
	CodeTTL          = 5 * time.Minute
	MaxFailures      = 3
	handshakeTimeout = 30 * time.Second
	exporterLabel    = "EXPORTER-bernard-pairing-v1"
	// KeepAlive détecte une liaison morte (câble débranché) sans attendre
	// le délai par défaut du système.
	KeepAlive = 5 * time.Second
)

// Erreurs d'appairage.
var (
	ErrCodeExpired = errors.New("code d'appairage expiré : un nouveau code est nécessaire")
	ErrCodeRevoked = errors.New("trop d'essais incorrects : un nouveau code est nécessaire")
	ErrBadCode     = pake.ErrBadCode
	ErrNoSession   = errors.New("aucune session à reprendre : un nouvel appairage est nécessaire")
)

// Conn est une session appairée.
type Conn struct {
	*tls.Conn
	PeerName string
	// ResumeKey permet de rétablir la session sur une autre liaison sans
	// nouveau code (ex. câble débranché, bascule sur le Wi-Fi).
	ResumeKey []byte
}

type hello struct {
	Type     string `json:"type"`
	Protocol int    `json:"protocol"`
	Name     string `json:"name"`
	Spake    []byte `json:"spake,omitempty"`
	Conf     []byte `json:"conf,omitempty"`
	Error    string `json:"error,omitempty"`
}

// resumeProof prouve la connaissance de la clé de session, liée à la
// nouvelle session TLS : un enregistrement rejoué ne sert à rien.
func resumeProof(key, bind []byte, role string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("bernard-resume-v1|" + role + "|"))
	m.Write(bind)
	return m.Sum(nil)
}

// ServerConfig crée une configuration TLS avec un certificat éphémère,
// jamais réutilisé ni stocké.
func ServerConfig() (*tls.Config, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	}, nil
}

// clientConfig n'authentifie pas le certificat : c'est SPAKE2, lié à la
// session TLS, qui authentifie la cible.
func clientConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13, InsecureSkipVerify: true}
}

func binding(c *tls.Conn) ([]byte, error) {
	cs := c.ConnectionState()
	return cs.ExportKeyingMaterial(exporterLabel, nil, 32)
}

// Pairer garde le code courant de la cible et compte les échecs.
type Pairer struct {
	Name     string
	mu       sync.Mutex
	code     string
	expires  time.Time
	failures int
	used     bool
	resume   []byte // clé de la session appairée, pour les reconnexions
}

// NewPairer tire un premier code.
func NewPairer(name string) (*Pairer, error) {
	p := &Pairer{Name: name}
	return p, p.Renew()
}

// Renew tire un nouveau code et remet les compteurs à zéro.
func (p *Pairer) Renew() error {
	code, err := pake.NewCode()
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.code, p.expires, p.failures, p.used = code, time.Now().Add(CodeTTL), 0, false
	return nil
}

// RenewIfNeeded tire un nouveau code si le courant est expiré ou révoqué
// après trop d'échecs, et indique s'il l'a fait.
func (p *Pairer) RenewIfNeeded() (bool, error) {
	p.mu.Lock()
	stale := !p.used && (p.failures >= MaxFailures || time.Now().After(p.expires))
	p.mu.Unlock()
	if !stale {
		return false, nil
	}
	return true, p.Renew()
}

// Code renvoie le code à afficher.
func (p *Pairer) Code() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code
}

func (p *Pairer) current() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case p.used || p.failures >= MaxFailures:
		return "", ErrCodeRevoked
	case time.Now().After(p.expires):
		return "", ErrCodeExpired
	}
	return p.code, nil
}

func (p *Pairer) result(ok bool, key []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ok {
		p.resume = key
		p.used = true // un code ne sert qu'une fois
	} else {
		p.failures++
	}
}

// Accept réalise l'appairage côté cible sur une connexion entrante.
func (p *Pairer) Accept(ctx context.Context, raw net.Conn, cfg *tls.Config) (*Conn, error) {
	c := tls.Server(raw, cfg)
	c.SetDeadline(time.Now().Add(handshakeTimeout))
	if err := c.HandshakeContext(ctx); err != nil {
		c.Close()
		return nil, err
	}
	fail := func(err error) (*Conn, error) {
		wire.WriteJSON(c, hello{Type: "error", Error: err.Error()})
		c.Close()
		return nil, err
	}
	var in hello
	if err := wire.ReadJSON(c, &in); err != nil {
		c.Close()
		return nil, err
	}
	if in.Type == "resume" && in.Protocol == wire.Protocol {
		return p.acceptResume(c, in)
	}
	if in.Type != "hello" || in.Protocol != wire.Protocol {
		return fail(fmt.Errorf("version de protocole incompatible (%d)", in.Protocol))
	}
	code, err := p.current()
	if err != nil {
		return fail(err)
	}
	bind, err := binding(c)
	if err != nil {
		return fail(err)
	}
	st, msg, err := pake.Start(pake.RoleTarget, code, bind)
	if err != nil {
		return fail(err)
	}
	keys, err := st.Finish(in.Spake)
	if err != nil {
		p.result(false, nil)
		return fail(err)
	}
	if err := wire.WriteJSON(c, hello{Type: "hello", Protocol: wire.Protocol, Name: p.Name, Spake: msg, Conf: keys.MyConf}); err != nil {
		c.Close()
		return nil, err
	}
	var conf hello
	if err := wire.ReadJSON(c, &conf); err != nil {
		// La source raccroche quand notre confirmation ne correspond pas à
		// son code : c'est un code erroné.
		p.result(false, nil)
		c.Close()
		return nil, ErrBadCode
	}
	if conf.Type != "confirm" || keys.Verify(conf.Conf) != nil {
		p.result(false, nil)
		c.Close()
		return nil, ErrBadCode
	}
	p.result(true, keys.Session)
	c.SetDeadline(time.Time{})
	return &Conn{Conn: c, PeerName: in.Name, ResumeKey: keys.Session}, nil
}

func (p *Pairer) acceptResume(c *tls.Conn, in hello) (*Conn, error) {
	p.mu.Lock()
	key := p.resume
	p.mu.Unlock()
	fail := func(err error) (*Conn, error) {
		wire.WriteJSON(c, hello{Type: "error", Error: err.Error()})
		c.Close()
		return nil, err
	}
	if key == nil {
		return fail(ErrNoSession)
	}
	bind, err := binding(c)
	if err != nil {
		return fail(err)
	}
	if !hmac.Equal(in.Conf, resumeProof(key, bind, pake.RoleSource)) {
		return fail(ErrNoSession)
	}
	if err := wire.WriteJSON(c, hello{Type: "resumed", Protocol: wire.Protocol, Name: p.Name, Conf: resumeProof(key, bind, pake.RoleTarget)}); err != nil {
		c.Close()
		return nil, err
	}
	c.SetDeadline(time.Time{})
	return &Conn{Conn: c, PeerName: in.Name, ResumeKey: key}, nil
}

// Resume rétablit une session déjà appairée, éventuellement par une autre
// liaison, sans nouveau code.
func Resume(ctx context.Context, addr string, key []byte, name string) (*Conn, error) {
	c, err := dialTLS(ctx, addr)
	if err != nil {
		return nil, err
	}
	bind, err := binding(c)
	if err != nil {
		c.Close()
		return nil, err
	}
	if err := wire.WriteJSON(c, hello{Type: "resume", Protocol: wire.Protocol, Name: name, Conf: resumeProof(key, bind, pake.RoleSource)}); err != nil {
		c.Close()
		return nil, err
	}
	var in hello
	if err := wire.ReadJSON(c, &in); err != nil {
		c.Close()
		return nil, err
	}
	if in.Type != "resumed" || !hmac.Equal(in.Conf, resumeProof(key, bind, pake.RoleTarget)) {
		c.Close()
		if in.Error != "" {
			return nil, errors.New(in.Error)
		}
		return nil, ErrNoSession
	}
	c.SetDeadline(time.Time{})
	return &Conn{Conn: c, PeerName: in.Name, ResumeKey: key}, nil
}

func dialTLS(ctx context.Context, addr string) (*tls.Conn, error) {
	d := net.Dialer{Timeout: 10 * time.Second, KeepAlive: KeepAlive}
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	c := tls.Client(raw, clientConfig())
	c.SetDeadline(time.Now().Add(handshakeTimeout))
	if err := c.HandshakeContext(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// Dial se connecte à la cible et réalise l'appairage côté source.
func Dial(ctx context.Context, addr, code, name string) (*Conn, error) {
	c, err := dialTLS(ctx, addr)
	if err != nil {
		return nil, err
	}
	bind, err := binding(c)
	if err != nil {
		c.Close()
		return nil, err
	}
	st, msg, err := pake.Start(pake.RoleSource, code, bind)
	if err != nil {
		c.Close()
		return nil, err
	}
	if err := wire.WriteJSON(c, hello{Type: "hello", Protocol: wire.Protocol, Name: name, Spake: msg}); err != nil {
		c.Close()
		return nil, err
	}
	var in hello
	if err := wire.ReadJSON(c, &in); err != nil {
		c.Close()
		return nil, err
	}
	if in.Type == "error" {
		c.Close()
		return nil, errors.New(in.Error)
	}
	keys, err := st.Finish(in.Spake)
	if err != nil {
		c.Close()
		return nil, err
	}
	if err := keys.Verify(in.Conf); err != nil {
		c.Close()
		return nil, err
	}
	if err := wire.WriteJSON(c, hello{Type: "confirm", Conf: keys.MyConf}); err != nil {
		c.Close()
		return nil, err
	}
	c.SetDeadline(time.Time{})
	return &Conn{Conn: c, PeerName: in.Name, ResumeKey: keys.Session}, nil
}
