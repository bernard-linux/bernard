package keepawake

import "testing"

func TestWhat(t *testing.T) {
	if What(false) != "sleep:idle" || What(true) != "sleep:idle:handle-lid-switch" {
		t.Fatal("verrous inattendus")
	}
}

func TestAcquireReleaseNeverBlocks(t *testing.T) {
	// Sans logind (conteneur, CI), Acquire peut échouer : l'agent doit
	// continuer, et Release doit rendre la main.
	l := Acquire("test")
	l.Release()
	var nilLock *Lock
	nilLock.Release()
}
