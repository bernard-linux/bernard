package system

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bernard-linux/bernard/internal/sysexec"
)

func fakeRoot(t *testing.T, group string) {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "etc"), 0o755)
	os.WriteFile(filepath.Join(root, "etc/passwd"), []byte(
		"root:x:0:0:root:/root:/bin/bash\n"+
			"tmp:x:1000:1000:Tmp:/home/tmp:/bin/bash\n"+
			"arnaud:x:1001:1001:Arnaud:/home/arnaud:/bin/bash\n"+
			"gdm:x:120:125::/var/lib/gdm3:/bin/false\n"), 0o644)
	os.WriteFile(filepath.Join(root, "etc/group"), []byte(group), 0o644)
	old := Root
	Root = root
	t.Cleanup(func() { Root = old })
}

func TestCheckRemovable(t *testing.T) {
	fakeRoot(t, "sudo:x:27:tmp,arnaud\n")
	if _, err := CheckRemovable("tmp"); err != nil {
		t.Fatalf("tmp devrait être supprimable : %v", err)
	}
	fakeRoot(t, "sudo:x:27:tmp\n")
	if _, err := CheckRemovable("tmp"); err != ErrLastAdmin {
		t.Fatalf("dernier administrateur : %v", err)
	}
	if _, err := CheckRemovable("gdm"); err == nil {
		t.Fatal("compte système refusé attendu")
	}
}

func TestScheduleAndCancel(t *testing.T) {
	fakeRoot(t, "sudo:x:27:tmp,arnaud\n")
	var cmds []string
	s := &System{Exec: func(_ context.Context, c sysexec.Cmd) (string, error) {
		cmds = append(cmds, c.Name+" "+strings.Join(c.Args, " "))
		return "", nil
	}}
	if err := s.ScheduleRemoval(context.Background(), "tmp", "/usr/bin/bernard"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(unitPath("tmp"))
	if !strings.Contains(string(b), "ExecStart=/usr/bin/bernard remove-account tmp") || !RemovalScheduled("tmp") {
		t.Fatalf("unité :\n%s", b)
	}
	if err := s.CancelRemoval(context.Background(), "tmp"); err != nil || RemovalScheduled("tmp") {
		t.Fatalf("annulation : %v", err)
	}
	if len(cmds) != 2 || !strings.HasPrefix(cmds[0], "systemctl enable") || !strings.HasPrefix(cmds[1], "systemctl disable") {
		t.Fatalf("commandes : %v", cmds)
	}
	if err := s.ScheduleRemoval(context.Background(), "tmp; rm -rf /", "/usr/bin/bernard"); err == nil {
		t.Fatal("identifiant invalide accepté")
	}
}
