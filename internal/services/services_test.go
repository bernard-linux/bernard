package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bernard-linux/bernard/internal/sysexec"
)

func TestPauseResume(t *testing.T) {
	var cmds []string
	c := &Controller{Exec: func(_ context.Context, cmd sysexec.Cmd) (string, error) {
		line := cmd.Name + " " + strings.Join(cmd.Args, " ")
		cmds = append(cmds, line)
		if line == "systemctl is-active docker.socket" {
			return "inactive\n", errors.New("3")
		}
		if line == "systemctl is-active docker" {
			return "active\n", nil
		}
		return "", nil
	}}
	resume, err := c.Pause(context.Background(), "docker.socket docker ; --force")
	if err != nil {
		t.Fatal(err)
	}
	resume()
	want := "systemctl is-active docker.socket|systemctl is-active docker|systemctl stop docker|systemctl start docker"
	if got := strings.Join(cmds, "|"); got != want {
		t.Fatalf("\n%s\n%s", got, want)
	}
}

func TestLibvirtRunningVMs(t *testing.T) {
	c := &Controller{Exec: func(_ context.Context, cmd sysexec.Cmd) (string, error) {
		return "win10\n", nil
	}}
	if _, err := c.Pause(context.Background(), Libvirt); err == nil || !strings.Contains(err.Error(), "win10") {
		t.Fatalf("machine allumée non signalée : %v", err)
	}
}

// Arrêter docker.socket arrête aussi docker.service : les deux doivent être
// relancés, puisqu'ils tournaient avant (constaté sur le banc en machines
// virtuelles).
func TestPauseSocketStopsService(t *testing.T) {
	var cmds []string
	up := map[string]bool{"docker.socket": true, "docker": true}
	c := &Controller{Exec: func(_ context.Context, cmd sysexec.Cmd) (string, error) {
		line := cmd.Name + " " + strings.Join(cmd.Args, " ")
		cmds = append(cmds, line)
		switch {
		case cmd.Args[0] == "is-active" && up[cmd.Args[1]]:
			return "active\n", nil
		case cmd.Args[0] == "is-active":
			return "inactive\n", errors.New("3")
		case cmd.Args[0] == "stop" && cmd.Args[1] == "docker.socket":
			up["docker.socket"], up["docker"] = false, false
		case cmd.Args[0] == "stop":
			up[cmd.Args[1]] = false
		case cmd.Args[0] == "start":
			up[cmd.Args[1]] = true
		}
		return "", nil
	}}
	resume, err := c.Pause(context.Background(), "docker.socket docker")
	if err != nil {
		t.Fatal(err)
	}
	resume()
	if !up["docker"] || !up["docker.socket"] {
		t.Fatalf("services non relancés : %v\n%s", up, strings.Join(cmds, "\n"))
	}
}
