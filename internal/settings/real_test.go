package settings

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Test réel : écrire puis restaurer les préférences dconf d'un compte non
// connecté. Uniquement en root et sur demande :
//
//	sudo BERNARD_SYSTEM_TESTS=1 go test -run Real ./internal/settings
func TestRealDconfForLoggedOutUser(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("BERNARD_SYSTEM_TESTS") != "1" {
		t.Skip("test système réel désactivé")
	}
	const login = "bernarddconf"
	exec.Command("userdel", "-r", login).Run()
	if out, err := exec.Command("useradd", "-m", login).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	defer exec.Command("userdel", "-r", login).Run()
	home := "/home/" + login
	a := New(t.TempDir())
	ctx := context.Background()
	d := ParseDump("[org/gnome/desktop/interface]\ntext-scaling-factor=1.25\n")
	backup, err := a.ApplyDconf(ctx, login, home, d)
	if err != nil {
		t.Fatal(err)
	}
	read := func() string {
		out, _ := a.asUser(ctx, login, home, "", "dconf", "dump", "/")
		return out
	}
	if !strings.Contains(read(), "text-scaling-factor=1.25") {
		t.Fatalf("réglage non écrit : %q", read())
	}
	if fi, err := os.Stat(home + "/.config/dconf/user"); err != nil || fi.Sys() == nil {
		t.Fatal("base dconf absente")
	}
	if err := a.RestoreDconf(ctx, login, home, backup); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(), "text-scaling-factor") {
		t.Fatalf("réglage non retiré à l'annulation : %q", read())
	}
}
