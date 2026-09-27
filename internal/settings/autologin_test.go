package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoLoginGDM(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "etc/gdm3/custom.conf")
	os.MkdirAll(filepath.Dir(f), 0o755)
	orig := "# GDM configuration\n[daemon]\nAutomaticLoginEnable=True\nAutomaticLogin=tmp\nWaylandEnable=false\n\n[security]\n"
	os.WriteFile(f, []byte(orig), 0o644)

	al := DetectAutoLogin(root)
	if al == nil || al.User != "tmp" {
		t.Fatalf("détection : %+v", al)
	}
	a := &Applier{StateDir: t.TempDir()}
	pairs, err := a.DisableAutoLogin(root)
	if err != nil || len(pairs) != 1 {
		t.Fatalf("%v %v", pairs, err)
	}
	b, _ := os.ReadFile(f)
	if !strings.Contains(string(b), "AutomaticLoginEnable=False") || !strings.Contains(string(b), "WaylandEnable=false") {
		t.Fatalf("contenu :\n%s", b)
	}
	if DetectAutoLogin(root) != nil {
		t.Fatal("toujours active")
	}
	if err := RestoreFile(pairs[0][0], pairs[0][1]); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(f); string(b) != orig {
		t.Fatalf("restauration :\n%s", b)
	}
}

func TestAutoLoginLightDM(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "etc/lightdm/lightdm.conf")
	os.MkdirAll(filepath.Dir(f), 0o755)
	os.WriteFile(f, []byte("[Seat:*]\nautologin-guest=false\nautologin-user=tmp\nautologin-user-timeout=0\n"), 0o644)
	if al := DetectAutoLogin(root); al == nil || al.User != "tmp" {
		t.Fatalf("détection : %+v", al)
	}
	a := &Applier{StateDir: t.TempDir()}
	if _, err := a.DisableAutoLogin(root); err != nil {
		t.Fatal(err)
	}
	if DetectAutoLogin(root) != nil {
		t.Fatal("toujours active")
	}
	if _, err := a.DisableAutoLogin(root); err != ErrSkipped {
		t.Fatalf("second passage : %v", err)
	}
}

func TestAutoLoginNone(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "etc/gdm3/custom.conf")
	os.MkdirAll(filepath.Dir(f), 0o755)
	os.WriteFile(f, []byte("[daemon]\n#AutomaticLoginEnable=True\n#AutomaticLogin=user1\n"), 0o644)
	if al := DetectAutoLogin(root); al != nil {
		t.Fatalf("aucune attendue : %+v", al)
	}
}
