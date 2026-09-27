// Commande bernard : le moteur, sur la nouvelle machine (la cible).
//
// V1.0, étape 3 : réception réseau ou depuis un disque externe, journalisée
// et reprenable, et annulation. En mode développement, les données arrivent
// dans --dest/<login> ; l'installation dans /home avec création des comptes
// arrive à l'étape 4.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bernard-linux/bernard/internal/apply"
	"github.com/bernard-linux/bernard/internal/discovery"
	"github.com/bernard-linux/bernard/internal/engine"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/journal"
	"github.com/bernard-linux/bernard/internal/link"
	"github.com/bernard-linux/bernard/internal/migrate"
	"github.com/bernard-linux/bernard/internal/pack"
	"github.com/bernard-linux/bernard/internal/plan"
	"github.com/bernard-linux/bernard/internal/remote"
	"github.com/bernard-linux/bernard/internal/session"
	"github.com/bernard-linux/bernard/internal/settings"
	"github.com/bernard-linux/bernard/internal/source"
	"github.com/bernard-linux/bernard/internal/sysexec"
	"github.com/bernard-linux/bernard/internal/system"
	"github.com/bernard-linux/bernard/internal/transfer"
	"github.com/bernard-linux/bernard/internal/ui"
	"github.com/bernard-linux/bernard/internal/version"
)

const usage = `bernard — moteur de Bernard, l'assistant de migration vers Linux

Usage :
  bernard gui
      Ouvre l'assistant graphique (demande le mot de passe administrateur).
  sudo bernard receive --system [--yes] [--port 51516]
      Migration réelle : attend l'ancien ordinateur, affiche le code,
      montre le plan, crée les comptes, installe les applications et copie
      les données dans /home. Survit aux changements de liaison ; relancer
      la commande reprend une migration interrompue.
  bernard receive --dest DOSSIER [--port 51516]
      Mode essai : copie seulement les données dans DOSSIER/<compte>.
  bernard unpack --pack DOSSIER_DU_PAQUET --dest DOSSIER
      Lit un paquet écrit sur disque externe par bernard-agent pack.
  [sudo] bernard undo --journal FICHIER
      Supprime uniquement ce que Bernard a créé et qui n'a pas été modifié ;
      en administrateur, retire aussi les comptes et applications ajoutés.
  bernard plan -i inventaire.json [-o plan.json]
      Calcule ce qui serait fait sur CETTE machine. Ne modifie rien.
  bernard copy SOURCE DESTINATION
      Copie vérifiée d'un dossier local (outil de test).
  sudo bernard remove-account IDENTIFIANT
      Supprime un compte provisoire et son dossier personnel (utilisé au
      démarrage quand la suppression a été programmée en fin de migration).
  bernard version
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var code int
	switch os.Args[1] {
	case "gui":
		code = runGUI()
	case "ui":
		code = runUI(ctx, os.Args[2:])
	case "receive":
		code = runReceive(ctx, os.Args[2:])
	case "unpack":
		code = runUnpack(ctx, os.Args[2:])
	case "undo":
		code = runUndo(os.Args[2:])
	case "plan":
		code = runPlan(ctx, os.Args[2:])
	case "copy":
		code = runCopy(os.Args[2:])
	case "remove-account":
		code = runRemoveAccount(ctx, os.Args[2:])
	case "version":
		fmt.Println(version.Version)
	default:
		fmt.Fprint(os.Stderr, usage)
		code = 2
	}
	os.Exit(code)
}

func journalPath(dest string) string { return filepath.Join(dest, ".bernard", "journal.jsonl") }

func progressPrinter() func(engine.Progress) {
	last := int64(-1)
	return func(p engine.Progress) {
		if pct := p.Files * 100 / max(p.Total, 1); pct/10 != last/10 || p.Files == p.Total {
			last = pct
			fmt.Printf("\r  %s : %d / %d éléments (%d %%), %.1f Mo reçus    ", p.Dataset, p.Files, p.Total, pct, float64(p.Bytes)/1e6)
			if p.Files == p.Total {
				fmt.Println()
			}
		}
	}
}

func printReports(reports map[string]*transfer.TreeReport) bool {
	ok := true
	for id, rep := range reports {
		ok = ok && rep.OK()
		fmt.Printf("%s : %d fichiers copiés et vérifiés (%.1f Mo), %d déjà présents\n",
			id, rep.Files, float64(rep.Bytes)/1e6, rep.AlreadyPresent)
		for _, r := range rep.Renamed {
			fmt.Println("  Renommé (un fichier du même nom existait) :", r.Dst)
		}
		for _, s := range rep.Skipped {
			fmt.Println("  Non migrable (fichier spécial) :", s)
		}
		for _, e := range rep.Errors {
			fmt.Printf("  ERREUR %s : %s\n", e.Path, e.Err)
		}
	}
	return ok
}

// runMigration copie depuis une source (mode essai) et affiche le bilan.
func runMigration(ctx context.Context, src source.Source, dest string) int {
	jp := journalPath(dest)
	sum, err := engine.RunAll(ctx, src, dest, jp, progressPrinter())
	if sum != nil {
		printReports(sum.Reports)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Migration interrompue :", err)
		fmt.Fprintln(os.Stderr, "Relancez la même commande pour reprendre ; rien de ce qui est déjà vérifié ne sera recopié.")
		return 1
	}
	fmt.Println("Journal :", jp)
	if !sum.OK() {
		fmt.Println("Terminé avec des éléments non copiés (voir ci-dessus).")
		return 1
	}
	fmt.Println("Migration terminée : tout a été copié et vérifié.")
	return 0
}

func runReceive(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("receive", flag.ExitOnError)
	dest := fs.String("dest", "", "mode essai : dossier de destination")
	sysMode := fs.Bool("system", false, "migration réelle (comptes, applications, /home) ; nécessite sudo")
	yes := fs.Bool("yes", false, "ne pas demander de confirmation du plan")
	port := fs.Int("port", 51516, "port TCP d'appairage")
	fs.Parse(args)
	if (*dest == "") == !*sysMode || (*dest != "" && *sysMode) {
		fmt.Fprintln(os.Stderr, "Choisissez --system (migration réelle) ou --dest DOSSIER (essai).")
		return 2
	}
	if *sysMode && os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "La migration réelle crée des comptes et installe des applications : lancez-la avec sudo.")
		return 1
	}

	lc := net.ListenConfig{KeepAlive: session.KeepAlive}
	ln, err := lc.Listen(ctx, "tcp", ":"+strconv.Itoa(*port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Écoute impossible :", err)
		return 1
	}
	defer ln.Close()
	go func() { <-ctx.Done(); ln.Close() }()
	host, _ := os.Hostname()
	pairer, err := session.NewPairer(host)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cfg, err := session.ServerConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	idb := make([]byte, 8)
	rand.Read(idb)
	// Les balises restent actives toute la session : l'agent s'en sert pour
	// retrouver la cible par une autre liaison après une coupure.
	go discovery.Announce(ctx, discovery.Beacon{ID: hex.EncodeToString(idb), Name: host, Port: *port})

	showCode := func() {
		fmt.Printf("\nSur l'ancien ordinateur, lancez : sudo bernard-agent connect\nCode d'appairage : %s\n", spaced(pairer.Code()))
		for _, itf := range discovery.Interfaces() {
			fmt.Printf("  (si la recherche échoue : --target <adresse de %s>:%d)\n", itf.Name, *port)
		}
	}
	showCode()
	accept := link.NewAccepter(ln, pairer, cfg, nil, func(err error) {
		fmt.Println("Connexion refusée :", err)
		if renewed, _ := pairer.RenewIfNeeded(); renewed {
			showCode()
		}
	})
	first, err := accept(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Arrêt :", err)
		return 1
	}
	fmt.Printf("Appairé avec %s.\n", first.PeerName)

	var run func(context.Context, *remote.Client) error
	if *sysMode {
		run = systemRun(*yes)
	} else {
		run = func(ctx context.Context, cli *remote.Client) error {
			sum, err := engine.RunAll(ctx, cli, *dest, journalPath(*dest), progressPrinter())
			if sum != nil && err == nil {
				printReports(sum.Reports)
				fmt.Println("Journal :", journalPath(*dest))
			}
			return err
		}
	}
	err = link.ReceiveWithReconnect(ctx, first, accept, run, func(msg string) { fmt.Println("\n" + msg) })
	if err != nil {
		fmt.Fprintln(os.Stderr, "Migration interrompue :", err)
		fmt.Fprintln(os.Stderr, "Relancez les deux commandes pour reprendre : rien de ce qui est vérifié ne sera refait.")
		return 1
	}
	fmt.Println("Migration terminée.")
	return 0
}

// systemRun renvoie la fonction de migration réelle en ligne de commande.
// Elle peut être rappelée après une reconnexion : le plan n'est confirmé
// qu'une fois, les choix sont réappliqués et le journal fait sauter ce qui
// est déjà fait. Même déroulé que l'interface graphique (package migrate).
func systemRun(yes bool) func(context.Context, *remote.Client) error {
	confirmed := yes
	var choices *migrate.Choices
	in := bufio.NewReader(os.Stdin)
	return func(ctx context.Context, cli *remote.Client) error {
		sess, err := migrate.Prepare(ctx, cli)
		if err != nil {
			return err
		}
		secrets := migrate.Secrets(ctx, cli)
		if choices == nil {
			if !confirmed {
				printPlan(sess.Plan, sess.Inv, sess.Warnings)
				if sess.Plan.Blocked {
					return errors.New("espace disque insuffisant")
				}
				fmt.Print("Lancer la migration ? [o/N] ")
				ans, _ := in.ReadString('\n')
				if a := strings.ToLower(strings.TrimSpace(ans)); a != "o" && a != "oui" && a != "y" {
					return errors.New("migration annulée avant toute modification")
				}
				confirmed = true
			}
			choices = &migrate.Choices{Passwords: map[string]string{}}
			for _, login := range migrate.MissingPasswords(sess, secrets) {
				for {
					fmt.Printf("Nouveau mot de passe pour %s : ", login)
					a, _ := in.ReadString('\n')
					fmt.Printf("Confirmez : ")
					b, _ := in.ReadString('\n')
					a, b = strings.TrimRight(a, "\r\n"), strings.TrimRight(b, "\r\n")
					if a != "" && a == b {
						choices.Passwords[login] = a
						break
					}
					fmt.Println("Les deux saisies diffèrent ou sont vides.")
				}
			}
		}
		choices.ApplyTo(sess.Plan)
		res, err := migrate.Execute(ctx, cli, sess, *choices, secrets, migrate.Hooks{
			Log:      func(m string) { fmt.Println(m) },
			Progress: progressPrinter(),
		})
		if res != nil {
			if res.System != nil {
				for name, why := range res.System.Failed {
					fmt.Printf("  Non installé : %s (%s)\n", name, why)
				}
			}
			if res.Data != nil {
				printReports(res.Data)
			}
			if res.Settings != nil {
				for _, a := range res.Settings.Applied {
					fmt.Println("  Repris :", a)
				}
				for k, why := range res.Settings.Skipped {
					fmt.Printf("  Non repris : %s (%s)\n", k, why)
				}
				for k, why := range res.Settings.Failed {
					fmt.Printf("  ÉCHEC : %s (%s)\n", k, why)
				}
			}
		}
		if err != nil {
			return err
		}
		fmt.Println("Journal (pour annuler : sudo bernard undo --journal) :", res.JournalPath)
		return nil
	}
}

func printPlan(p *plan.Plan, inv *inventory.Inventory, warnings []string) {
	fmt.Println("\n=== Plan de migration ===")
	for _, a := range p.Actions {
		if !a.Selected {
			continue
		}
		switch a.Op {
		case plan.OpCreateUser:
			how := "mot de passe repris"
			if a.Password == "ask" {
				how = "nouveau mot de passe demandé"
			}
			fmt.Printf("  Créer le compte %s (%s)\n", a.Login, how)
		case plan.OpUseUser:
			fmt.Printf("  Utiliser le compte existant %s\n", a.Login)
		case plan.OpCopy:
			fmt.Printf("  Copier %s → /home/%s (%.1f Go, %d fichiers)\n", a.Label, a.Login, float64(a.Bytes)/1e9, a.Files)
		case plan.OpRemove:
			fmt.Printf("  Retirer %s (absente de l'ancien ordinateur)\n", a.Label)
		case plan.OpKeyboard:
			fmt.Printf("  Reprendre la disposition du clavier de l'ancien ordinateur pour %s\n", a.Login)
		}
	}
	var apt, fp, review int
	for _, a := range p.Actions {
		switch {
		case a.Selected && a.Op == plan.OpInstall && a.Via == "apt":
			apt++
		case a.Selected && a.Op == plan.OpInstall && a.Via == "flatpak":
			fp++
		case a.Op == plan.OpReview:
			review++
		}
	}
	fmt.Printf("  Installer %d applications depuis les dépôts et %d depuis Flathub\n", apt, fp)
	if review > 0 {
		fmt.Printf("  %d éléments sans équivalent automatique (listés dans le plan)\n", review)
	}
	for _, w := range warnings {
		fmt.Println("  Attention :", w)
	}
	fmt.Printf("  Espace : %.1f Go à copier, %.1f Go libres\n", float64(p.Totals.Bytes)/1e9, float64(p.Target.FreeBytes)/1e9)
}

func spaced(code string) string {
	if len(code) == 6 {
		return code[:3] + " " + code[3:]
	}
	return code
}

func runUnpack(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("unpack", flag.ExitOnError)
	dir := fs.String("pack", "", "dossier du paquet sur le disque externe")
	dest := fs.String("dest", "", "dossier de destination")
	fs.Parse(args)
	if *dir == "" || *dest == "" {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	pass := os.Getenv("BERNARD_PASSPHRASE")
	if pass == "" {
		fmt.Print("Phrase de passe du paquet : ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		pass = strings.TrimSpace(line)
	}
	p, err := pack.Open(*dir, pass)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Paquet illisible :", err)
		return 1
	}
	return runMigration(ctx, p, *dest)
}

func runUndo(args []string) int {
	fs := flag.NewFlagSet("undo", flag.ExitOnError)
	jp := fs.String("journal", "", "journal de la migration à annuler")
	fs.Parse(args)
	if *jp == "" {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	st, err := journal.Load(*jp)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Journal illisible :", err)
		return 1
	}
	if len(st.Sys) > 0 && os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "Cette migration a créé des comptes ou installé des applications : lancez l'annulation avec sudo.")
		return 1
	}
	rep, err := engine.Undo(*jp)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Annulation impossible :", err)
		return 1
	}
	fmt.Printf("Supprimés : %d fichiers et liens créés par Bernard, %d dossiers vides, %d fichiers temporaires.\n",
		rep.Removed, rep.DirsRemoved, rep.Parts)
	for _, k := range rep.Kept {
		fmt.Println("Conservé (modifié depuis la copie) :", k)
	}
	if len(st.Sys) > 0 {
		u := apply.UndoSystem(context.Background(), st, system.New(), settings.New(filepath.Dir(*jp)))
		for _, n := range u.Removed {
			fmt.Println("Application retirée :", n)
		}
		for _, n := range u.UsersDeleted {
			fmt.Printf("Compte supprimé : %s (son dossier personnel est conservé s'il contient encore des fichiers)\n", n)
		}
		for _, e := range u.Errors {
			fmt.Println("Non annulé :", e)
		}
	}
	return 0
}

func runPlan(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	in := fs.String("i", "inventaire.json", "inventaire produit par bernard-agent")
	out := fs.String("o", "plan.json", "fichier de plan à écrire")
	fs.Parse(args)

	inv, err := inventory.Load(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Inventaire refusé :", err)
		return 1
	}
	target, warnings := plan.DetectTarget(ctx, sysexec.Exec)
	p, err := plan.Build(inv, target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Plan impossible :", err)
		return 1
	}
	if err := p.Save(*out); err != nil {
		fmt.Fprintln(os.Stderr, "Écriture impossible :", err)
		return 1
	}
	labels := map[string]string{
		plan.OpCreateUser: "comptes à créer", plan.OpUseUser: "comptes existants réutilisés",
		plan.OpSetupFlatpak: "installation de Flatpak", plan.OpInstall: "applications à installer",
		plan.OpCopy: "dossiers à copier", plan.OpImportWifi: "réseaux Wi-Fi à importer",
		plan.OpSkip: "éléments déjà présents ou inutiles", plan.OpReview: "actions manuelles proposées",
		plan.OpSettings: "réglages de comptes", plan.OpAddPrinter: "imprimantes",
		plan.OpRemove:   "applications absentes de l'ancien ordinateur (retrait proposé)",
		plan.OpKeyboard: "options de clavier (non cochées)",
	}
	sum := p.Summary()
	fmt.Printf("Cible : %s %s — %.1f Go libres\n", target.Distro, target.Version, float64(target.FreeBytes)/1e9)
	for _, op := range p.SortedOps() {
		fmt.Printf("  %4d  %s\n", sum[op], labels[op])
	}
	fmt.Printf("Volume à transférer : %.1f Go (%d fichiers)\n", float64(p.Totals.Bytes)/1e9, p.Totals.Files)
	for _, w := range warnings {
		fmt.Println("Attention :", w)
	}
	if p.Blocked {
		fmt.Println("BLOQUÉ : espace disque insuffisant sur la cible.")
	}
	fmt.Println("Plan écrit dans", *out, "— rien n'a été modifié.")
	return 0
}

func runCopy(args []string) int {
	if len(args) != 2 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	rep, err := transfer.CopyTree(args[0], args[1], transfer.TreeOptions{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Copie impossible :", err)
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(rep)
	if !rep.OK() {
		return 1
	}
	return 0
}

// runUI lance le moteur et son interface locale (en administrateur). Il
// écrit « URL <adresse> » sur la sortie standard. Avec --stdio, il s'arrête
// quand son entrée standard se ferme (fenêtre fermée par l'utilisateur).
func runUI(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ui", flag.ExitOnError)
	stdio := fs.Bool("stdio", false, "s'arrêter à la fermeture de l'entrée standard")
	fs.Parse(args)
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "Le moteur doit être lancé en administrateur : utilisez « bernard gui ».")
		return 1
	}
	ctrl := ui.NewController()
	srv, err := ui.NewServer(ctrl)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	go srv.Serve()
	fmt.Println("URL " + srv.URL())
	os.Stdout.Sync()

	closed := make(chan struct{})
	if *stdio {
		go func() {
			io.Copy(io.Discard, os.Stdin)
			close(closed)
		}()
	}
	select {
	case <-ctx.Done():
	case <-ctrl.Quit():
	case <-closed:
	}
	ctrl.Stop()
	srv.Shutdown()
	return 0
}

// runRemoveAccount supprime un compte provisoire. Lancée au démarrage par
// l'unité systemd que programme l'écran de fin de migration.
func runRemoveAccount(ctx context.Context, args []string) int {
	if len(args) != 1 || os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "usage : sudo bernard remove-account IDENTIFIANT")
		return 2
	}
	if err := system.New().RemoveAccountNow(ctx, args[0]); err != nil {
		fmt.Fprintln(os.Stderr, "Compte", args[0], "non supprimé :", err)
		return 1
	}
	fmt.Println("Compte", args[0], "supprimé, avec son dossier personnel.")
	return 0
}

// runGUI est le point d'entrée du menu des applications. Il tourne en
// utilisateur normal : il lance le moteur via pkexec, puis ouvre la fenêtre
// avec les droits de l'utilisateur (jamais en administrateur).
func runGUI() int {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var engineCmd *exec.Cmd
	if os.Geteuid() == 0 {
		fmt.Fprintln(os.Stderr, "Attention : lancez plutôt « bernard gui » sans sudo ; la fenêtre ne devrait pas tourner en administrateur.")
		engineCmd = exec.Command(self, "ui", "--stdio")
	} else {
		engineCmd = exec.Command("pkexec", self, "ui", "--stdio")
	}
	stdin, _ := engineCmd.StdinPipe()
	stdout, _ := engineCmd.StdoutPipe()
	engineCmd.Stderr = os.Stderr
	if err := engineCmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "Impossible de lancer le moteur :", err)
		return 1
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "URL ") {
		fmt.Fprintln(os.Stderr, "Le moteur n'a pas démarré (mot de passe administrateur refusé ?)")
		engineCmd.Wait()
		return 1
	}
	url := strings.TrimSpace(strings.TrimPrefix(line, "URL "))

	window := ""
	if p := filepath.Join(filepath.Dir(self), "bernard-window"); fileExists(p) {
		window = p
	} else if p, err := exec.LookPath("bernard-window"); err == nil {
		window = p
	}
	if window != "" {
		exec.Command(window, url).Run() // bloque jusqu'à la fermeture de la fenêtre
		stdin.Close()
	} else {
		fmt.Println("Fenêtre dédiée absente : ouverture dans le navigateur.")
		exec.Command("xdg-open", url).Start()
		fmt.Println("Fermez Bernard depuis la fenêtre, ou ici avec Ctrl+C.")
	}
	engineCmd.Wait()
	return 0
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}
