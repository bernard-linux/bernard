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
	"time"

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
	OpSettings     = "settings"     // réglages du bureau et tâches planifiées d'un compte
	OpAddPrinter   = "addPrinter"   // réinstaller une imprimante réseau
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
	Selected   bool   `json:"selected"`
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

	// 2. Applications.
	var installs []Action
	needFlatpak := false
	for _, app := range inv.Apps {
		a := appAction(app, t)
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

	// 3. Données.
	for _, d := range inv.DataSets {
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
	}

	// 5. Réseau et imprimantes.
	for _, w := range inv.Network.Wifi {
		add(Action{Op: OpImportWifi, Label: w, Fidelity: FidelityFull, Selected: true})
	}
	for _, pr := range inv.Network.Printers {
		add(Action{Op: OpAddPrinter, Label: pr, Fidelity: FidelityFull, Selected: true})
	}

	for i := range p.Actions {
		p.Actions[i].ID = "p" + strconv.Itoa(i+1)
	}
	p.checkSpace()
	return p, nil
}

// appAction applique les règles de la section « Décisions prises » :
// déjà présent → gardé ; logiciel de base → dépôt de la distribution ;
// le reste → Flatpak ; Snap jamais installé, converti en Flatpak.
func appAction(app inventory.App, t Target) Action {
	a := Action{From: app.ID, Label: app.Name, Selected: true}
	switch app.Origin {
	case inventory.OriginApt:
		switch {
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
		if a.Op == OpCopy && a.Selected {
			p.Totals.Files += a.Files
			p.Totals.Bytes += a.Bytes
		}
	}
	p.checkSpace()
}

// checkSpace bloque le plan si les données sélectionnées ne tiennent pas.
func (p *Plan) checkSpace() {
	var need int64
	for _, a := range p.Actions {
		if a.Op == OpCopy && a.Selected {
			need += a.Bytes
		}
	}
	limit := int64(float64(p.Target.FreeBytes) * (1 - SpaceMargin))
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
