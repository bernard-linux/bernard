// Commande bernard-agent : s'exécute sur l'ancienne machine (la source).
// Elle ne modifie jamais cette machine.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/bernard-linux/bernard/internal/collect/linux"
	"github.com/bernard-linux/bernard/internal/discovery"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/link"
	"github.com/bernard-linux/bernard/internal/pack"
	"github.com/bernard-linux/bernard/internal/remote"
	"github.com/bernard-linux/bernard/internal/session"
	"github.com/bernard-linux/bernard/internal/sysexec"
	"github.com/bernard-linux/bernard/internal/version"
)

const usage = `bernard-agent — agent source de Bernard, l'assistant de migration vers Linux

Usage :
  bernard-agent connect [--code 123456] [--target hôte:port]
      Trouve le nouvel ordinateur sur le réseau (Wi-Fi, câble, Thunderbolt)
      et lui envoie les données après saisie du code affiché sur celui-ci.
  bernard-agent pack --dest /media/disque/migration
      Écrit un paquet chiffré sur un disque externe.
  bernard-agent inventory [-o inventaire.json] [--no-data]
  bernard-agent version

L'agent ne modifie jamais cette machine.
`

var stdin = bufio.NewReader(os.Stdin)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var code int
	switch os.Args[1] {
	case "inventory":
		code = runInventory(ctx, os.Args[2:])
	case "connect":
		code = runConnect(ctx, os.Args[2:])
	case "pack":
		code = runPack(ctx, os.Args[2:])
	case "version":
		fmt.Println(version.Version)
	default:
		fmt.Fprint(os.Stderr, usage)
		code = 2
	}
	os.Exit(code)
}

// sourceRoot permet de lire un système monté ailleurs (disque retiré d'un
// ancien PC, essais). "/" par défaut.
var sourceRoot = "/"

func collect(ctx context.Context, noData bool) (*inventory.Inventory, error) {
	fmt.Fprintln(os.Stderr, "Inventaire en cours (lecture seule)…")
	return linux.Collect(ctx, linux.Options{Root: sourceRoot, SkipData: noData, AgentVersion: version.Version})
}

func runInventory(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("inventory", flag.ExitOnError)
	out := fs.String("o", "inventaire.json", "fichier de sortie")
	noData := fs.Bool("no-data", false, "ne pas mesurer les dossiers personnels (plus rapide)")
	fs.StringVar(&sourceRoot, "root", "/", "racine du système à migrer (disque monté ailleurs)")
	fs.Parse(args)

	inv, err := collect(ctx, *noData)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Échec :", err)
		return 1
	}
	if err := inv.Save(*out); err != nil {
		fmt.Fprintln(os.Stderr, "Écriture impossible :", err)
		return 1
	}
	printInventory(inv)
	fmt.Println("Inventaire écrit dans", *out)
	return 0
}

func printInventory(inv *inventory.Inventory) {
	var bytes int64
	for _, d := range inv.DataSets {
		bytes += d.SizeBytes
	}
	desktop := inv.Source.Desktop
	if desktop == "" {
		desktop = "bureau non détecté"
	}
	fmt.Printf("Système      : %s %s (%s)\n", inv.Source.Distro, inv.Source.Version, desktop)
	fmt.Printf("Comptes      : %d\n", len(inv.Users))
	fmt.Printf("Applications : %d\n", len(inv.Apps))
	fmt.Printf("Données      : %s\n", humanBytes(bytes))
	for _, w := range inv.Warnings {
		fmt.Println("Attention    :", w)
	}
}

var linkNames = map[string]string{
	discovery.LinkThunderbolt: "câble Thunderbolt / USB4",
	discovery.LinkEthernet:    "câble réseau (RJ45)",
	discovery.LinkWifi:        "Wi-Fi",
	discovery.LinkOther:       "réseau",
}

func describe(r discovery.Route) string {
	s := linkNames[r.Link]
	if r.Speed > 0 {
		s += fmt.Sprintf(", %d Mb/s", r.Speed)
	}
	return s
}

func ask(prompt string) string {
	fmt.Print(prompt)
	line, _ := stdin.ReadString('\n')
	return strings.TrimSpace(line)
}

// chooseTarget renvoie l'adresse et l'identifiant de la cible choisie.
func chooseTarget(ctx context.Context) (string, string, error) {
	fmt.Println("Recherche du nouvel ordinateur (Wi-Fi, câble réseau, Thunderbolt)…")
	targets, err := discovery.Listen(ctx, 0, 4*time.Second)
	if err != nil {
		return "", "", err
	}
	switch len(targets) {
	case 0:
		return "", "", errors.New("aucun ordinateur trouvé. Vérifiez que Bernard est ouvert sur le nouvel ordinateur, " +
			"ou reliez les deux par un câble. Sur certains Wi-Fi (invités, entreprise), les appareils ne se voient pas : " +
			"utilisez --target avec l'adresse affichée sur le nouvel ordinateur")
	case 1:
		t := targets[0]
		fmt.Printf("Trouvé : %s, par %s\n", t.Name, describe(t.Best()))
		return t.Best().Addr, t.ID, nil
	}
	for i, t := range targets {
		fmt.Printf("  %d. %s (%s)\n", i+1, t.Name, describe(t.Best()))
	}
	n, err := strconv.Atoi(ask("Numéro de l'ordinateur : "))
	if err != nil || n < 1 || n > len(targets) {
		return "", "", errors.New("choix invalide")
	}
	return targets[n-1].Best().Addr, targets[n-1].ID, nil
}

func runConnect(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	codeFlag := fs.String("code", "", "code à 6 chiffres affiché sur le nouvel ordinateur")
	target := fs.String("target", "", "adresse hôte:port du nouvel ordinateur (sinon recherche automatique)")
	fs.StringVar(&sourceRoot, "root", "/", "racine du système à migrer (disque monté ailleurs)")
	fs.Parse(args)

	inv, err := collect(ctx, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Échec de l'inventaire :", err)
		return 1
	}
	printInventory(inv)

	if os.Geteuid() != 0 {
		fmt.Println("Remarque : lancé sans sudo, l'agent ne lit que vos propres fichiers et les mots de passe")
		fmt.Println("devront être saisis à nouveau. Pour tout migrer : sudo bernard-agent connect")
	}

	addr, targetID := *target, ""
	if addr == "" {
		if addr, targetID, err = chooseTarget(ctx); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	code := *codeFlag
	if code == "" {
		code = ask("Code affiché sur le nouvel ordinateur : ")
	}
	host, _ := os.Hostname()
	conn, err := session.Dial(ctx, addr, strings.ReplaceAll(code, " ", ""), host)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Appairage impossible :", err)
		return 1
	}
	fmt.Printf("Connecté à %s. Transfert en cours — laissez cette fenêtre ouverte.\n", conn.PeerName)
	fmt.Println("Vous pouvez changer de liaison (débrancher le câble, passer en Wi-Fi) : le transfert reprendra seul.")

	var files, bytes int64
	srv := &remote.Server{Inv: inv, OnFile: func(rel string, n int64) {
		files++
		bytes += n
		if files%200 == 0 {
			fmt.Printf("  %d fichiers envoyés (%s)\n", files, humanBytes(bytes))
		}
	}}
	if os.Geteuid() == 0 {
		var logins []string
		for _, u := range inv.Users {
			logins = append(logins, u.Login)
		}
		srv.Secrets = func() (map[string]string, error) { return linux.ReadPasswordHashes(sourceRoot, logins) }
		srv.Extras = func() (any, error) {
			return linux.CollectExtras(ctx, sourceRoot, inv.Source.Desktop, inv.Users, sysexec.Run), nil
		}
	}

	// Adresses possibles pour se reconnecter : la cible réannoncée par ses
	// balises (toutes liaisons, la meilleure d'abord), puis l'adresse initiale.
	routes := func(ctx context.Context) []string {
		var out []string
		if targetID != "" {
			found, _ := discovery.Listen(ctx, 0, 3*time.Second)
			for _, t := range found {
				if t.ID == targetID {
					for _, r := range t.Routes {
						out = append(out, r.Addr)
					}
				}
			}
		}
		return append(out, addr)
	}
	err = link.ServeWithReconnect(ctx, conn, srv, routes, host, func(msg string) { fmt.Println(msg) })
	if err != nil {
		fmt.Fprintf(os.Stderr, "Transfert interrompu après %d fichiers : %v\n", files, err)
		fmt.Fprintln(os.Stderr, "Relancez la commande (un nouveau code sera demandé) : rien de ce qui est vérifié ne sera renvoyé.")
		return 1
	}
	fmt.Printf("Terminé : %d fichiers envoyés (%s). Rien n'a été modifié sur cet ordinateur.\n", files, humanBytes(bytes))
	return 0
}

func runPack(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("pack", flag.ExitOnError)
	dest := fs.String("dest", "", "dossier vide sur le disque externe")
	fs.StringVar(&sourceRoot, "root", "/", "racine du système à migrer (disque monté ailleurs)")
	fs.Parse(args)
	if *dest == "" {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	inv, err := collect(ctx, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Échec de l'inventaire :", err)
		return 1
	}
	printInventory(inv)

	pass := os.Getenv("BERNARD_PASSPHRASE")
	if pass == "" {
		pass = ask("Phrase de passe pour chiffrer le paquet (8 caractères minimum) : ")
		if ask("Confirmez la phrase de passe : ") != pass {
			fmt.Fprintln(os.Stderr, "Les deux saisies diffèrent.")
			return 1
		}
	}
	var files int64
	var extras any
	var secrets map[string]string
	if os.Geteuid() == 0 {
		// Les réglages (dont les mots de passe Wi-Fi) vont dans le manifeste
		// chiffré du paquet.
		extras = linux.CollectExtras(ctx, sourceRoot, inv.Source.Desktop, inv.Users, sysexec.Run)
		var logins []string
		for _, u := range inv.Users {
			logins = append(logins, u.Login)
		}
		secrets, _ = linux.ReadPasswordHashes(sourceRoot, logins)
	} else {
		fmt.Println("Remarque : sans sudo, ni les mots de passe, ni les réglages du bureau, ni le Wi-Fi ne seront inclus.")
	}
	rep, err := pack.Write(ctx, inv, *dest, pass, pack.WriteOptions{Extras: extras, Secrets: secrets, OnFile: func(string, int64) {
		if files++; files%200 == 0 {
			fmt.Printf("  %d fichiers écrits\n", files)
		}
	}})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Écriture du paquet impossible :", err)
		fmt.Fprintln(os.Stderr, "Videz le dossier de destination avant de recommencer.")
		return 1
	}
	fmt.Printf("Paquet écrit : %d fichiers (%s) dans %s\n", rep.Files, humanBytes(rep.Bytes), *dest)
	for _, e := range rep.Errors {
		fmt.Println("Non inclus :", e)
	}
	fmt.Println("Conservez la phrase de passe : sans elle, le paquet est illisible.")
	return 0
}

func humanBytes(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d o", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %co", float64(b)/float64(div), "kMGTPE"[exp])
}
