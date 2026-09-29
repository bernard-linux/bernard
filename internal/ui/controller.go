// Package ui expose l'assistant de migration par une API locale (HTTP sur
// 127.0.0.1, protégée par un jeton) et une interface web embarquée, affichée
// dans une fenêtre dédiée. Une future interface texte sera un second client
// de la même API.
package ui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/bernard-linux/bernard/internal/directlink"
	"github.com/bernard-linux/bernard/internal/discovery"
	"github.com/bernard-linux/bernard/internal/engine"
	"github.com/bernard-linux/bernard/internal/i18n"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/keepawake"
	"github.com/bernard-linux/bernard/internal/link"
	"github.com/bernard-linux/bernard/internal/migrate"
	"github.com/bernard-linux/bernard/internal/pack"
	"github.com/bernard-linux/bernard/internal/plan"
	"github.com/bernard-linux/bernard/internal/remote"
	"github.com/bernard-linux/bernard/internal/session"
	"github.com/bernard-linux/bernard/internal/source"
	"github.com/bernard-linux/bernard/internal/sysexec"
	"github.com/bernard-linux/bernard/internal/system"
)

// Étapes de l'assistant (écrans).
const (
	StepWelcome   = "welcome"
	StepNetwork   = "network"   // code affiché, attente de l'ancien ordinateur
	StepDisk      = "disk"      // choix du paquet et phrase de passe
	StepAnalysing = "analysing" // lecture de l'inventaire
	StepChoose    = "choose"    // comptes, données, applications
	StepRunning   = "running"
	StepReport    = "report"
	StepStopped   = "stopped" // interrompu, reprise possible
)

// Progress est l'avancement affiché pendant la migration.
type Progress struct {
	Phase     string    `json:"phase"` // system, copy
	Dataset   string    `json:"dataset"`
	Rel       string    `json:"rel"`
	Files     int64     `json:"files"`
	Total     int64     `json:"total"`
	Bytes     int64     `json:"bytes"`
	Planned   int64     `json:"planned"`
	StartedAt time.Time `json:"startedAt"`
	LinkLost  bool      `json:"linkLost"`
}

// UserInfo résume un compte de la source.
type UserInfo struct {
	Login    string `json:"login"`
	FullName string `json:"fullName"`
}

// UndoInfo résume une annulation.
type UndoInfo struct {
	Files int      `json:"files"`
	Dirs  int      `json:"dirs"`
	Apps  []string `json:"apps"`
	Back  []string `json:"reinstalled"` // applications remises en place
	Users []string `json:"users"`
	Kept  []string `json:"kept"`
	Errs  []string `json:"errors"`
}

// State est tout ce que l'interface affiche. Il est publié à chaque changement.
type State struct {
	Step      string                `json:"step"`
	Error     string                `json:"error,omitempty"`
	Host      string                `json:"host"`
	Code      string                `json:"code,omitempty"`
	Port      int                   `json:"port,omitempty"`
	Addresses []string              `json:"addresses,omitempty"`
	Peer      string                `json:"peer,omitempty"`
	Mode      string                `json:"mode,omitempty"` // network, disk
	Packs     []string              `json:"packs,omitempty"`
	Source    *inventory.Source     `json:"source,omitempty"`
	Users     map[string]UserInfo   `json:"users,omitempty"`
	Apps      map[string]*time.Time `json:"apps,omitempty"` // dernière utilisation par identifiant d'appli
	Plan      *plan.Plan            `json:"plan,omitempty"`
	NeedPass  []string              `json:"needPasswords,omitempty"`
	Warnings  []string              `json:"warnings,omitempty"`
	Progress  *Progress             `json:"progress,omitempty"`
	Log       []string              `json:"log,omitempty"`
	Result    *migrate.Result       `json:"result,omitempty"`
	Undo      *UndoInfo             `json:"undo,omitempty"`
	Busy      bool                  `json:"busy"`
	// Comptes de cet ordinateur qui ne viennent pas de l'ancien (compte
	// provisoire créé à l'installation), proposés à la suppression en fin
	// de migration.
	Extra []system.Account `json:"extraAccounts,omitempty"`

	// Rôle de cet ordinateur : target (nouveau) ou source (ancien).
	Role string `json:"role,omitempty"`
	// Côté ancien ordinateur.
	Targets   []TargetInfo `json:"targets,omitempty"`
	Waited    int64        `json:"waited,omitempty"` // secondes d'attente
	Inventory *InvSummary  `json:"inventory,omitempty"`
	Send      *SendInfo    `json:"send,omitempty"`
	Disks     []DiskInfo   `json:"disks,omitempty"`
}

// Controller pilote une migration à la fois.
type Controller struct {
	Port int // port d'appairage (51516)

	mu      sync.Mutex
	st      State
	subs    map[chan struct{}]struct{}
	cancel  context.CancelFunc
	choices chan migrate.Choices
	sess    *migrate.Session
	quit    chan struct{}

	ctx  context.Context // contexte de l'opération en cours
	lock *keepawake.Lock // pas de mise en veille pendant une opération
	// cable rend utilisable un câble direct entre les deux ordinateurs.
	cable *directlink.Watcher

	// Côté ancien ordinateur.
	tracker  *discovery.Tracker
	srcInv   *inventory.Inventory
	invErr   error
	invReady chan struct{}
}

// NewController prépare l'assistant.
func NewController() *Controller {
	host, _ := os.Hostname()
	return &Controller{
		Port:    51516,
		st:      State{Step: StepRole, Host: host},
		subs:    map[chan struct{}]struct{}{},
		choices: make(chan migrate.Choices, 1),
		quit:    make(chan struct{}),
	}
}

// Quit est fermé quand l'utilisateur demande à quitter.
func (c *Controller) Quit() <-chan struct{} { return c.quit }

// Snapshot renvoie une copie de l'état.
func (c *Controller) Snapshot() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.st
	s.Log = append([]string(nil), c.st.Log...)
	return s
}

// Subscribe renvoie un canal notifié à chaque changement d'état.
func (c *Controller) Subscribe() (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	c.mu.Lock()
	c.subs[ch] = struct{}{}
	c.mu.Unlock()
	return ch, func() {
		c.mu.Lock()
		delete(c.subs, ch)
		c.mu.Unlock()
	}
}

// update modifie l'état sous verrou puis prévient les abonnés.
func (c *Controller) update(f func(*State)) {
	c.mu.Lock()
	f(&c.st)
	for ch := range c.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	c.mu.Unlock()
}

func (c *Controller) log(msg string) {
	c.update(func(s *State) {
		s.Log = append(s.Log, time.Now().Format("15:04:05")+"  "+msg)
		if len(s.Log) > 300 {
			s.Log = s.Log[len(s.Log)-300:]
		}
		switch msg {
		case linkLostMsg, i18n.T(linkLostMsg):
			if s.Progress != nil {
				s.Progress.LinkLost = true
			}
		case linkBackMsg, i18n.T(linkBackMsg):
			if s.Progress != nil {
				s.Progress.LinkLost = false
			}
		}
	})
}

// Messages du journal envoyés par le paquet link, reconnus ici en français
// comme en traduction.
const (
	linkLostMsg = "Liaison perdue. En attente de l'ancien ordinateur ; le transfert reprendra seul…"
	linkBackMsg = "Reconnecté. Reprise du transfert."
)

func (c *Controller) fail(err error) {
	c.update(func(s *State) {
		s.Busy = false
		s.Error = err.Error()
		if s.Step == StepRunning {
			s.Step = StepStopped
		}
	})
}

func (c *Controller) begin() (context.Context, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		return nil, errors.New(i18n.T("une migration est déjà en cours"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel, c.ctx = cancel, ctx
	// Un ordinateur en veille disparaît du réseau : la veille est bloquée
	// tant que Bernard attend ou transfère.
	c.lock = keepawake.Acquire(i18n.T("Bernard : migration en cours"))
	c.cable = directlink.Start(c.log)
	return ctx, nil
}

func (c *Controller) end() {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.ctx, c.tracker = nil, nil
	lock, cable := c.lock, c.cable
	c.lock, c.cable = nil, nil
	c.mu.Unlock()
	lock.Release()
	cable.Stop()
}

// ---------------------------------------------------------------- réseau

// StartNetwork affiche le code et attend l'ancien ordinateur.
func (c *Controller) StartNetwork() error {
	ctx, err := c.begin()
	if err != nil {
		return err
	}
	lc := net.ListenConfig{KeepAlive: session.KeepAlive}
	ln, err := lc.Listen(ctx, "tcp", ":"+strconv.Itoa(c.Port))
	if err != nil {
		c.end()
		return i18n.Errorf("le port %d est occupé : une autre instance de Bernard est-elle ouverte ?", c.Port)
	}
	pairer, err := session.NewPairer(c.st.Host)
	if err != nil {
		ln.Close()
		c.end()
		return err
	}
	cfg, err := session.ServerConfig()
	if err != nil {
		ln.Close()
		c.end()
		return err
	}
	idb := make([]byte, 8)
	rand.Read(idb)
	go discovery.Announce(ctx, discovery.Beacon{ID: hex.EncodeToString(idb), Name: c.st.Host, Port: c.Port})
	go func() { <-ctx.Done(); ln.Close() }()

	var addrs []string
	for _, itf := range discovery.Interfaces() {
		if ip := ifaceIP(itf.Name); ip != "" {
			addrs = append(addrs, ip+":"+strconv.Itoa(c.Port))
		}
	}
	c.update(func(s *State) {
		s.Step, s.Mode, s.Code, s.Port, s.Addresses, s.Error = StepNetwork, "network", pairer.Code(), c.Port, addrs, ""
	})
	accept := link.NewAccepter(ln, pairer, cfg, nil, func(err error) {
		c.log(i18n.Tf("Connexion refusée : %s", err.Error()))
		if renewed, _ := pairer.RenewIfNeeded(); renewed {
			c.update(func(s *State) { s.Code = pairer.Code() })
		}
	})
	go func() {
		defer c.end()
		first, err := accept(ctx)
		if err != nil {
			if ctx.Err() == nil {
				c.fail(err)
			}
			return
		}
		c.update(func(s *State) { s.Peer, s.Code = first.PeerName, "" })
		// Liaison perdue : un nouveau code est affiché, pour que l'ancien
		// ordinateur puisse aussi reprendre après un arrêt volontaire (le code
		// initial ne sert qu'une fois). Le journal vérifie qu'il s'agit bien
		// de la même migration.
		logf := func(msg string) {
			c.log(msg)
			switch msg {
			case linkLostMsg, i18n.T(linkLostMsg):
				if pairer.Renew() == nil {
					c.update(func(s *State) { s.Code = pairer.Code() })
				}
			case linkBackMsg, i18n.T(linkBackMsg):
				c.update(func(s *State) { s.Code = "" })
			}
		}
		err = link.ReceiveWithReconnect(ctx, first, accept, func(ctx context.Context, cli *remote.Client) error {
			return c.run(ctx, cli)
		}, logf)
		if err != nil && ctx.Err() == nil {
			c.fail(err)
		}
	}()
	return nil
}

func ifaceIP(name string) string {
	itf, err := net.InterfaceByName(name)
	if err != nil {
		return ""
	}
	addrs, _ := itf.Addrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
			return ipn.IP.String()
		}
	}
	return ""
}

// ---------------------------------------------------------------- disque

// ShowDisk passe à l'écran de choix du paquet.
func (c *Controller) ShowDisk() {
	packs := FindPacks()
	c.update(func(s *State) { s.Step, s.Mode, s.Packs, s.Error = StepDisk, "disk", packs, "" })
}

// FindPacks cherche les paquets Bernard sur les disques montés.
func FindPacks() []string {
	var out []string
	seen := map[string]bool{}
	for _, pattern := range []string{
		"/media/*/*/bernard-pack.json", "/media/*/*/*/bernard-pack.json",
		"/run/media/*/*/bernard-pack.json", "/run/media/*/*/*/bernard-pack.json",
		"/mnt/*/bernard-pack.json", "/mnt/*/*/bernard-pack.json",
	} {
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			d := filepath.Dir(m)
			if !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	sort.Strings(out)
	return out
}

// StartDisk ouvre un paquet et prépare le plan.
func (c *Controller) StartDisk(dir, passphrase string) error {
	p, err := pack.Open(dir, passphrase)
	if err != nil {
		return err
	}
	ctx, err := c.begin()
	if err != nil {
		return err
	}
	c.update(func(s *State) { s.Peer, s.Error = dir, "" })
	go func() {
		defer c.end()
		if err := c.run(ctx, p); err != nil && ctx.Err() == nil {
			c.fail(err)
		}
	}()
	return nil
}

// ---------------------------------------------------------------- déroulé

// run prépare le plan, attend les choix (la première fois seulement) et
// exécute. Rappelée après chaque reconnexion.
func (c *Controller) run(ctx context.Context, src source.Source) error {
	c.update(func(s *State) {
		if s.Step != StepRunning {
			s.Step = StepAnalysing
		}
	})
	announce := func(st remote.Status) {
		if a, ok := src.(interface {
			SendStatus(context.Context, remote.Status) error
		}); ok {
			a.SendStatus(ctx, st)
		}
	}
	sess, err := migrate.Prepare(ctx, src)
	if err != nil {
		return err
	}
	secrets := migrate.Secrets(ctx, src)

	c.mu.Lock()
	first := c.sess == nil
	prev := c.sess
	c.mu.Unlock()

	var ch migrate.Choices
	if first {
		users := map[string]UserInfo{}
		for _, u := range sess.Inv.Users {
			users[u.ID] = UserInfo{Login: u.Login, FullName: u.FullName}
		}
		apps := map[string]*time.Time{}
		for _, a := range sess.Inv.Apps {
			apps[a.ID] = a.LastUsed
		}
		src := sess.Inv.Source
		c.mu.Lock()
		c.sess = sess
		c.mu.Unlock()
		c.update(func(s *State) {
			s.Step, s.Source, s.Users, s.Apps, s.Plan, s.Warnings = StepChoose, &src, users, apps, sess.Plan, sess.Warnings
			s.NeedPass = migrate.MissingPasswords(sess, secrets)
		})
		announce(remote.Status{Phase: "choose"})
		select {
		case ch = <-c.choices:
		case <-ctx.Done():
			return ctx.Err()
		}
		c.mu.Lock()
		c.sess.Choices = ch
		c.mu.Unlock()
	} else {
		ch = prev.Choices
		ch.ApplyTo(sess.Plan)
		sess.Choices = ch
		c.mu.Lock()
		c.sess = sess
		c.mu.Unlock()
	}
	ch.ApplyTo(sess.Plan)

	c.update(func(s *State) {
		s.Step, s.Plan, s.Busy = StepRunning, sess.Plan, true
		if s.Progress == nil {
			s.Progress = &Progress{StartedAt: time.Now(), Planned: sess.Plan.Totals.Bytes}
		}
	})
	var base int64
	c.mu.Lock()
	if c.st.Progress != nil {
		base = c.st.Progress.Bytes
	}
	c.mu.Unlock()

	res, err := migrate.Execute(ctx, src, sess, ch, secrets, migrate.Hooks{
		Log: c.log,
		Phase: func(ph string) {
			c.update(func(s *State) { s.Progress.Phase = ph })
			announce(remote.Status{Phase: ph, Planned: sess.Plan.Totals.Bytes})
		},
		Progress: func(p engine.Progress) {
			c.update(func(s *State) {
				s.Progress.Dataset, s.Progress.Rel = p.Dataset, p.Rel
				s.Progress.Files, s.Progress.Total = p.Files, p.Total
				s.Progress.Bytes = base + p.Bytes
			})
		},
	})
	if err != nil {
		return err
	}
	extra := extraAccounts(sess)
	c.update(func(s *State) { s.Step, s.Result, s.Busy, s.Extra = StepReport, res, false, extra })
	return nil
}

// extraAccounts liste les comptes de la cible qui ne font pas partie de la
// migration.
func extraAccounts(sess *migrate.Session) []system.Account {
	migrated := map[string]bool{}
	for _, u := range sess.Inv.Users {
		migrated[u.Login] = true
	}
	accs, err := system.HumanAccounts()
	if err != nil {
		return nil
	}
	current := os.Getenv("PKEXEC_UID")
	var out []system.Account
	for _, a := range accs {
		if migrated[a.Login] {
			continue
		}
		a.Files, a.Bytes = system.MeasureHome(a.Home)
		a.Current = current != "" && current == strconv.Itoa(a.UID)
		a.Scheduled = system.RemovalScheduled(a.Login)
		out = append(out, a)
	}
	return out
}

// ScheduleRemoval programme (ou annule) la suppression d'un compte non
// migré au prochain démarrage.
func (c *Controller) ScheduleRemoval(login string, on bool) error {
	c.mu.Lock()
	sess, step := c.sess, c.st.Step
	c.mu.Unlock()
	if step != StepReport || sess == nil {
		return errors.New(i18n.T("possible seulement à la fin de la migration"))
	}
	allowed := false
	for _, a := range extraAccounts(sess) {
		allowed = allowed || a.Login == login
	}
	if !allowed {
		return errors.New(i18n.T("ce compte fait partie de la migration : il ne peut pas être supprimé ici"))
	}
	sys := system.New()
	var err error
	if on {
		self, _ := os.Executable()
		err = sys.ScheduleRemoval(context.Background(), login, self)
		if errors.Is(err, system.ErrLastAdmin) {
			err = errors.New(i18n.T("aucun compte migré n'est administrateur : gardez ce compte, sinon plus personne ne pourrait gérer l'ordinateur"))
		}
	} else {
		err = sys.CancelRemoval(context.Background(), login)
	}
	extra := extraAccounts(sess)
	c.update(func(s *State) { s.Extra = extra })
	return err
}

// Submit transmet les choix de l'utilisateur. ids : identifiant d'action du
// plan affiché → coché ou non.
func (c *Controller) Submit(ids map[string]bool, passwords map[string]string) error {
	c.mu.Lock()
	sess := c.sess
	step := c.st.Step
	c.mu.Unlock()
	if sess == nil || step != StepChoose {
		return errors.New(i18n.T("aucun plan en attente de validation"))
	}
	ch := migrate.Choices{Selected: map[string]bool{}, Passwords: passwords}
	for _, a := range sess.Plan.Actions {
		if v, ok := ids[a.ID]; ok {
			ch.Selected[migrate.Key(a)] = v
		}
	}
	// Vérifications avant de lancer : espace et mots de passe.
	check := *sess.Plan
	check.Actions = append([]plan.Action(nil), sess.Plan.Actions...)
	ch.ApplyTo(&check)
	if check.Blocked {
		return errors.New(i18n.T("la sélection ne tient pas sur ce disque : décochez des dossiers"))
	}
	for _, login := range c.Snapshot().NeedPass {
		for _, a := range check.Actions {
			if a.Op == plan.OpCreateUser && a.Login == login && a.Selected && len(passwords[login]) < 1 {
				return i18n.Errorf("choisissez un mot de passe pour le compte %s", login)
			}
		}
	}
	select {
	case c.choices <- ch:
		return nil
	default:
		return errors.New(i18n.T("choix déjà transmis"))
	}
}

// Stop interrompt la migration en cours ; elle pourra reprendre.
func (c *Controller) Stop() {
	c.end()
	c.update(func(s *State) {
		switch s.Step {
		case StepRunning:
			s.Step = StepStopped
		case StepSrcSend:
			s.Step = StepSrcStopped
		}
		s.Busy = false
	})
}

// Reset revient à l'accueil (après une erreur avant exécution).
func (c *Controller) Reset() {
	c.end()
	c.mu.Lock()
	c.sess = nil
	select {
	case <-c.choices:
	default:
	}
	c.mu.Unlock()
	host := c.st.Host
	c.update(func(s *State) { *s = State{Step: StepRole, Host: host} })
}

// UndoMigration annule la migration terminée ou interrompue.
func (c *Controller) UndoMigration() error {
	c.mu.Lock()
	sess := c.sess
	c.mu.Unlock()
	if sess == nil {
		return errors.New(i18n.T("aucune migration à annuler"))
	}
	c.end()
	c.update(func(s *State) { s.Busy = true })
	for _, a := range extraAccounts(sess) {
		if a.Scheduled {
			system.New().CancelRemoval(context.Background(), a.Login)
		}
	}
	files, sys, err := migrate.Undo(context.Background(), sess.JournalPath)
	if err != nil {
		c.update(func(s *State) { s.Busy = false })
		return err
	}
	info := &UndoInfo{Files: files.Removed, Dirs: files.DirsRemoved, Kept: files.Kept}
	if sys != nil {
		info.Apps, info.Back, info.Users, info.Errs = sys.Removed, sys.Reinstalled, sys.UsersDeleted, sys.Errors
	}
	c.update(func(s *State) { s.Undo, s.Busy = info, false })
	return nil
}

// Reboot redémarre l'ordinateur, une fois la migration terminée : le
// trousseau de clés et les réglages du bureau repris ne sont pris en compte
// qu'à la prochaine ouverture de session.
func (c *Controller) Reboot() error {
	c.mu.Lock()
	step := c.st.Step
	c.mu.Unlock()
	if step != StepReport {
		return errors.New(i18n.T("redémarrage possible seulement à la fin de la migration"))
	}
	c.end()
	_, err := sysexec.Run(context.Background(), sysexec.Cmd{Name: "systemctl", Args: []string{"reboot"}})
	return err
}

// RequestQuit demande la fermeture de Bernard.
func (c *Controller) RequestQuit() {
	c.end()
	select {
	case <-c.quit:
	default:
		close(c.quit)
	}
}
