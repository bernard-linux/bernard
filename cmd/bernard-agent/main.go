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
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bernard-linux/bernard/internal/agent"
	"github.com/bernard-linux/bernard/internal/collect/linux"
	"github.com/bernard-linux/bernard/internal/directlink"
	"github.com/bernard-linux/bernard/internal/discovery"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/keepawake"
	"github.com/bernard-linux/bernard/internal/pack"
	"github.com/bernard-linux/bernard/internal/remote"
	"github.com/bernard-linux/bernard/internal/services"
	"github.com/bernard-linux/bernard/internal/sysexec"
	"github.com/bernard-linux/bernard/internal/version"
)

const usage = `bernard-agent — agent source de Bernard, l'assistant de migration vers Linux

Usage :
  bernard-agent connect [--code 123456] [--target hôte:port] [--timeout 2h]
      Attend le nouvel ordinateur sur le réseau (Wi-Fi, câble, Thunderbolt),
      aussi longtemps qu'il le faut, puis lui envoie les données après saisie
      du code affiché sur celui-ci. Peut être lancé avant même d'installer
      Linux sur le nouvel ordinateur ; la mise en veille est bloquée.
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
	return agent.Collect(ctx, sourceRoot, noData)
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

func ask(prompt string) string {
	fmt.Print(prompt)
	line, _ := stdin.ReadString('\n')
	return strings.TrimSpace(line)
}

// chooseTarget attend le nouvel ordinateur (sans limite) et renvoie son
// adresse et son identifiant.
func chooseTarget(ctx context.Context, tr *discovery.Tracker) (string, string, error) {
	fmt.Println("En attente du nouvel ordinateur (Wi-Fi, câble réseau, Thunderbolt)…")
	fmt.Println("Ouvrez Bernard sur le nouvel ordinateur, maintenant ou plus tard : cet ordinateur l'attendra")
	fmt.Println("aussi longtemps qu'il le faut, sans charger le réseau. Ctrl+C pour abandonner.")
	targets, err := tr.Wait(ctx, func(d time.Duration) {
		fmt.Printf("  toujours en attente (%s)…\n", humanDuration(d))
		if d >= 5*time.Minute && d < 6*time.Minute {
			fmt.Println("  Si Bernard est déjà ouvert sur le nouvel ordinateur : les deux sont-ils sur le même réseau ?")
			fmt.Println("  Sur un Wi-Fi invité ou d'entreprise, reliez-les par un câble ou utilisez --target.")
		}
	})
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", "", errors.New("délai dépassé : aucun ordinateur trouvé")
		}
		if ctx.Err() != nil {
			return "", "", errors.New("attente abandonnée")
		}
		return "", "", err
	}
	if len(targets) == 1 {
		t := targets[0]
		fmt.Printf("Trouvé : %s, par %s\n", t.Name, agent.Describe(t.Best()))
		return t.Best().Addr, t.ID, nil
	}
	for i, t := range targets {
		fmt.Printf("  %d. %s (%s)\n", i+1, t.Name, agent.Describe(t.Best()))
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
	timeout := fs.Duration("timeout", 0, "abandonner si le nouvel ordinateur n'apparaît pas dans ce délai (ex. 2h ; 0 = attendre indéfiniment)")
	fs.StringVar(&sourceRoot, "root", "/", "racine du système à migrer (disque monté ailleurs)")
	fs.Parse(args)

	if os.Geteuid() != 0 {
		fmt.Println("Remarque : lancé sans sudo, l'agent ne lit que vos propres fichiers et les mots de passe")
		fmt.Println("devront être saisis à nouveau. Pour tout migrer : sudo bernard-agent connect")
	}

	// Pas de mise en veille tant que l'agent attend ou envoie : un ordinateur
	// endormi disparaît du réseau.
	lock := keepawake.Acquire("Migration Bernard : attente du nouvel ordinateur et envoi des données")
	defer lock.Release()
	if lock == nil {
		fmt.Println("Remarque : impossible de bloquer la mise en veille ; désactivez-la le temps de la migration.")
	}

	// Câble direct entre les deux ordinateurs : adresse automatique.
	cable := directlink.Start(func(m string) { fmt.Println(m) })
	defer cable.Stop()

	// Une seule écoute des balises : attente, reconnexion, câble branché.
	tr, err := discovery.Track(ctx, 0)
	if err != nil && *target == "" {
		fmt.Fprintln(os.Stderr, "Écoute du réseau impossible (Bernard est-il déjà ouvert sur cet ordinateur ?) :", err)
		return 1
	}

	addr, targetID := *target, ""
	if addr == "" {
		wctx, cancel := ctx, context.CancelFunc(func() {})
		if *timeout > 0 {
			wctx, cancel = context.WithTimeout(ctx, *timeout)
		}
		addr, targetID, err = chooseTarget(wctx, tr)
		cancel()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	// L'inventaire est fait maintenant, et non au lancement : l'attente a pu
	// durer des heures, et les fichiers ont pu changer entre-temps.
	inv, err := collect(ctx, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Échec de l'inventaire :", err)
		return 1
	}
	printInventory(inv)

	code := *codeFlag
	if code == "" {
		code = ask("Code affiché sur le nouvel ordinateur : ")
	}
	conn, err := agent.Pair(ctx, addr, strings.ReplaceAll(code, " ", ""))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Appairage impossible :", err)
		return 1
	}
	fmt.Printf("Connecté à %s. Transfert en cours — laissez cette fenêtre ouverte.\n", conn.PeerName)
	fmt.Println("Vous pouvez brancher un câble réseau à tout moment : le transfert basculera dessus de lui-même.")

	var files, bytes int64
	var lastPrint time.Time
	srv := agent.NewServer(ctx, inv, sourceRoot, func(rel string, n int64) {
		files++
	}, func(rel string, n int64) {
		bytes += n
		if time.Since(lastPrint) > 5*time.Second {
			lastPrint = time.Now()
			fmt.Printf("  %d fichiers envoyés (%s)\n", files, humanBytes(bytes))
		}
	}, func(st remote.Status) {
		switch st.Phase {
		case "system":
			fmt.Println("Le nouvel ordinateur installe les comptes et les applications…")
		case "copy":
			if st.Planned > 0 {
				fmt.Printf("Copie des fichiers : %s à envoyer.\n", humanBytes(st.Planned))
			}
		case "settings":
			fmt.Println("Le nouvel ordinateur applique les réglages…")
		}
	})
	err = agent.Serve(ctx, conn, srv, tr, targetID, addr, func(msg string) { fmt.Println(msg) }, nil)
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
	lock := keepawake.Acquire("Migration Bernard : écriture du paquet sur le disque externe")
	defer lock.Release()

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
	rep, err := pack.Write(ctx, inv, *dest, pass, pack.WriteOptions{Extras: extras, Secrets: secrets, Prepare: packPrepare(ctx), OnFile: func(string, int64) {
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

func humanDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	h, m := int(d.Hours()), int(d.Minutes())%60
	switch {
	case h == 0:
		return fmt.Sprintf("%d min", m)
	case m == 0:
		return fmt.Sprintf("%d h", h)
	}
	return fmt.Sprintf("%d h %02d", h, m)
}

// packPrepare arrête les services (bases…) pendant l'écriture de leurs
// données dans le paquet, sur la machine courante en administrateur.
func packPrepare(ctx context.Context) func(inventory.DataSet) (func(), error) {
	f := services.PrepareFor(ctx, os.Geteuid() == 0 && filepath.Clean(sourceRoot) == "/")
	if f == nil {
		return nil
	}
	return func(ds inventory.DataSet) (func(), error) { return f(ds.Service) }
}
