// Package sysexec exécute des commandes système de façon remplaçable dans
// les tests.
package sysexec

import (
	"bufio"
	"context"
	"errors"
	"os/exec"
	"strings"
)

// Runner exécute une commande et renvoie sa sortie standard.
type Runner func(ctx context.Context, name string, args ...string) (string, error)

// ErrMissingCommand signale qu'un outil n'est pas installé.
var ErrMissingCommand = errors.New("commande absente")

// Exec est le Runner réel. La locale est forcée à C pour des sorties stables.
func Exec(ctx context.Context, name string, args ...string) (string, error) {
	if _, err := exec.LookPath(name); err != nil {
		return "", ErrMissingCommand
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	return string(out), err
}

// Lines découpe une sortie en lignes non vides, sans espaces aux extrémités.
func Lines(s string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// Cmd décrit une commande avec entrée standard et variables d'environnement.
// L'entrée standard sert à passer les secrets (mots de passe) sans qu'ils
// apparaissent dans la liste des processus.
type Cmd struct {
	Name  string
	Args  []string
	Stdin string
	Env   []string
}

// Executor exécute une Cmd ; remplaçable dans les tests.
type Executor func(ctx context.Context, c Cmd) (string, error)

// Run est l'Executor réel. En cas d'échec, l'erreur inclut la sortie
// d'erreur de la commande.
func Run(ctx context.Context, c Cmd) (string, error) {
	if _, err := exec.LookPath(c.Name); err != nil {
		return "", ErrMissingCommand
	}
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Env = append(append(cmd.Environ(), "LC_ALL=C"), c.Env...)
	if c.Stdin != "" {
		cmd.Stdin = strings.NewReader(c.Stdin)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 400 {
			msg = msg[len(msg)-400:]
		}
		return string(out), &CmdError{Cmd: c.Name, Err: err, Stderr: msg}
	}
	return string(out), nil
}

// CmdError est l'échec d'une commande système.
type CmdError struct {
	Cmd    string
	Err    error
	Stderr string
}

func (e *CmdError) Error() string {
	if e.Stderr != "" {
		return e.Cmd + " : " + e.Stderr
	}
	return e.Cmd + " : " + e.Err.Error()
}

func (e *CmdError) Unwrap() error { return e.Err }
