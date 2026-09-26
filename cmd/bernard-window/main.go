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

	webview "github.com/webview/webview_go"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage : bernard-window http://127.0.0.1:PORT/?t=JETON")
		os.Exit(2)
	}
	u, err := url.Parse(os.Args[1])
	// La fenêtre n'ouvre que l'interface locale de Bernard.
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
		fmt.Fprintln(os.Stderr, "adresse refusée : seule l'interface locale de Bernard peut être ouverte")
		os.Exit(2)
	}
	w := webview.New(false)
	defer w.Destroy()
	w.SetTitle("Bernard — assistant de migration")
	w.SetSize(1040, 720, webview.HintNone)
	w.SetSize(760, 560, webview.HintMin)
	w.Navigate(u.String())
	w.Run()
}
