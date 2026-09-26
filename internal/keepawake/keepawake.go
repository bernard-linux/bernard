// Package keepawake empêche la mise en veille pendant que l'agent attend le
// nouvel ordinateur ou envoie les données : une machine endormie disparaît du
// réseau et la migration s'arrête.
//
// Sous Linux, un processus enfant « systemd-inhibit … cat » tient le verrou
// de logind. Son entrée standard est un tube ouvert par l'agent : si l'agent
// se termine, même brutalement, le tube se ferme, cat s'arrête et le verrou
// est rendu. Aucun réglage du système n'est modifié.
package keepawake

import (
	"os"
	"os/exec"
)

// Lock représente un verrou actif. Release est sans effet sur un verrou nul.
type Lock struct {
	cmd  *exec.Cmd
	pipe *os.File
}

// What renvoie ce qui est bloqué : la veille et la mise en veille automatique
// ; en administrateur, aussi la fermeture du capot d'un portable.
func What(root bool) string {
	if root {
		return "sleep:idle:handle-lid-switch"
	}
	return "sleep:idle"
}

// Acquire pose le verrou. Il renvoie nil (sans erreur bloquante) si
// systemd-inhibit n'existe pas sur ce système.
func Acquire(why string) *Lock {
	bin, err := exec.LookPath("systemd-inhibit")
	if err != nil {
		return nil
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil
	}
	cmd := exec.Command(bin, "--what="+What(os.Geteuid() == 0), "--who=Bernard",
		"--why="+why, "--mode=block", "cat")
	cmd.Stdin = r
	if err := cmd.Start(); err != nil {
		r.Close()
		w.Close()
		return nil
	}
	r.Close()
	return &Lock{cmd: cmd, pipe: w}
}

// Release rend le verrou.
func (l *Lock) Release() {
	if l == nil {
		return
	}
	l.pipe.Close()
	l.cmd.Wait()
}
