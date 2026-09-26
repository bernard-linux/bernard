package session

import (
	"context"
	"errors"
	"net"
	"testing"
)

// serveOnce accepte une connexion et renvoie le résultat de l'appairage.
func serveOnce(t *testing.T, ln net.Listener, p *Pairer) <-chan error {
	cfg, err := ServerConfig()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		raw, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		c, err := p.Accept(context.Background(), raw, cfg)
		if err == nil {
			c.Close()
		}
		done <- err
	}()
	return done
}

func listen(t *testing.T) net.Listener {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

func TestPairingWithRightCode(t *testing.T) {
	ln := listen(t)
	p, _ := NewPairer("nouveau-pc")
	done := serveOnce(t, ln, p)
	c, err := Dial(context.Background(), ln.Addr().String(), p.Code(), "ancien-pc")
	if err != nil {
		t.Fatalf("appairage refusé : %v", err)
	}
	defer c.Close()
	if c.PeerName != "nouveau-pc" {
		t.Errorf("nom de la cible : %q", c.PeerName)
	}
	if err := <-done; err != nil {
		t.Fatalf("côté cible : %v", err)
	}
	// Le code est à usage unique.
	if _, err := p.current(); !errors.Is(err, ErrCodeRevoked) {
		t.Error("le code aurait dû être consommé")
	}
}

func TestPairingRevokedAfterThreeFailures(t *testing.T) {
	ln := listen(t)
	p, _ := NewPairer("nouveau-pc")
	wrong := "000000"
	if p.Code() == wrong {
		wrong = "111111"
	}
	for i := 0; i < MaxFailures; i++ {
		done := serveOnce(t, ln, p)
		if _, err := Dial(context.Background(), ln.Addr().String(), wrong, "intrus"); err == nil {
			t.Fatal("mauvais code accepté")
		}
		<-done
	}
	done := serveOnce(t, ln, p)
	_, err := Dial(context.Background(), ln.Addr().String(), p.Code(), "ancien-pc")
	if err == nil || err.Error() != ErrCodeRevoked.Error() {
		t.Fatalf("après 3 échecs, même le bon code doit être refusé : %v", err)
	}
	<-done
}
