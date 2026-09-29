// Package apply exécute un plan validé sur la cible, en administrateur :
// création des comptes, installation des applications, copie des données
// dans les dossiers personnels avec les bons propriétaires.
//
// Chaque modification système est journalisée avant de passer à la suivante,
// ce qui permet de relancer après une coupure (les étapes faites sont
// sautées) et d'annuler (UndoSystem).
package apply

import (
	"context"
	"errors"
	"fmt"

	"github.com/bernard-linux/bernard/internal/engine"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/journal"
	"github.com/bernard-linux/bernard/internal/plan"
	"github.com/bernard-linux/bernard/internal/settings"
	"github.com/bernard-linux/bernard/internal/sysexec"
	"github.com/bernard-linux/bernard/internal/system"
	"github.com/bernard-linux/bernard/internal/transfer"
)

// Applier exécute un plan.
type Applier struct {
	Sys     *system.System
	Journal *journal.Journal
	State   *journal.State
	// Secrets : hachages des mots de passe venant de la source (Linux).
	Secrets map[string]string
	// AskPassword est appelé quand un compte doit être créé sans hachage
	// repris (source Windows/macOS, ou agent sans droits administrateur).
	AskPassword func(login string) (string, error)
	Log         func(string)
}

// Report résume l'exécution des actions système.
type Report struct {
	UsersCreated []string          `json:"usersCreated,omitempty"`
	Installed    []string          `json:"installed,omitempty"`
	Removed      []string          `json:"removed,omitempty"`
	Failed       map[string]string `json:"failed,omitempty"` // étiquette → raison
}

func (a *Applier) log(format string, v ...any) {
	if a.Log != nil {
		a.Log(fmt.Sprintf(format, v...))
	}
}

func (a *Applier) did(op, name string) bool {
	for _, r := range a.State.Sys {
		if r.Op == op && r.Name == name {
			return true
		}
	}
	return false
}

func (a *Applier) record(op, name string) error {
	r := journal.Record{T: journal.RecSys, Op: op, Name: name}
	if err := a.Journal.Append(r); err != nil {
		return err
	}
	a.State.Sys = append(a.State.Sys, r)
	return nil
}

// System exécute les comptes et les applications du plan (actions
// sélectionnées uniquement). Une application qui échoue n'arrête rien : elle
// figure au rapport. Un compte qui échoue arrête tout, car ses données ne
// pourraient pas être copiées.
func (a *Applier) System(ctx context.Context, p *plan.Plan, inv *inventory.Inventory) (*Report, error) {
	rep := &Report{Failed: map[string]string{}}
	users := map[string]inventory.User{}
	for _, u := range inv.Users {
		users[u.ID] = u
	}

	for _, act := range p.Actions {
		if !act.Selected || act.Op != plan.OpCreateUser {
			continue
		}
		u := users[act.From]
		if a.did(journal.SysUserCreated, u.Login) || a.Sys.UserExists(ctx, u.Login) {
			continue // déjà créé lors d'une session précédente
		}
		spec := system.UserSpec{Login: u.Login, FullName: u.FullName, UID: u.UID, Groups: u.Groups}
		if h := a.Secrets[u.Login]; h != "" && act.Password == "copyHash" {
			spec.PasswordHash = h
		} else {
			if a.AskPassword == nil {
				return rep, fmt.Errorf("mot de passe requis pour %s", u.Login)
			}
			pw, err := a.AskPassword(u.Login)
			if err != nil {
				return rep, err
			}
			spec.Password = pw
		}
		a.log("Création du compte %s…", u.Login)
		groups, err := a.Sys.CreateUser(ctx, spec)
		if a.Sys.UserExists(ctx, u.Login) {
			// Journalisé dès qu'il existe, même si une étape suivante a
			// échoué, pour que l'annulation le retrouve.
			if rerr := a.record(journal.SysUserCreated, u.Login); rerr != nil {
				return rep, rerr
			}
		}
		if err != nil {
			return rep, fmt.Errorf("compte %s : %w", u.Login, err)
		}
		rep.UsersCreated = append(rep.UsersCreated, u.Login)
		if len(groups) > 0 {
			a.log("  groupes : %v", groups)
		}
	}

	// Retraits d'abord : ils libèrent de la place pour la suite.
	a.removeApps(ctx, p, rep)

	var aptPkgs, flatpaks []string
	needFlatpak := false
	for _, act := range p.Actions {
		if !act.Selected {
			continue
		}
		switch {
		case act.Op == plan.OpSetupFlatpak:
			needFlatpak = true
		case act.Op == plan.OpInstall && act.Via == "apt":
			aptPkgs = append(aptPkgs, act.Package)
		case act.Op == plan.OpInstall && act.Via == "flatpak":
			flatpaks = append(flatpaks, act.Package)
		}
	}

	if len(aptPkgs) > 0 || needFlatpak {
		a.log("Mise à jour de la liste des paquets…")
		if err := a.Sys.AptUpdate(ctx); err != nil {
			a.log("  attention : %v", err)
		}
	}
	if len(aptPkgs) > 0 {
		a.log("Installation de %d applications depuis les dépôts…", len(aptPkgs))
		added, failed := a.Sys.AptInstall(ctx, aptPkgs)
		for _, pkg := range added {
			if err := a.record(journal.SysAptAdded, pkg); err != nil {
				return rep, err
			}
		}
		rep.Installed = append(rep.Installed, added...)
		for pkg, err := range failed {
			rep.Failed[pkg] = err.Error()
		}
	}

	if len(flatpaks) > 0 {
		if needFlatpak {
			a.log("Mise en place de Flatpak et Flathub…")
			addedFP, addedRemote, err := a.Sys.FlatpakSetup(ctx)
			if addedFP {
				a.record(journal.SysAptAdded, "flatpak")
			}
			if addedRemote {
				a.record(journal.SysFlathubAdded, "flathub")
			}
			if err != nil {
				for _, id := range flatpaks {
					rep.Failed[id] = "Flatpak indisponible : " + err.Error()
				}
				flatpaks = nil
			}
		}
		present := a.flatpakInstalled(ctx)
		for _, id := range flatpaks {
			if present[id] || a.did(journal.SysFlatpakAdded, id) {
				continue
			}
			a.log("Installation de %s (Flathub)…", id)
			if err := a.Sys.FlatpakInstall(ctx, id); err != nil {
				rep.Failed[id] = err.Error()
				continue
			}
			if err := a.record(journal.SysFlatpakAdded, id); err != nil {
				return rep, err
			}
			rep.Installed = append(rep.Installed, id)
		}
	}
	return rep, nil
}

// removeApps retire les applications choisies (absentes de l'ancien
// ordinateur). Un échec n'arrête rien : il figure au rapport.
func (a *Applier) removeApps(ctx context.Context, p *plan.Plan, rep *Report) {
	var apt []string
	var labels = map[string]string{}
	for _, act := range p.Actions {
		if !act.Selected || act.Op != plan.OpRemove {
			continue
		}
		labels[act.Package] = act.Label
		switch act.Via {
		case "apt":
			if !a.did(journal.SysAptRemoved, act.Package) {
				apt = append(apt, act.Package)
			}
		case "flatpak":
			if a.did(journal.SysFlatpakRemoved, act.Package) {
				continue
			}
			origin := a.Sys.FlatpakOrigin(ctx, act.Package)
			a.log("Retrait de %s…", act.Label)
			if err := a.Sys.FlatpakUninstall(ctx, act.Package); err != nil {
				rep.Failed[act.Label] = "retrait : " + err.Error()
				continue
			}
			a.Journal.Append(journal.Record{T: journal.RecSys, Op: journal.SysFlatpakRemoved, Name: act.Package, Key: origin})
			a.State.Sys = append(a.State.Sys, journal.Record{Op: journal.SysFlatpakRemoved, Name: act.Package, Key: origin})
			rep.Removed = append(rep.Removed, act.Label)
		}
	}
	if len(apt) == 0 {
		return
	}
	a.log("Retrait de %d applications absentes de l'ancien ordinateur…", len(apt))
	removed, err := a.Sys.AptRemoveApps(ctx, apt)
	for _, pkg := range removed {
		a.record(journal.SysAptRemoved, pkg)
		if l, ok := labels[pkg]; ok {
			rep.Removed = append(rep.Removed, l)
		}
	}
	if err != nil {
		gone := map[string]bool{}
		for _, r := range removed {
			gone[r] = true
		}
		for _, pkg := range apt {
			if !gone[pkg] {
				rep.Failed[labels[pkg]] = "retrait : " + err.Error()
			}
		}
	}
}

func (a *Applier) flatpakInstalled(ctx context.Context) map[string]bool {
	out, err := a.Sys.Exec(ctx, sysexec.Cmd{Name: "flatpak", Args: []string{"list", "--system", "--app", "--columns=application"}})
	set := map[string]bool{}
	if err == nil {
		for _, l := range sysexec.Lines(out) {
			set[l] = true
		}
	}
	return set
}

// CopyData copie les jeux de données sélectionnés dans le dossier personnel
// de chaque compte, avec le bon propriétaire. Pour un compte créé par
// Bernard, les fichiers modèles encore intacts (.bashrc…) sont d'abord
// retirés, pour que ceux de l'ancien ordinateur les remplacent.
//
// Renvoie aussi les fichiers de profil de l'ancien ordinateur mis à la place
// de ceux du nouveau (voir PreferSource).
func CopyData(ctx context.Context, r *engine.Receiver, p *plan.Plan, inv *inventory.Inventory, created map[string]bool) (out map[string]*transfer.TreeReport, replaced []string, err error) {
	sets := map[string]inventory.DataSet{}
	for _, d := range inv.DataSets {
		sets[d.ID] = d
	}
	out = map[string]*transfer.TreeReport{}
	for _, act := range p.Actions {
		if !act.Selected || act.Op != plan.OpCopy {
			continue
		}
		uid, gid, home, err := system.Owner(act.Login)
		if err != nil {
			return out, replaced, fmt.Errorf("compte %s introuvable sur la cible : %w", act.Login, err)
		}
		if created[act.Login] {
			settings.ClearPristineSkeleton(home, "/etc/skel")
		}
		rep, err := r.CopyDataSetAs(ctx, sets[act.From], home, &transfer.Owner{UID: uid, GID: gid})
		out[act.From] = rep
		if err != nil {
			return out, replaced, err
		}
		// Compte qui existait déjà sur un système tout juste installé : ses
		// fichiers de profil sont vierges, ceux de l'ancien ordinateur
		// prennent leur place (trousseau, profils des navigateurs).
		if !created[act.Login] && Fresh() {
			st, err := journal.Load(r.Journal.Path)
			if err != nil {
				return out, replaced, err
			}
			done, err := PreferSource(home, st, r.Journal)
			for _, rel := range done {
				replaced = append(replaced, act.Login+" : "+rel)
			}
			if err != nil {
				return out, replaced, fmt.Errorf("mise en place des profils de %s : %w", act.Login, err)
			}
		}
	}
	return out, replaced, nil
}

// Fresh indique si la cible est fraîchement installée (remplacé dans les
// tests).
var Fresh = FreshInstall

// SettingsReport résume la reprise des réglages.
type SettingsReport struct {
	Applied []string          `json:"applied,omitempty"`
	Skipped map[string]string `json:"skipped,omitempty"` // non appliqué volontairement, avec la raison
	Failed  map[string]string `json:"failed,omitempty"`
}

// Settings applique réglages du bureau, tâches planifiées, Wi-Fi et
// imprimantes. Chaque élément est indépendant : un échec n'arrête rien.
func (a *Applier) Settings(ctx context.Context, p *plan.Plan, inv *inventory.Inventory, ex *settings.Extras, sa *settings.Applier) (*SettingsReport, error) {
	rep := &SettingsReport{Skipped: map[string]string{}, Failed: map[string]string{}}
	note := func(label string, err error) {
		switch {
		case err == nil:
			rep.Applied = append(rep.Applied, label)
		case errors.Is(err, settings.ErrSkipped):
			rep.Skipped[label] = err.Error()
		default:
			rep.Failed[label] = err.Error()
		}
	}
	userExists := func(login string) bool { return a.Sys.UserExists(ctx, login) }

	for _, act := range p.Actions {
		if !act.Selected {
			continue
		}
		switch act.Op {
		case plan.OpSettings:
			_, _, home, err := system.Owner(act.Login)
			if err != nil {
				note("Réglages de "+act.Login, err)
				continue
			}
			if act.Reason != plan.ReasonDesktopMismatch && !a.did(journal.SysDconfApplied, act.Login) {
				hw := settings.Hardware{KeepKeyboard: keepKeyboard(p, act.Login), TargetKeyboard: p.Target.Keyboard}
				d := settings.Translate(settings.ParseDump(ex.Dconf[act.Login]), ex.Desktop, p.Target.Desktop, settings.ThemeExists(home), hw)
				if len(d) == 0 {
					note("Réglages du bureau de "+act.Login, fmt.Errorf("%w : aucun réglage lu sur l'ancien ordinateur", settings.ErrSkipped))
				} else {
					backup, err := sa.ApplyDconf(ctx, act.Login, home, d, hw.Resets()...)
					if backup != "" {
						if rerr := a.Journal.Append(journal.Record{T: journal.RecSys, Op: journal.SysDconfApplied, Name: act.Login, Dst: backup}); rerr != nil {
							return rep, rerr
						}
						a.State.Sys = append(a.State.Sys, journal.Record{Op: journal.SysDconfApplied, Name: act.Login, Dst: backup})
					}
					note("Réglages du bureau de "+act.Login, err)
				}
			}
			if c := ex.Crontabs[act.Login]; c != "" && !a.did(journal.SysCrontabSet, act.Login) {
				err := sa.InstallCrontab(ctx, act.Login, c)
				if err == nil {
					if rerr := a.record(journal.SysCrontabSet, act.Login); rerr != nil {
						return rep, rerr
					}
				}
				note("Tâches planifiées de "+act.Login, err)
			}

		case plan.OpImportWifi:
			label := "Wi-Fi « " + act.Label + " »"
			var conn *settings.NMConnection
			for i, w := range ex.Wifi {
				if _, id, _, err := settings.SanitizeWifi(w.Content, nil); err == nil && id == act.Label {
					conn = &ex.Wifi[i]
				}
			}
			if conn == nil {
				note(label, fmt.Errorf("%w : mot de passe non lu (agent sans droits administrateur ?)", settings.ErrSkipped))
				continue
			}
			if a.didLabel(journal.SysWifiAdded, act.Label) {
				continue
			}
			path, err := sa.InstallWifi(ctx, *conn, userExists)
			if err == nil {
				if rerr := a.Journal.Append(journal.Record{T: journal.RecSys, Op: journal.SysWifiAdded, Name: act.Label, Dst: path}); rerr != nil {
					return rep, rerr
				}
				a.State.Sys = append(a.State.Sys, journal.Record{Op: journal.SysWifiAdded, Name: act.Label, Dst: path})
			}
			note(label, err)

		case plan.OpAutoLoginOff:
			label := "Ouverture automatique de la session « " + act.Label + " » désactivée"
			if a.didAny(journal.SysAutoLoginOff) {
				continue
			}
			pairs, err := sa.DisableAutoLogin("/")
			for _, pr := range pairs {
				if rerr := a.Journal.Append(journal.Record{T: journal.RecSys, Op: journal.SysAutoLoginOff, Name: pr[0], Dst: pr[1]}); rerr != nil {
					return rep, rerr
				}
				a.State.Sys = append(a.State.Sys, journal.Record{Op: journal.SysAutoLoginOff, Name: pr[0], Dst: pr[1]})
			}
			note(label, err)

		case plan.OpAddPrinter:
			label := "Imprimante " + act.Label
			var pr *settings.Printer
			for i := range ex.Printers {
				if ex.Printers[i].Name == act.Label {
					pr = &ex.Printers[i]
				}
			}
			if pr == nil {
				note(label, fmt.Errorf("%w : adresse de l'imprimante non lue", settings.ErrSkipped))
				continue
			}
			if a.did(journal.SysPrinterAdded, pr.Name) {
				continue
			}
			err := sa.AddPrinter(ctx, *pr)
			if err == nil {
				if rerr := a.record(journal.SysPrinterAdded, pr.Name); rerr != nil {
					return rep, rerr
				}
			}
			note(label, err)
		}
	}
	return rep, nil
}

// keepKeyboard : la disposition du clavier de l'ancien ordinateur n'est
// reprise que si les deux claviers sont identiques (aucune action
// « keyboard » dans le plan) ou si l'utilisateur l'a demandé.
func keepKeyboard(p *plan.Plan, login string) bool {
	for _, a := range p.Actions {
		if a.Op == plan.OpKeyboard && a.Login == login {
			return a.Selected
		}
	}
	return true
}

func (a *Applier) didAny(op string) bool {
	for _, r := range a.State.Sys {
		if r.Op == op {
			return true
		}
	}
	return false
}

func (a *Applier) didLabel(op, name string) bool { return a.did(op, name) }

// UndoReport résume l'annulation des modifications système.
type UndoReport struct {
	UsersDeleted []string `json:"usersDeleted,omitempty"`
	Removed      []string `json:"removed,omitempty"`
	Reinstalled  []string `json:"reinstalled,omitempty"`
	Errors       []string `json:"errors,omitempty"`
}

// UndoSystem défait, dans l'ordre inverse, les modifications système
// journalisées. Les comptes sont supprimés mais leur dossier personnel est
// conservé s'il contient encore des fichiers (engine.Undo retire d'abord ce
// que Bernard y a copié).
func UndoSystem(ctx context.Context, st *journal.State, sys *system.System, sa *settings.Applier) *UndoReport {
	rep := &UndoReport{}
	fail := func(what string, err error) { rep.Errors = append(rep.Errors, what+" : "+err.Error()) }
	var apt, aptBack []string
	for i := len(st.Sys) - 1; i >= 0; i-- {
		r := st.Sys[i]
		switch r.Op {
		case journal.SysAutoLoginOff:
			if err := settings.RestoreFile(r.Name, r.Dst); err != nil {
				fail("ouverture automatique de session", err)
			}
		case journal.SysAptRemoved:
			aptBack = append(aptBack, r.Name)
		case journal.SysFlatpakRemoved:
			if err := sys.FlatpakReinstall(ctx, r.Key, r.Name); err != nil {
				fail(r.Name, err)
			} else {
				rep.Reinstalled = append(rep.Reinstalled, r.Name)
			}
		case journal.SysWifiAdded:
			if err := sa.RemoveWifi(ctx, r.Dst); err != nil {
				fail("Wi-Fi "+r.Name, err)
			} else {
				rep.Removed = append(rep.Removed, "Wi-Fi "+r.Name)
			}
		case journal.SysPrinterAdded:
			if err := sa.RemovePrinter(ctx, r.Name); err != nil {
				fail("imprimante "+r.Name, err)
			} else {
				rep.Removed = append(rep.Removed, "imprimante "+r.Name)
			}
		case journal.SysCrontabSet:
			if err := sa.RemoveCrontab(ctx, r.Name); err != nil {
				fail("tâches planifiées de "+r.Name, err)
			}
		case journal.SysDconfApplied:
			if _, _, home, err := system.Owner(r.Name); err == nil {
				if err := sa.RestoreDconf(ctx, r.Name, home, r.Dst); err != nil {
					fail("réglages de "+r.Name, err)
				}
			}
		case journal.SysFlatpakAdded:
			if err := sys.FlatpakUninstall(ctx, r.Name); err != nil {
				fail(r.Name, err)
			} else {
				rep.Removed = append(rep.Removed, r.Name)
			}
		case journal.SysFlathubAdded:
			if err := sys.FlatpakRemoveRemote(ctx); err != nil {
				fail("flathub", err)
			}
		case journal.SysAptAdded:
			apt = append(apt, r.Name)
		}
	}
	if len(aptBack) > 0 {
		sys.AptUpdate(ctx)
		back, failed := sys.AptInstall(ctx, aptBack)
		rep.Reinstalled = append(rep.Reinstalled, back...)
		for pkg, err := range failed {
			fail("réinstallation de "+pkg, err)
		}
	}
	if len(apt) > 0 {
		if err := sys.AptRemove(ctx, apt); err != nil {
			fail("paquets apt", err)
		} else {
			rep.Removed = append(rep.Removed, apt...)
		}
	}
	for i := len(st.Sys) - 1; i >= 0; i-- {
		if r := st.Sys[i]; r.Op == journal.SysUserCreated {
			if err := sys.DeleteUser(ctx, r.Name); err != nil {
				fail(r.Name, err)
			} else {
				rep.UsersDeleted = append(rep.UsersDeleted, r.Name)
			}
		}
	}
	return rep
}
