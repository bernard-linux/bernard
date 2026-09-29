package apply

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bernard-linux/bernard/internal/aptrepo"
	"github.com/bernard-linux/bernard/internal/journal"
	"github.com/bernard-linux/bernard/internal/plan"
	"github.com/bernard-linux/bernard/internal/settings"
	"github.com/bernard-linux/bernard/internal/sysexec"
	"github.com/bernard-linux/bernard/internal/system"
)

func TestAddReposRetargetAndDropBroken(t *testing.T) {
	root := t.TempDir()
	oldRoot, oldList := AptRoot, AptListDir
	AptRoot, AptListDir = root, filepath.Join(root, "etc/apt/sources.list.d")
	os.MkdirAll(AptListDir, 0o755)
	t.Cleanup(func() { AptRoot, AptListDir = oldRoot, oldList })

	f, a, p, inv, jp := setup(t)
	inv.Source.Codename = "jammy"
	p.Target.Codename = "noble"
	inv.AptSources = []aptrepo.Source{
		{File: "brave.list", Content: "deb [signed-by=/usr/share/keyrings/brave.gpg] https://brave.example stable main\n",
			URIs: []string{"brave.example"}, Keys: map[string]string{"/usr/share/keyrings/brave.gpg": "Q0xF"}},
		{File: "vieux-ppa.list", Content: "deb http://ppa.example/vieux/ubuntu jammy main\n", URIs: []string{"ppa.example/vieux/ubuntu"}},
	}
	p.Actions = append([]plan.Action{
		{Op: plan.OpAddRepo, Label: "brave.example", Package: "brave.list", Selected: true},
		{Op: plan.OpAddRepo, Label: "ppa.example", Package: "vieux-ppa.list", Selected: true},
	}, p.Actions...)
	// apt-get update : le PPA n'existe pas pour « noble ».
	exec := a.Sys.Exec
	var ppaContent string
	a.Sys.Exec = func(ctx context.Context, c sysexec.Cmd) (string, error) {
		if c.Name == "apt-get" && len(c.Args) > 0 && c.Args[0] == "update" {
			b, _ := os.ReadFile(filepath.Join(AptListDir, "vieux-ppa.list"))
			if len(b) > 0 {
				ppaContent = string(b)
			}
			return "Hit:1 https://brave.example stable InRelease\nErr:2 http://ppa.example/vieux/ubuntu noble Release\n  404  Not Found\n", nil
		}
		return exec(ctx, c)
	}
	rep, err := a.System(context.Background(), p, inv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ppaContent, " noble main") {
		t.Errorf("nom de code non adapté : %q", ppaContent)
	}
	if b, _ := os.ReadFile(filepath.Join(AptListDir, "brave.list")); !strings.Contains(string(b), "brave.example") {
		t.Error("dépôt Brave non ajouté")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "usr/share/keyrings/brave.gpg")); string(b) != "CLE" {
		t.Errorf("clé : %q", b)
	}
	if _, err := os.Stat(filepath.Join(AptListDir, "vieux-ppa.list")); err == nil {
		t.Error("le PPA injoignable devait être retiré")
	}
	if _, ok := rep.Failed["Dépôt ppa.example"]; !ok {
		t.Errorf("échec du PPA non signalé : %v", rep.Failed)
	}

	st, _ := journal.Load(jp)
	UndoSystem(context.Background(), st, &system.System{Exec: f.exec}, &settings.Applier{Exec: f.exec})
	if _, err := os.Stat(filepath.Join(AptListDir, "brave.list")); err == nil {
		t.Error("annulation : dépôt Brave toujours présent")
	}
	if _, err := os.Stat(filepath.Join(root, "usr/share/keyrings/brave.gpg")); err == nil {
		t.Error("annulation : clé toujours présente")
	}
}
