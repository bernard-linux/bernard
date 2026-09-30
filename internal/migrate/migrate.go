// Package migrate enchaîne une migration réelle : préparation du plan à
// partir d'une source, puis exécution des choix de l'utilisateur.
//
// La ligne de commande, l'interface graphique et la future interface texte
// l'utilisent toutes : elles ne diffèrent que par la façon de présenter le
// plan et de recueillir les choix.
package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/bernard-linux/bernard/internal/apply"
	"github.com/bernard-linux/bernard/internal/engine"
	"github.com/bernard-linux/bernard/internal/i18n"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/journal"
	"github.com/bernard-linux/bernard/internal/plan"
	"github.com/bernard-linux/bernard/internal/settings"
	"github.com/bernard-linux/bernard/internal/source"
	"github.com/bernard-linux/bernard/internal/sysexec"
	"github.com/bernard-linux/bernard/internal/system"
	"github.com/bernard-linux/bernard/internal/transfer"
)

// StateDir contient les journaux et rapports des migrations réelles.
var StateDir = "/var/lib/bernard"

// Session est une migration préparée, en attente des choix.
type Session struct {
	Inv         *inventory.Inventory
	Plan        *plan.Plan
	Warnings    []string
	JournalPath string
	Choices     Choices // choix retenus, réappliqués après une reconnexion
}

// JournalFor renvoie l'emplacement du journal d'un inventaire.
func JournalFor(inv *inventory.Inventory) string {
	id := strings.TrimPrefix(inv.Identity(), "id:")
	return filepath.Join(StateDir, id[:16], "journal.jsonl")
}

// Prepare lit l'inventaire de la source et calcule le plan pour cette
// machine. Ne modifie rien.
func Prepare(ctx context.Context, src source.Source) (*Session, error) {
	inv, err := src.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	target, warnings := plan.DetectTarget(ctx, sysexec.Exec)
	p, err := plan.Build(inv, target)
	if err != nil {
		return nil, err
	}
	return &Session{Inv: inv, Plan: p, Warnings: warnings, JournalPath: JournalFor(inv)}, nil
}

// Key identifie une action de façon stable d'un calcul de plan à l'autre
// (après une reconnexion, le plan est recalculé et les choix réappliqués).
func Key(a plan.Action) string { return a.Op + "|" + a.From + "|" + a.Package + "|" + a.Label }

// Choices sont les décisions de l'utilisateur.
type Choices struct {
	Selected  map[string]bool   `json:"selected"`  // par Key ; absent = choix par défaut
	Passwords map[string]string `json:"passwords"` // par identifiant, pour les comptes « ask »
}

// ApplyTo reporte les choix sur un plan (recalculé ou non).
func (c Choices) ApplyTo(p *plan.Plan) {
	for i, a := range p.Actions {
		if v, ok := c.Selected[Key(a)]; ok {
			p.Actions[i].Selected = v
		}
	}
	p.Recheck()
}

// Hooks transmet l'avancement à l'interface.
type Hooks struct {
	Log      func(string)
	Progress func(engine.Progress)
	Phase    func(string) // "system", "copy"
}

// Result est le bilan d'une exécution.
type Result struct {
	System   *apply.Report                   `json:"system"`
	Settings *apply.SettingsReport           `json:"settings,omitempty"`
	Data     map[string]*transfer.TreeReport `json:"data"`
	// Replaced : fichiers de profil (trousseau, navigateurs) de l'ancien
	// ordinateur mis à la place de ceux, neufs, du nouveau.
	Replaced    []string `json:"replaced,omitempty"`
	JournalPath string   `json:"journalPath"`
	ReportPath  string   `json:"reportPath"`
	// Bilan : pages lisibles déposées dans le dossier Documents des comptes.
	Bilan []string `json:"bilan,omitempty"`
}

// OK indique une migration sans aucun élément manqué.
func (r *Result) OK() bool {
	if r.System != nil && len(r.System.Failed) > 0 {
		return false
	}
	for _, d := range r.Data {
		if !d.OK() {
			return false
		}
	}
	return true
}

type secretSource interface {
	Secrets(ctx context.Context) (map[string]string, error)
}

type extrasSource interface {
	Extras(ctx context.Context, out any) error
}

// Extras demande les réglages lus en administrateur, si la source sait les
// fournir. Sans eux, la migration se fait sans Wi-Fi ni réglages du bureau.
func Extras(ctx context.Context, src source.Source) *settings.Extras {
	ex := &settings.Extras{}
	if es, ok := src.(extrasSource); ok {
		if es.Extras(ctx, ex) != nil {
			return &settings.Extras{}
		}
	}
	return ex
}

// ErrPasswordMissing signale un compte à créer sans mot de passe fourni.
var ErrPasswordMissing = i18n.NewError("mot de passe manquant")

// MissingPasswords renvoie les comptes pour lesquels il faudra saisir un
// mot de passe, compte tenu des hachages disponibles.
func MissingPasswords(s *Session, secrets map[string]string) []string {
	var out []string
	for _, a := range s.Plan.Actions {
		if a.Selected && a.Op == plan.OpCreateUser && (a.Password == "ask" || secrets[a.Login] == "") {
			out = append(out, a.Login)
		}
	}
	return out
}

// Secrets demande les hachages à la source si elle sait les fournir.
func Secrets(ctx context.Context, src source.Source) map[string]string {
	if ss, ok := src.(secretSource); ok {
		if m, err := ss.Secrets(ctx); err == nil {
			return m
		}
	}
	return map[string]string{}
}

// Execute exécute le plan : comptes et applications, puis données. Peut être
// rappelée après une coupure : le journal fait sauter ce qui est fait.
func Execute(ctx context.Context, src source.Source, s *Session, ch Choices, secrets map[string]string, h Hooks) (*Result, error) {
	log := func(msg string) {
		if h.Log != nil {
			h.Log(msg)
		}
	}
	if s.Plan.Blocked {
		return nil, errors.New(i18n.T("espace disque insuffisant sur cet ordinateur"))
	}
	if err := os.MkdirAll(filepath.Dir(s.JournalPath), 0o700); err != nil {
		return nil, err
	}
	j, st, err := engine.Begin(s.JournalPath, s.Inv.Identity())
	if err != nil {
		return nil, err
	}
	defer j.Close()

	res := &Result{JournalPath: s.JournalPath}
	if h.Phase != nil {
		h.Phase("system")
	}
	ap := &apply.Applier{
		Sys: system.New(), Journal: j, State: st, Secrets: secrets, Log: h.Log,
		AskPassword: func(login string) (string, error) {
			if pw := ch.Passwords[login]; pw != "" {
				return pw, nil
			}
			return "", i18n.Errorf("%w pour %s", ErrPasswordMissing, login)
		},
	}
	res.System, err = ap.System(ctx, s.Plan, s.Inv)
	if err != nil {
		return res, err
	}

	if h.Phase != nil {
		h.Phase("copy")
	}
	log(i18n.T("Copie des données…"))
	created := map[string]bool{}
	for _, rec := range st.Sys {
		if rec.Op == journal.SysUserCreated {
			created[rec.Name] = true
		}
	}
	r := &engine.Receiver{Src: src, Journal: j, State: st, OnProgress: h.Progress}
	res.Data, res.Replaced, err = apply.CopyData(ctx, r, s.Plan, s.Inv, created)
	if err != nil {
		return res, err
	}
	sys, err := apply.CopySystem(ctx, r, s.Plan, s.Inv)
	for id, rep := range sys {
		res.Data[id] = rep
	}
	if err != nil {
		return res, err
	}

	if h.Phase != nil {
		h.Phase("settings")
	}
	log(i18n.T("Reprise des réglages…"))
	ex := Extras(ctx, src)
	for _, w := range ex.Warnings {
		log("  " + w)
	}
	res.Settings, err = ap.Settings(ctx, s.Plan, s.Inv, ex, settings.New(filepath.Dir(s.JournalPath)))
	if err != nil {
		return res, err
	}
	rebuildFontCaches(ctx, s, settings.New(filepath.Dir(s.JournalPath)), log)
	res.Bilan = WriteBilan(res, s, j)
	j.Append(journal.Record{T: journal.RecFinish})
	res.ReportPath = filepath.Join(filepath.Dir(s.JournalPath), "rapport.json")
	if b, err := json.MarshalIndent(res, "", "  "); err == nil {
		os.WriteFile(res.ReportPath, b, 0o600)
	}
	return res, nil
}

// rebuildFontCaches refait l'index des polices de chaque compte migré (voir
// settings.RebuildFontCache). Sans fc-cache, l'index se refera à la
// première ouverture de session.
func rebuildFontCaches(ctx context.Context, s *Session, sa *settings.Applier, log func(string)) {
	done := map[string]bool{}
	for _, a := range s.Plan.Actions {
		if !a.Selected || a.Op != plan.OpCopy || a.Login == "" || done[a.Login] {
			continue
		}
		done[a.Login] = true
		_, _, home, err := system.Owner(a.Login)
		if err != nil {
			continue
		}
		if err := sa.RebuildFontCache(ctx, a.Login, home); err != nil {
			log(i18n.Tf("  index des polices de %s : sera refait à la première ouverture de session", a.Login))
		}
	}
}

// Undo annule une migration : fichiers d'abord, puis applications et comptes.
func Undo(ctx context.Context, journalPath string) (*engine.UndoReport, *apply.UndoReport, error) {
	st, err := journal.Load(journalPath)
	if err != nil {
		return nil, nil, err
	}
	// Les profils échangés reprennent d'abord leur place, pour que
	// l'annulation des fichiers retrouve les copies sous leur nom d'origine.
	prefErrs := apply.UndoPreferSource(st)
	// Dossiers de services (bases, Docker) remis en entier avant tout le
	// reste : l'annulation fichier par fichier ne les touche alors plus.
	svcRestored, svcErrs := apply.UndoServiceData(ctx, st, filepath.Dir(journalPath))
	prefErrs = append(prefErrs, svcErrs...)
	files, err := engine.Undo(journalPath)
	if err != nil {
		return nil, nil, err
	}
	var sys *apply.UndoReport
	if len(st.Sys) > 0 {
		sys = apply.UndoSystem(ctx, st, system.New(), settings.New(filepath.Dir(journalPath)))
		sys.Errors = append(sys.Errors, prefErrs...)
		restored, errs := apply.UndoReplaced(st)
		sys.Restored = append(svcRestored, restored...)
		sys.Errors = append(sys.Errors, errs...)
		if len(restored) > 0 {
			sysexec.Run(ctx, sysexec.Cmd{Name: "systemctl", Args: []string{"daemon-reload"}})
		}
	}
	return files, sys, nil
}
