// Package plan transforme un inventaire (ce qui existe sur la source) en plan
// (ce qui va être fait sur la cible). Build est une fonction pure : elle ne
// lit rien et ne modifie rien, ce qui la rend entièrement testable.
package plan

import (
	"encoding/json"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bernard-linux/bernard/internal/aptrepo"
	"github.com/bernard-linux/bernard/internal/hardware"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/settings"
)

// Schema identifie la version du format de plan.
const Schema = "migration-plan/1"

// Opérations.
const (
	OpCreateUser   = "createUser"   // créer un compte
	OpUseUser      = "useUser"      // le compte existe déjà : on y migre
	OpSetupFlatpak = "setupFlatpak" // installer Flatpak et ajouter Flathub
	OpInstall      = "install"      // installer une application
	OpCopy         = "copy"         // copier un jeu de données
	OpImportWifi   = "importWifi"   // importer une connexion Wi-Fi
	OpImportVPN    = "importVPN"    // importer une connexion VPN
	OpSettings     = "settings"     // réglages du bureau et tâches planifiées d'un compte
	OpAddPrinter   = "addPrinter"   // réinstaller une imprimante réseau
	OpRemove       = "remove"       // retirer une application absente de l'ancien ordinateur
	OpKeyboard     = "keyboard"     // reprendre la disposition du clavier de l'ancien ordinateur
	OpAutoLoginOff = "autoLoginOff" // ne plus ouvrir seule la session d'un compte non migré
	OpSystemData   = "systemData"   // données hors des dossiers personnels
	OpAddRepo      = "addRepo"      // ajouter un dépôt de logiciels de l'ancien ordinateur
	OpAttachDisk   = "attachDisk"   // rattacher tel quel un disque déplacé dans ce PC
	OpSkip         = "skip"         // rien à faire (déjà présent, technique…)
	OpReview       = "review"       // action manuelle proposée à l'utilisateur
)

// Niveaux de fidélité, repris dans l'interface et le rapport.
const (
	FidelityFull       = "full"       // identique
	FidelitySubstitute = "substitute" // équivalent sous un autre format ou nom
	FidelityNone       = "none"       // non migrable automatiquement
)

// Raisons (codes stables, traduits par l'interface).
const (
	ReasonAlreadyInstalled = "alreadyInstalled"
	ReasonNotInRepos       = "notInTargetRepos"
	ReasonNoEquivalent     = "noEquivalent"
	ReasonSnapInfra        = "snapInfrastructure"
	ReasonNotEnoughSpace   = "notEnoughSpace"
	ReasonPrinterSetup     = "printerNeedsSetup"
	ReasonDesktopMismatch  = "desktopMismatch" // bureaux différents : seules les tâches planifiées suivent
	ReasonHardware         = "hardware"        // pilote, micrologiciel, noyau : propre à chaque machine
	// Raisons d'un retrait proposé.
	ReasonRemovedOnSource = "removedOnSource" // retiré de l'ancien ordinateur (journal de dpkg)
	ReasonAbsentOnSource  = "absentOnSource"  // absent de l'ancien ordinateur, même version du système
	ReasonAbsentOlder     = "absentOlder"     // absent de l'ancien, dont le système est d'une autre version
)

// Action est une opération du plan. Selected permet à l'interface de cocher
// ou décocher ; le moteur n'exécute que les actions sélectionnées.
type Action struct {
	ID         string `json:"id"`
	Op         string `json:"op"`
	From       string `json:"from,omitempty"` // identifiant dans l'inventaire
	Label      string `json:"label"`
	Via        string `json:"via,omitempty"` // apt, flatpak
	Package    string `json:"package,omitempty"`
	Login      string `json:"login,omitempty"`
	Password   string `json:"password,omitempty"` // copyHash | ask
	To         string `json:"to,omitempty"`
	Files      int64  `json:"files,omitempty"`
	Bytes      int64  `json:"bytes,omitempty"`
	Fidelity   string `json:"fidelity,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
	// Also : autres paquets que retirerait un retrait (OpRemove), ou
	// fichiers concernés (OpSystemData, liste courte).
	Also []string `json:"also,omitempty"`
	// Note : précision affichée (service arrêté pendant la copie, raison
	// d'une copie impossible…).
	Note string `json:"note,omitempty"`
	// Used : place réellement occupée (OpSystemData), plus petite que Bytes
	// pour les fichiers creux.
	Used int64 `json:"used,omitempty"`
	// Date : date du retrait sur l'ancien ordinateur (OpRemove), ou
	// disposition du clavier de l'ancien ordinateur (OpKeyboard).
	Date     string `json:"date,omitempty"`
	Selected bool   `json:"selected"`
}

// Target décrit l'état de la machine cible, relevé par DetectTarget.
type Target struct {
	Distro           string            `json:"distro"`
	Version          string            `json:"version"`
	Desktop          string            `json:"desktop"`
	HomeRoot         string            `json:"homeRoot"` // en général /home
	FreeBytes        int64             `json:"freeBytes"`
	ExistingUsers    map[string]bool   `json:"-"`
	AptInstalled     map[string]bool   `json:"-"`
	AptAvailable     func(string) bool `json:"-"`
	FlatpakReady     bool              `json:"flatpakReady"`
	FlatpakInstalled map[string]bool   `json:"-"`
	SnapInstalled    map[string]bool   `json:"-"`
	Keyboard         string            `json:"keyboard,omitempty"`
	GPUs             []string          `json:"gpus,omitempty"`
	// Codename : nom de code Ubuntu/Debian de la cible (« noble »).
	Codename string `json:"codename,omitempty"`
	// UUIDs : systèmes de fichiers présents sur la cible (disque déplacé).
	UUIDs map[string]bool `json:"-"`
	// DataMounts : disques de données de la cible, avec leur place libre.
	DataMounts []Mount `json:"dataMounts,omitempty"`
	// KnownRepos : adresses des dépôts déjà configurés sur la cible.
	KnownRepos map[string]bool `json:"-"`
	// AutoLoginUser : compte dont la session s'ouvre seule au démarrage.
	AutoLoginUser string `json:"autoLoginUser,omitempty"`
	// Pour proposer le retrait des applications absentes de la source.
	AptManual map[string]bool `json:"-"`
	// AptApps : paquet → nom de l'application qu'il affiche dans le menu.
	AptApps map[string]string `json:"-"`
	// AptProtected : paquets indispensables au système ou au bureau.
	AptProtected map[string]bool `json:"-"`
	// AptSize : place occupée par paquet, en octets.
	AptSize map[string]int64 `json:"-"`
	// RemovalImpact renvoie tout ce qu'apt retirerait avec ce paquet (lui
	// compris), sans rien faire (simulation).
	RemovalImpact func(pkg string) ([]string, error) `json:"-"`
	// FlatpakNames : identifiant → nom, pour les applications Flatpak de la cible.
	FlatpakNames map[string]string `json:"-"`
}

// Mount est un disque de données de la cible.
type Mount struct {
	Point string `json:"point"`
	Free  int64  `json:"free"`
}

// Totals résume le volume à transférer.
type Totals struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

// Plan est le document validé par l'utilisateur avant toute action.
type Plan struct {
	Schema    string    `json:"schema"`
	CreatedAt time.Time `json:"createdAt"`
	Inventory string    `json:"inventory"` // empreinte de l'inventaire source
	Target    Target    `json:"target"`
	Actions   []Action  `json:"actions"`
	Totals    Totals    `json:"totals"`
	// Trimmed : données hors dossiers personnels décochées faute de place.
	Trimmed []string `json:"trimmed,omitempty"`
	// Freed : place libérée par les retraits sélectionnés.
	Freed int64 `json:"freed,omitempty"`
	// Blocked empêche le démarrage (espace insuffisant…) ; Reason explique.
	Blocked bool   `json:"blocked"`
	Reason  string `json:"reason,omitempty"`
}

// SpaceMargin est la part d'espace libre qu'on s'interdit de remplir.
const SpaceMargin = 0.05

// Build construit le plan. inv doit avoir été validé.
func Build(inv *inventory.Inventory, t Target) (*Plan, error) {
	digest, err := inv.Digest()
	if err != nil {
		return nil, err
	}
	p := &Plan{Schema: Schema, CreatedAt: time.Now().UTC().Truncate(time.Second), Inventory: digest, Target: t}
	add := func(a Action) { p.Actions = append(p.Actions, a) }

	// 1. Comptes.
	logins := map[string]string{}
	for _, u := range inv.Users {
		logins[u.ID] = u.Login
		if t.ExistingUsers[u.Login] {
			add(Action{Op: OpUseUser, From: u.ID, Login: u.Login, Label: u.Login, Selected: true})
			continue
		}
		pw := "ask"
		if inv.Source.OS == "linux" {
			pw = "copyHash" // hachages /etc/shadow compatibles entre Linux
		}
		add(Action{Op: OpCreateUser, From: u.ID, Login: u.Login, Label: u.Login, Password: pw, Selected: true})
	}

	// 2. Dépôts de logiciels ajoutés sur l'ancien ordinateur et inconnus ici.
	newRepos := map[string]string{} // adresse → hôte
	if inv.Source.OS == "linux" {
		for _, src := range inv.AptSources {
			missing := false
			for _, u := range src.URIs {
				if !t.KnownRepos[u] {
					missing = true
				}
			}
			if !missing {
				continue
			}
			host := aptrepo.Host(src.URIs[0])
			add(Action{Op: OpAddRepo, Label: host, Package: src.File, Fidelity: FidelityFull, Selected: true})
			for _, u := range src.URIs {
				newRepos[u] = host
			}
		}
	}

	// 2 bis. Applications.
	var installs []Action
	needFlatpak := false
	for _, app := range inv.Apps {
		a := appAction(app, t)
		// Introuvable ici, mais son dépôt sera ajouté : installable.
		if a.Op == OpReview && a.Reason == ReasonNotInRepos && newRepos[app.Repo] != "" {
			a.Op, a.Via, a.Package, a.Fidelity, a.Selected, a.Reason = OpInstall, "apt", app.Name, FidelityFull, true, ""
			a.Suggestion = "dépôt " + newRepos[app.Repo]
		}
		if a.Op == OpInstall && a.Via == "flatpak" {
			needFlatpak = true
		}
		installs = append(installs, a)
	}
	if needFlatpak && !t.FlatpakReady {
		add(Action{Op: OpSetupFlatpak, Label: "Flatpak et Flathub", Fidelity: FidelityFull, Selected: true})
	}
	for _, a := range installs {
		add(a)
	}
	// 2 bis. Applications que l'utilisateur avait retirées de l'ancien
	// ordinateur mais que l'installation du nouveau a remises.
	for _, a := range removals(inv, t) {
		add(a)
	}

	// 3. Données.
	sysSets := map[string]inventory.DataSet{}
	for _, d := range inv.DataSets {
		if d.Kind == "system" {
			sysSets[d.System] = d
			continue
		}
		login := logins[d.User]
		add(Action{
			Op: OpCopy, From: d.ID, Label: d.Path, Login: login,
			To: path.Join(t.HomeRoot, login), Files: d.Files, Bytes: d.SizeBytes,
			Fidelity: FidelityFull, Selected: true,
		})
		p.Totals.Files += d.Files
		p.Totals.Bytes += d.SizeBytes
	}

	// 4. Réglages, après les données (le fond d'écran doit être arrivé).
	fid := settings.Fidelity(inv.Source.Desktop, t.Desktop)
	for _, u := range inv.Users {
		a := Action{Op: OpSettings, From: u.ID, Login: u.Login, Label: u.Login, Fidelity: fid, Selected: fid != FidelityNone}
		if fid == FidelityNone {
			// Les tâches planifiées restent transposables.
			a.Reason = ReasonDesktopMismatch
			a.Selected = true
		}
		add(a)
		// Clavier : chaque ordinateur garde la disposition choisie à son
		// installation (clavier physique différent). Reprendre celle de
		// l'ancien reste possible, dans les options avancées.
		if fid != FidelityNone && inv.Source.Keyboard != t.Keyboard {
			add(Action{Op: OpKeyboard, From: u.ID, Login: u.Login, Label: u.Login,
				Date: inv.Source.Keyboard, Fidelity: fid, Selected: false})
		}
	}

	// 4 bis. Session ouverte automatiquement sur un compte qui n'est pas
	// migré (compte provisoire créé à l'installation) : après la migration,
	// l'écran de connexion doit laisser choisir le compte migré.
	if u := t.AutoLoginUser; u != "" {
		migrated := false
		for _, iu := range inv.Users {
			migrated = migrated || iu.Login == u
		}
		if !migrated {
			add(Action{Op: OpAutoLoginOff, Login: u, Label: u, Fidelity: FidelityFull, Selected: true})
		}
	}

	// 4 ter. Données hors des dossiers personnels : détectées et montrées ;
	// leur copie arrive avec la version 0.5.
	for _, it := range inv.System {
		a := Action{Op: OpSystemData, From: it.ID, Label: it.Label, Files: it.Files, Bytes: it.Bytes, Used: it.Used,
			Reason: it.Kind, Suggestion: it.Advice, Also: it.Detail, Fidelity: FidelityNone}
		if diskKinds[it.Kind] {
			add(diskAction(it, sysSets[it.ID], inv, t))
			continue
		}
		if d, ok := sysSets[it.ID]; ok {
			// Copié à l'identique, au même endroit ; coché quand Bernard le
			// conseille, à examiner sinon.
			a.Fidelity, a.To, a.Package = FidelityFull, d.Dest, d.ID
			a.Selected = it.Advice == inventory.AdviceCopy
			a.Note = serviceNote(it, inv.Source, t)
			if it.Kind == inventory.SysDatabase && !sameRelease(inv.Source, t) {
				// Fichiers de base d'une autre version du système : le
				// serveur de la cible ne saurait pas forcément les lire.
				a.Fidelity, a.Selected, a.Package = FidelityNone, false, ""
			}
		}
		add(a)
	}

	// 5. Réseau et imprimantes.
	for _, w := range inv.Network.Wifi {
		add(Action{Op: OpImportWifi, Label: w, Fidelity: FidelityFull, Selected: true})
	}
	for _, v := range inv.Network.VPN {
		add(Action{Op: OpImportVPN, Label: v, Fidelity: FidelityFull, Selected: true})
	}
	for _, pr := range inv.Network.Printers {
		add(Action{Op: OpAddPrinter, Label: pr, Fidelity: FidelityFull, Selected: true})
	}

	for i := range p.Actions {
		p.Actions[i].ID = "p" + strconv.Itoa(i+1)
	}
	p.Recheck()
	p.fitSystemData()
	return p, nil
}

// fitSystemData : si tout ne tient pas sur la cible mais que les dossiers
// personnels tiennent, les données hors dossiers personnels sont décochées,
// des plus grosses aux plus petites, jusqu'à ce que le reste tienne. Jamais
// bloquant pour les dossiers personnels ; l'interface prévient et renvoie
// vers le détail (Trimmed).
func (p *Plan) fitSystemData() {
	if !p.Blocked {
		return
	}
	var idx []int
	for i, a := range p.Actions {
		if a.Op == OpSystemData && a.Selected {
			idx = append(idx, i)
		}
	}
	sort.Slice(idx, func(x, y int) bool { return p.Actions[idx[x]].Used > p.Actions[idx[y]].Used })
	for _, i := range idx {
		p.Actions[i].Selected = false
		p.Trimmed = append(p.Trimmed, p.Actions[i].Label)
		p.Recheck()
		if !p.Blocked {
			return
		}
	}
}

var diskKinds = map[string]bool{
	inventory.SysDisk: true, inventory.SysHomeElse: true, inventory.SysSteam: true, inventory.SysBackup: true,
}

// onDataMount : la destination est sur un autre disque de la cible (sa
// place ne se compte pas sur celle du disque système).
func onDataMount(dest string, mounts []Mount) bool {
	for _, m := range mounts {
		if dest == m.Point || strings.HasPrefix(dest, m.Point+"/") {
			return true
		}
	}
	return false
}

// AttachPoint : point de montage d'un disque rattaché. Les montages
// automatiques (/media/<compte>/…) sont éphémères : le disque est monté à
// demeure dans /mnt.
func AttachPoint(src string) string {
	if strings.HasPrefix(src, "/media/") || strings.HasPrefix(src, "/run/media/") {
		return "/mnt/" + path.Base(src)
	}
	return src
}

// diskAction décide du sort d'un autre disque de l'ancien ordinateur :
// rattaché tel quel s'il a été déplacé dans ce PC, sinon copié sur le disque
// de données le plus libre de la cible, ou à défaut dans le dossier personnel
// du premier compte (dossier « Disques »).
func diskAction(it inventory.SystemItem, ds inventory.DataSet, inv *inventory.Inventory, t Target) Action {
	a := Action{Op: OpSystemData, From: it.ID, Label: it.Label, Files: it.Files, Bytes: it.Bytes, Used: it.Used,
		Reason: it.Kind, Suggestion: it.Advice, Fidelity: FidelityNone}
	if it.UUID != "" && t.UUIDs[it.UUID] {
		a.Op, a.To, a.Fidelity, a.Selected = OpAttachDisk, AttachPoint(it.Paths[0]), FidelityFull, true
		a.Note = "Ce disque est branché sur ce PC : il sera monté tel quel dans " + a.To + ", à chaque démarrage, sans rien copier."
		return a
	}
	if ds.ID == "" {
		return a
	}
	name := path.Base(it.Paths[0])
	var best Mount
	for _, m := range t.DataMounts {
		if m.Free > best.Free {
			best = m
		}
	}
	switch {
	case best.Point != "" && best.Free > it.Used:
		a.To = path.Join(best.Point, name)
	case len(inv.Users) > 0:
		a.To = path.Join(t.HomeRoot, inv.Users[0].Login, "Disques", name)
	default:
		return a
	}
	a.Fidelity, a.Package = FidelityFull, ds.ID
	a.Selected = it.Advice == inventory.AdviceCopy
	a.Note = "Copié dans " + a.To + "."
	if it.Kind == inventory.SysBackup {
		a.Note += " Copie déconseillée si les données qu'il protège sont déjà migrées."
	}
	return a
}

// sameRelease : même distribution et même base (nom de code) des deux côtés,
// donc mêmes versions majeures des serveurs de bases de données.
func sameRelease(src inventory.Source, t Target) bool {
	return src.Distro == t.Distro && src.Codename != "" && src.Codename == t.Codename
}

func serviceNote(it inventory.SystemItem, src inventory.Source, t Target) string {
	switch {
	case it.Kind == inventory.SysDatabase && !sameRelease(src, t):
		return "Versions du système différentes : copie directe risquée, export et import prévus dans une prochaine version."
	case it.Kind == inventory.SysVM:
		return "Éteignez les machines virtuelles avant la migration : leurs disques sont copiés tels quels, fichiers creux compris."
	case it.Service != "":
		return "Service arrêté quelques minutes sur les deux ordinateurs pendant la copie, puis relancé."
	}
	return ""
}

// appAction applique les règles de la section « Décisions prises » :
// déjà présent → gardé ; logiciel de base → dépôt de la distribution ;
// le reste → Flatpak ; Snap jamais installé, converti en Flatpak.
func appAction(app inventory.App, t Target) Action {
	a := Action{From: app.ID, Label: app.Name, Selected: true}
	switch app.Origin {
	case inventory.OriginApt:
		switch {
		case hardware.HardwarePackage(app.Name):
			a.Op, a.Reason, a.Selected = OpSkip, ReasonHardware, false
		case t.AptInstalled[app.Name]:
			a.Op, a.Reason = OpSkip, ReasonAlreadyInstalled
		case t.AptAvailable != nil && t.AptAvailable(app.Name):
			a.Op, a.Via, a.Package, a.Fidelity = OpInstall, "apt", app.Name, FidelityFull
		default:
			a.Op, a.Reason, a.Fidelity, a.Selected = OpReview, ReasonNotInRepos, FidelityNone, false
		}
	case inventory.OriginFlatpak:
		id := app.SourceID[len("flatpak:"):]
		if t.FlatpakInstalled[id] {
			a.Op, a.Reason = OpSkip, ReasonAlreadyInstalled
		} else {
			a.Op, a.Via, a.Package, a.Fidelity = OpInstall, "flatpak", id, FidelityFull
		}
	case inventory.OriginSnap:
		name := app.Name
		eq, known := SnapToFlatpak[name]
		switch {
		case known && eq == "":
			a.Op, a.Reason, a.Selected = OpSkip, ReasonSnapInfra, false
		case t.SnapInstalled[name] || t.AptInstalled[name] || (known && t.FlatpakInstalled[eq]):
			a.Op, a.Reason = OpSkip, ReasonAlreadyInstalled
		case known:
			a.Op, a.Via, a.Package, a.Fidelity = OpInstall, "flatpak", eq, FidelitySubstitute
		default:
			a.Op, a.Reason, a.Fidelity, a.Selected = OpReview, ReasonNoEquivalent, FidelityNone, false
		}
	default:
		a.Op, a.Reason, a.Fidelity, a.Selected = OpReview, ReasonNoEquivalent, FidelityNone, false
	}
	return a
}

// Recheck recalcule les totaux et le blocage après un changement de
// sélection (décocher un dossier volumineux peut débloquer le plan).
func (p *Plan) Recheck() {
	p.Totals = Totals{}
	for _, a := range p.Actions {
		if (a.Op == OpCopy || a.Op == OpSystemData) && a.Selected {
			p.Totals.Files += a.Files
			p.Totals.Bytes += a.Bytes
		}
	}
	p.checkSpace()
}

// checkSpace bloque le plan si les données sélectionnées ne tiennent pas.
func (p *Plan) checkSpace() {
	var need int64
	p.Freed = 0
	for _, a := range p.Actions {
		switch {
		case a.Op == OpCopy && a.Selected:
			need += a.Bytes
		case a.Op == OpSystemData && a.Selected && !onDataMount(a.To, p.Target.DataMounts):
			need += a.Used
		case a.Op == OpRemove && a.Selected:
			p.Freed += a.Bytes
		}
	}
	limit := int64(float64(p.Target.FreeBytes+p.Freed) * (1 - SpaceMargin))
	p.Blocked, p.Reason = false, ""
	if need > limit {
		p.Blocked, p.Reason = true, ReasonNotEnoughSpace
	}
}

// Summary compte les actions par opération, pour l'affichage.
func (p *Plan) Summary() map[string]int {
	out := map[string]int{}
	for _, a := range p.Actions {
		out[a.Op]++
	}
	return out
}

// SortedOps renvoie les opérations présentes, triées.
func (p *Plan) SortedOps() []string {
	var ops []string
	for op := range p.Summary() {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	return ops
}

// Save écrit le plan en JSON indenté.
func (p *Plan) Save(file string) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(b, '\n'), 0o600)
}
