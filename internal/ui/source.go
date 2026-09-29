package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/bernard-linux/bernard/internal/agent"
	"github.com/bernard-linux/bernard/internal/collect/linux"
	"github.com/bernard-linux/bernard/internal/discovery"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/pack"
	"github.com/bernard-linux/bernard/internal/remote"
	"github.com/bernard-linux/bernard/internal/services"
	"github.com/bernard-linux/bernard/internal/session"
	"github.com/bernard-linux/bernard/internal/sysexec"
)

// Écrans de l'ancien ordinateur (la source).
const (
	StepRole       = "role"        // nouvel ou ancien ordinateur ?
	StepSrcHome    = "src-home"    // vers le réseau ou vers un disque ?
	StepSrcWait    = "src-wait"    // attente du nouvel ordinateur
	StepSrcCode    = "src-code"    // nouvel ordinateur trouvé : saisie du code
	StepSrcSend    = "src-send"    // envoi en cours
	StepSrcDisk    = "src-disk"    // choix du disque et phrase de passe
	StepSrcDone    = "src-done"    // terminé
	StepSrcStopped = "src-stopped" // interrompu
)

// TargetInfo décrit un nouvel ordinateur vu sur le réseau.
type TargetInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Link string `json:"link"` // meilleure liaison, en clair
}

// InvSummary résume l'inventaire de cet ordinateur.
type InvSummary struct {
	Distro   string `json:"distro"`
	Version  string `json:"version"`
	Desktop  string `json:"desktop"`
	Users    int    `json:"users"`
	Apps     int    `json:"apps"`
	Bytes    int64  `json:"bytes"`
	Files    int64  `json:"files"`
	Hostname string `json:"hostname"`
}

// SendInfo est l'avancement de l'envoi (réseau ou disque).
type SendInfo struct {
	Phase     string    `json:"phase"` // prepare, analysing, choose, system, copy, settings, done
	Files     int64     `json:"files"`
	Bytes     int64     `json:"bytes"`
	Planned   int64     `json:"planned"`
	Rel       string    `json:"rel"`
	Link      string    `json:"link,omitempty"`
	LinkLost  bool      `json:"linkLost"`
	StartedAt time.Time `json:"startedAt"`
	Dest      string    `json:"dest,omitempty"` // dossier du paquet (disque)
	Errors    []string  `json:"errors,omitempty"`
}

// DiskInfo est un disque externe où écrire un paquet.
type DiskInfo struct {
	Path  string `json:"path"`
	Label string `json:"label"`
	Free  int64  `json:"free"`
}

// sourceRoot est le système lu côté ancien ordinateur : « / », sauf pour
// les essais (BERNARD_SOURCE_ROOT désigne alors un système factice).
func sourceRoot() string {
	if r := os.Getenv("BERNARD_SOURCE_ROOT"); r != "" {
		return r
	}
	return "/"
}

// ChooseRole mémorise le rôle de cet ordinateur et affiche l'écran suivant.
func (c *Controller) ChooseRole(role string) error {
	switch role {
	case "target":
		c.update(func(s *State) { s.Role, s.Step, s.Error = "target", StepWelcome, "" })
	case "source":
		c.update(func(s *State) { s.Role, s.Step, s.Error = "source", StepSrcHome, "" })
	default:
		return fmt.Errorf("rôle inconnu : %q", role)
	}
	return nil
}

// ---------------------------------------------------------------- réseau

// StartSource attend le nouvel ordinateur, aussi longtemps qu'il le faut.
func (c *Controller) StartSource() error {
	ctx, err := c.begin()
	if err != nil {
		return err
	}
	tr, err := discovery.Track(ctx, 0)
	if err != nil {
		c.end()
		return errors.New("écoute du réseau impossible : Bernard est-il déjà ouvert sur cet ordinateur (ou bernard-agent dans un terminal) ?")
	}
	c.mu.Lock()
	c.tracker, c.srcInv, c.invErr, c.invReady = tr, nil, nil, make(chan struct{})
	c.mu.Unlock()
	c.update(func(s *State) {
		s.Role, s.Step, s.Error, s.Targets, s.Waited, s.Send = "source", StepSrcWait, "", nil, 0, nil
	})
	go c.watchTargets(ctx, tr)
	return nil
}

// watchTargets tient à jour la liste des nouveaux ordinateurs visibles et
// passe à la saisie du code dès qu'il y en a un.
func (c *Controller) watchTargets(ctx context.Context, tr *discovery.Tracker) {
	start := time.Now()
	var lastSeen time.Time
	collecting := false
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		step := c.Snapshot().Step
		if step != StepSrcWait && step != StepSrcCode {
			return
		}
		targets := tr.Targets()
		infos := make([]TargetInfo, 0, len(targets))
		for _, t := range targets {
			infos = append(infos, TargetInfo{ID: t.ID, Name: t.Name, Link: agent.Describe(t.Best())})
		}
		if len(targets) > 0 {
			lastSeen = time.Now()
			if !collecting {
				// L'inventaire est fait maintenant, pas au lancement : l'attente
				// a pu durer des heures.
				collecting = true
				go c.collectSource(ctx)
			}
		}
		c.update(func(s *State) {
			s.Targets, s.Waited = infos, int64(time.Since(start).Seconds())
			switch {
			case s.Step == StepSrcWait && len(infos) > 0:
				s.Step = StepSrcCode
			case s.Step == StepSrcCode && len(infos) == 0 && !s.Busy && time.Since(lastSeen) > 15*time.Second:
				s.Step = StepSrcWait // Bernard fermé sur le nouvel ordinateur
			}
		})
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// collectSource fait l'inventaire de cet ordinateur (lecture seule).
func (c *Controller) collectSource(ctx context.Context) {
	inv, err := agent.Collect(ctx, sourceRoot(), false)
	c.mu.Lock()
	c.srcInv, c.invErr = inv, err
	ready := c.invReady
	c.mu.Unlock()
	if err == nil {
		sum := summarize(inv)
		c.update(func(s *State) { s.Inventory = sum })
	}
	close(ready)
}

func summarize(inv *inventory.Inventory) *InvSummary {
	sum := &InvSummary{Distro: inv.Source.Distro, Version: inv.Source.Version, Desktop: inv.Source.Desktop,
		Users: len(inv.Users), Apps: len(inv.Apps), Hostname: inv.Source.Hostname}
	for _, d := range inv.DataSets {
		sum.Bytes += d.SizeBytes
		sum.Files += d.Files
	}
	return sum
}

// waitInventory attend la fin de l'inventaire.
func (c *Controller) waitInventory(ctx context.Context) (*inventory.Inventory, error) {
	c.mu.Lock()
	ready := c.invReady
	c.mu.Unlock()
	select {
	case <-ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.srcInv, c.invErr
}

// Pair s'appaire avec le nouvel ordinateur choisi, puis envoie les données.
func (c *Controller) Pair(targetID, code string) error {
	code = strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, code)
	if len(code) != 6 {
		return errors.New("le code comporte 6 chiffres")
	}
	c.mu.Lock()
	tr, ctx := c.tracker, c.ctx
	c.mu.Unlock()
	st := c.Snapshot()
	if tr == nil || ctx == nil || st.Step != StepSrcCode || st.Busy {
		return errors.New("aucun nouvel ordinateur en attente")
	}
	t, ok := tr.Target(targetID)
	if !ok {
		if ts := tr.Targets(); len(ts) == 1 && targetID == "" {
			t, ok = ts[0], true
		}
	}
	if !ok {
		return errors.New("ce nouvel ordinateur n'est plus visible : Bernard y est-il toujours ouvert ?")
	}
	c.update(func(s *State) { s.Busy, s.Error = true, "" })
	go c.pairAndSend(ctx, tr, t, code)
	return nil
}

func (c *Controller) pairAndSend(ctx context.Context, tr *discovery.Tracker, t discovery.Target, code string) {
	var conn *session.Conn
	var err error
	for _, r := range t.Routes { // meilleure liaison d'abord
		pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		conn, err = agent.Pair(pctx, r.Addr, code)
		cancel()
		if err == nil || errors.Is(err, session.ErrBadCode) || errors.Is(err, session.ErrCodeRevoked) ||
			strings.Contains(err.Error(), "code") {
			break
		}
	}
	if err != nil {
		msg := "Connexion impossible : " + err.Error()
		switch {
		case errors.Is(err, session.ErrBadCode) || strings.Contains(err.Error(), "incorrect"):
			msg = "Ce code ne correspond pas. Vérifiez le code affiché sur le nouvel ordinateur."
		case strings.Contains(err.Error(), "nouveau code"):
			msg = "Le nouvel ordinateur affiche maintenant un nouveau code : saisissez-le."
		}
		c.update(func(s *State) { s.Busy, s.Error = false, msg })
		return
	}
	addr := conn.RemoteAddr().String()
	c.update(func(s *State) {
		s.Step, s.Peer, s.Busy, s.Error = StepSrcSend, conn.PeerName, true, ""
		s.Send = &SendInfo{Phase: "prepare", StartedAt: time.Now(), Link: agent.LinkName(t.Best().Link)}
	})
	inv, err := c.waitInventory(ctx)
	if err != nil {
		conn.Close()
		c.srcFail(fmt.Errorf("inventaire de cet ordinateur impossible : %w", err))
		return
	}
	c.update(func(s *State) {
		s.Send.Phase = "analysing"
		if s.Send.Planned == 0 {
			s.Send.Planned = s.Inventory.Bytes
		}
	})
	srv := agent.NewServer(ctx, inv, sourceRoot(), func(rel string, n int64) {
		c.update(func(s *State) { s.Send.Files++ })
	}, func(rel string, n int64) {
		c.update(func(s *State) {
			s.Send.Bytes += n
			s.Send.Rel = rel
		})
	}, func(st remote.Status) {
		c.update(func(s *State) {
			s.Send.Phase = st.Phase
			if st.Planned > 0 {
				s.Send.Planned = st.Planned
			}
		})
	})
	err = agent.Serve(ctx, conn, srv, tr, t.ID, addr, c.srcLog, func(a string) {
		label := "réseau"
		if r, ok := agent.RouteFor(tr, t.ID, a); ok {
			label = agent.LinkName(r.Link)
		}
		c.update(func(s *State) {
			if s.Send != nil {
				s.Send.Link, s.Send.LinkLost = label, false
			}
		})
	})
	if err != nil {
		if ctx.Err() == nil {
			c.srcFail(err)
		}
		return
	}
	c.update(func(s *State) { s.Step, s.Busy, s.Send.Phase = StepSrcDone, false, "done" })
	c.end()
}

func (c *Controller) srcLog(msg string) {
	c.log(msg)
	if strings.HasPrefix(msg, "Liaison perdue") {
		c.update(func(s *State) {
			if s.Send != nil {
				s.Send.LinkLost = true
			}
		})
	}
}

func (c *Controller) srcFail(err error) {
	c.update(func(s *State) {
		s.Busy, s.Error = false, err.Error()
		if s.Step == StepSrcSend {
			s.Step = StepSrcStopped
		}
	})
	c.end()
}

// ---------------------------------------------------------------- disque

// ShowSourceDisk liste les disques externes et prépare l'inventaire.
func (c *Controller) ShowSourceDisk() error {
	ctx, err := c.begin()
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.srcInv, c.invErr, c.invReady = nil, nil, make(chan struct{})
	c.mu.Unlock()
	disks := FindDisks()
	c.update(func(s *State) { s.Role, s.Step, s.Disks, s.Error, s.Send = "source", StepSrcDisk, disks, "", nil })
	go c.collectSource(ctx)
	return nil
}

// RefreshDisks met à jour la liste (disque branché après coup).
func (c *Controller) RefreshDisks() {
	disks := FindDisks()
	c.update(func(s *State) { s.Disks = disks })
}

// FindDisks liste les disques externes montés, avec leur espace libre.
func FindDisks() []DiskInfo {
	b, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil
	}
	var out []DiskInfo
	seen := map[string]bool{}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		mp := strings.ReplaceAll(f[1], `\040`, " ")
		if !(strings.HasPrefix(mp, "/media/") || strings.HasPrefix(mp, "/run/media/") || strings.HasPrefix(mp, "/mnt/")) || seen[mp] {
			continue
		}
		// Un vrai disque (périphérique bloc), monté en écriture ; les essais
		// peuvent autoriser un disque en mémoire (BERNARD_TEST_DISKS).
		opts := ""
		if len(f) > 3 {
			opts = "," + f[3] + ","
		}
		if strings.Contains(opts, ",ro,") || (!strings.HasPrefix(f[0], "/dev/") && os.Getenv("BERNARD_TEST_DISKS") == "") {
			continue
		}
		var st syscall.Statfs_t
		if syscall.Statfs(mp, &st) != nil || st.Bavail == 0 {
			continue
		}
		seen[mp] = true
		out = append(out, DiskInfo{Path: mp, Label: filepath.Base(mp), Free: int64(st.Bavail) * int64(st.Bsize)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// StartPack écrit le paquet chiffré sur le disque choisi.
func (c *Controller) StartPack(disk, passphrase string) error {
	if len(passphrase) < 8 {
		return errors.New("la phrase de passe doit faire au moins 8 caractères")
	}
	var chosen *DiskInfo
	for _, d := range FindDisks() {
		if d.Path == disk {
			chosen = &d
			break
		}
	}
	if chosen == nil {
		return errors.New("ce disque n'est plus branché")
	}
	c.mu.Lock()
	ctx := c.ctx
	c.mu.Unlock()
	if ctx == nil || c.Snapshot().Busy {
		return errors.New("aucune préparation en cours")
	}
	host, _ := os.Hostname()
	dest := filepath.Join(disk, fmt.Sprintf("Bernard - %s - %s", host, time.Now().Format("2006-01-02")))
	for i := 2; ; i++ {
		if _, err := os.Lstat(dest); errors.Is(err, os.ErrNotExist) {
			break
		}
		dest = filepath.Join(disk, fmt.Sprintf("Bernard - %s - %s (%d)", host, time.Now().Format("2006-01-02"), i))
	}
	c.update(func(s *State) {
		s.Step, s.Busy, s.Error = StepSrcSend, true, ""
		s.Send = &SendInfo{Phase: "prepare", StartedAt: time.Now(), Dest: dest, Link: "disque externe"}
	})
	go func() {
		inv, err := c.waitInventory(ctx)
		if err != nil {
			c.srcFail(fmt.Errorf("inventaire de cet ordinateur impossible : %w", err))
			return
		}
		var total int64
		for _, d := range inv.DataSets {
			total += d.SizeBytes
		}
		if total > chosen.Free {
			c.srcFail(fmt.Errorf("pas assez de place sur %s : %s nécessaires, %s libres", chosen.Label, human(total), human(chosen.Free)))
			return
		}
		if err := os.Mkdir(dest, 0o700); err != nil {
			c.srcFail(err)
			return
		}
		c.update(func(s *State) { s.Send.Phase, s.Send.Planned = "copy", total })
		var logins []string
		for _, u := range inv.Users {
			logins = append(logins, u.Login)
		}
		secrets, _ := linux.ReadPasswordHashes(sourceRoot(), logins)
		extras := linux.CollectExtras(ctx, sourceRoot(), inv.Source.Desktop, inv.Users, sysexec.Run)
		rep, err := pack.Write(ctx, inv, dest, passphrase, pack.WriteOptions{Secrets: secrets, Extras: extras, Prepare: packPrepare(ctx, sourceRoot()),
			OnFile: func(rel string, n int64) {
				c.update(func(s *State) { s.Send.Files++; s.Send.Bytes += n; s.Send.Rel = rel })
			}})
		if err != nil {
			if ctx.Err() == nil {
				c.srcFail(fmt.Errorf("écriture du paquet impossible : %w", err))
			}
			return
		}
		syscall.Sync() // le disque peut être retiré dès l'écran suivant
		c.update(func(s *State) {
			s.Step, s.Busy, s.Send.Phase, s.Send.Errors = StepSrcDone, false, "done", rep.Errors
		})
		c.end()
	}()
	return nil
}

func human(b int64) string {
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

// packPrepare arrête les services (bases…) pendant l'écriture de leurs
// données dans le paquet.
func packPrepare(ctx context.Context, root string) func(inventory.DataSet) (func(), error) {
	f := services.PrepareFor(ctx, os.Geteuid() == 0 && filepath.Clean(root) == "/")
	if f == nil {
		return nil
	}
	return func(ds inventory.DataSet) (func(), error) { return f(ds.Service) }
}
