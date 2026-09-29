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
