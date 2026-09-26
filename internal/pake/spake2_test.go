package pake

import (
	"bytes"
	"testing"
)

func run(t *testing.T, codeA, codeB string, bindA, bindB []byte) (*Keys, *Keys) {
	t.Helper()
	a, msgA, err := Start(RoleTarget, codeA, bindA)
	if err != nil {
		t.Fatal(err)
	}
	b, msgB, err := Start(RoleSource, codeB, bindB)
	if err != nil {
		t.Fatal(err)
	}
	ka, err := a.Finish(msgB)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := b.Finish(msgA)
	if err != nil {
		t.Fatal(err)
	}
	return ka, kb
}

func TestSameCodeAgrees(t *testing.T) {
	ka, kb := run(t, "042917", "042917", []byte("tls"), []byte("tls"))
	if !bytes.Equal(ka.Session, kb.Session) {
		t.Fatal("clés de session différentes")
	}
	if ka.Verify(kb.MyConf) != nil || kb.Verify(ka.MyConf) != nil {
		t.Fatal("confirmations refusées avec le bon code")
	}
}

func TestWrongCodeFails(t *testing.T) {
	ka, kb := run(t, "042917", "042918", []byte("tls"), []byte("tls"))
	if ka.Verify(kb.MyConf) == nil || kb.Verify(ka.MyConf) == nil {
		t.Fatal("un mauvais code a été accepté")
	}
	if bytes.Equal(ka.Session, kb.Session) {
		t.Fatal("clés identiques malgré un mauvais code")
	}
}

func TestDifferentTLSSessionsFail(t *testing.T) {
	// Un intermédiaire actif a deux sessions TLS : les liaisons diffèrent.
	ka, kb := run(t, "123456", "123456", []byte("session-1"), []byte("session-2"))
	if ka.Verify(kb.MyConf) == nil {
		t.Fatal("interception non détectée")
	}
}

func TestConfirmationsAreNotReflectable(t *testing.T) {
	ka, _ := run(t, "123456", "123456", nil, nil)
	if ka.Verify(ka.MyConf) == nil {
		t.Fatal("une confirmation renvoyée telle quelle ne doit pas être acceptée")
	}
}

func TestNewCode(t *testing.T) {
	for i := 0; i < 50; i++ {
		c, err := NewCode()
		if err != nil || len(c) != 6 {
			t.Fatalf("code invalide : %q", c)
		}
	}
}

func TestRejectsInvalidPoint(t *testing.T) {
	a, _, _ := Start(RoleTarget, "000000", nil)
	if _, err := a.Finish([]byte{4, 1, 2, 3}); err == nil {
		t.Fatal("point invalide accepté")
	}
}
