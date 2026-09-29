// Commande bernard-window : la fenêtre dédiée de Bernard.
//
// Elle affiche l'interface servie par le moteur sur 127.0.0.1, avec les
// droits de l'utilisateur (jamais en administrateur). Module Go séparé : elle
// seule dépend de WebKitGTK et d'un compilateur C, le reste de Bernard reste
// un binaire Go autonome.
package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	webview "github.com/webview/webview_go"
)

// french : langue de la session, lue comme internal/i18n (module séparé,
// qui ne peut pas l'importer) : français si la session est en français.
func french() bool {
	for _, k := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		for _, l := range strings.Split(os.Getenv(k), ":") {
			if l = strings.TrimSpace(l); l != "" && l != "C" && l != "POSIX" && !strings.HasPrefix(l, "C.") {
				return strings.HasPrefix(strings.ToLower(l), "fr")
			}
		}
	}
	return true
}

// tr choisit le texte selon la langue de la session.
func tr(fr, en string) string {
	if french() {
		return fr
	}
	return en
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, tr("usage : bernard-window http://127.0.0.1:PORT/?t=JETON", "usage: bernard-window http://127.0.0.1:PORT/?t=TOKEN"))
		os.Exit(2)
	}
	u, err := url.Parse(os.Args[1])
	// La fenêtre n'ouvre que l'interface locale de Bernard.
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
		fmt.Fprintln(os.Stderr, tr("adresse refusée : seule l'interface locale de Bernard peut être ouverte", "address refused: only Bernard's local interface can be opened"))
		os.Exit(2)
	}
	w := webview.New(false)
	defer w.Destroy()
	w.SetTitle(tr("Bernard — assistant de migration", "Bernard — Migration Assistant"))
	w.SetSize(1040, 720, webview.HintNone)
	w.SetSize(760, 560, webview.HintMin)
	w.Navigate(u.String())
	w.Run()
}
