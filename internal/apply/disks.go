package apply

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/bernard-linux/bernard/internal/i18n"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/journal"
	"github.com/bernard-linux/bernard/internal/plan"
	"github.com/bernard-linux/bernard/internal/sysexec"
	"github.com/bernard-linux/bernard/internal/system"
)

// Fstab est la table des montages (remplacée dans les tests).
var Fstab = "/etc/fstab"

var uuidRe = regexp.MustCompile(`^[A-Za-z0-9-]{4,64}$`)

// fstabLine prépare la ligne de montage d'un disque rattaché. Les systèmes
// de fichiers sans propriétaires (FAT, exFAT, NTFS) sont attribués au
// premier compte, pour qu'il puisse y écrire.
func fstabLine(uuid, fstype, point string, uid, gid int) string {
	opts := "defaults,nofail"
	switch fstype {
	case "fuseblk", "ntfs", "ntfs3":
		fstype = "ntfs3"
		opts += fmt.Sprintf(",uid=%d,gid=%d", uid, gid)
	case "vfat", "exfat":
		opts += fmt.Sprintf(",uid=%d,gid=%d", uid, gid)
	case "":
		fstype = "auto"
	}
	return fmt.Sprintf("UUID=%s %s %s %s 0 2 # ajouté par Bernard\n", uuid, strings.ReplaceAll(point, " ", `\040`), fstype, opts)
}

// attachDisks monte à demeure les disques déplacés depuis l'ancien
// ordinateur. /etc/fstab est sauvegardé avant toute modification.
func (a *Applier) attachDisks(ctx context.Context, p *plan.Plan, inv *inventory.Inventory, rep *SettingsReport) error {
	items := map[string]inventory.SystemItem{}
	for _, it := range inv.System {
		items[it.ID] = it
	}
	uid, gid := 0, 0
	if len(inv.Users) > 0 {
		if u, g, _, err := system.Owner(inv.Users[0].Login); err == nil {
			uid, gid = u, g
		}
	}
	for _, act := range p.Actions {
		if !act.Selected || act.Op != plan.OpAttachDisk {
			continue
		}
		it := items[act.From]
		label := i18n.Tf("Disque rattaché : %s", act.To)
		if !uuidRe.MatchString(it.UUID) || !strings.HasPrefix(act.To, "/") || filepath.Clean(act.To) != act.To {
			rep.Failed[label] = i18n.T("identifiant ou emplacement refusé")
			continue
		}
		if a.did(journal.SysFstab, it.UUID) {
			continue
		}
		cur, err := os.ReadFile(Fstab)
		if err != nil {
			rep.Failed[label] = err.Error()
			continue
		}
		if strings.Contains(string(cur), "UUID="+it.UUID) {
			rep.Skipped[label] = i18n.T("déjà présent dans /etc/fstab")
			continue
		}
		backup := filepath.Join(filepath.Dir(a.Journal.Path), "fstab-"+strconv.Itoa(len(a.State.Sys)))
		if err := os.WriteFile(backup, cur, 0o600); err != nil {
			rep.Failed[label] = err.Error()
			continue
		}
		created := false
		if _, err := os.Stat(act.To); os.IsNotExist(err) {
			if err := os.MkdirAll(act.To, 0o755); err != nil {
				rep.Failed[label] = err.Error()
				continue
			}
			created = true
		}
		rec := journal.Record{T: journal.RecSys, Op: journal.SysFstab, Name: it.UUID, Dst: backup, Key: act.To}
		if created {
			rec.Status = "mkdir"
		}
		if err := a.Journal.Append(rec); err != nil {
			return err
		}
		a.State.Sys = append(a.State.Sys, rec)
		line := fstabLine(it.UUID, it.FSType, act.To, uid, gid)
		f, err := os.OpenFile(Fstab, os.O_APPEND|os.O_WRONLY, 0)
		if err == nil {
			if len(cur) > 0 && cur[len(cur)-1] != '\n' {
				f.WriteString("\n")
			}
			_, err = f.WriteString(line)
			f.Close()
		}
		if err != nil {
			rep.Failed[label] = err.Error()
			continue
		}
		sysexec.Run(ctx, sysexec.Cmd{Name: "systemctl", Args: []string{"daemon-reload"}})
		if _, err := sysexec.Run(ctx, sysexec.Cmd{Name: "mount", Args: []string{"--", act.To}}); err != nil {
			rep.Skipped[label] = i18n.Tf("ajouté au démarrage ; montage immédiat impossible (%v)", err)
		} else {
			rep.Applied = append(rep.Applied, label)
		}
		if it.Paths[0] != act.To {
			RewriteSteam(inv, it.Paths[0], act.To)
		}
	}
	return nil
}

// undoFstab démonte le disque et remet /etc/fstab d'origine.
func undoFstab(ctx context.Context, r journal.Record) error {
	sysexec.Run(ctx, sysexec.Cmd{Name: "umount", Args: []string{"--", r.Key}})
	b, err := os.ReadFile(r.Dst)
	if err != nil {
		return err
	}
	if err := os.WriteFile(Fstab, b, 0o644); err != nil {
		return err
	}
	if r.Status == "mkdir" {
		os.Remove(r.Key)
	}
	sysexec.Run(ctx, sysexec.Cmd{Name: "systemctl", Args: []string{"daemon-reload"}})
	return nil
}
