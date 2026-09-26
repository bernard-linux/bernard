package linux

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// ReadPasswordHashes lit les hachages de mots de passe des comptes demandés
// dans /etc/shadow. Il faut les droits administrateur ; sans eux, la carte
// renvoyée est vide et le moteur demandera un nouveau mot de passe.
//
// Ces hachages ne figurent jamais dans l'inventaire ni dans un fichier : ils
// ne transitent que par la session chiffrée, sur demande du moteur.
func ReadPasswordHashes(root string, logins []string) (map[string]string, error) {
	if root == "" {
		root = "/"
	}
	want := map[string]bool{}
	for _, l := range logins {
		want[l] = true
	}
	f, err := os.Open(filepath.Join(root, "etc/shadow"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ":")
		if len(fields) < 2 || !want[fields[0]] {
			continue
		}
		h := fields[1]
		// Comptes verrouillés ou sans mot de passe : rien à reprendre.
		if h == "" || strings.HasPrefix(h, "!") || strings.HasPrefix(h, "*") {
			continue
		}
		out[fields[0]] = h
	}
	return out, sc.Err()
}
