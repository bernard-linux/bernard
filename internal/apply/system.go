package apply

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/bernard-linux/bernard/internal/collect/linux"
	"github.com/bernard-linux/bernard/internal/engine"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/journal"
	"github.com/bernard-linux/bernard/internal/plan"
	"github.com/bernard-linux/bernard/internal/services"
	"github.com/bernard-linux/bernard/internal/source"
	"github.com/bernard-linux/bernard/internal/sysexec"
	"github.com/bernard-linux/bernard/internal/system"
	"github.com/bernard-linux/bernard/internal/transfer"
)

// Emplacements où Bernard accepte d'écrire des données hors des dossiers
// personnels. La destination vient de l'ancien ordinateur : elle est
// vérifiée ici, pour qu'aucune donnée ne puisse atterrir dans le système
// lui-même (/usr, /bin, /boot…).
var systemDestAllowed = []string{"/etc", "/opt/", "/srv/", "/usr/local", "/var/www", "/var/lib/", "/root", "/home/", "/mnt/"}

var systemDestDenied = []string{"/var/lib/dpkg", "/var/lib/apt", "/var/lib/bernard", "/var/lib/snapd", "/var/lib/flatpak", "/var/lib/systemd"}

// Dossiers de la racine qu'un dossier « ajouté » ne peut pas être.
var rootReserved = map[string]bool{
	"bin": true, "boot": true, "dev": true, "etc": true, "home": true, "lib": true, "lib32": true, "lib64": true,
	"libx32": true, "media": true, "mnt": true, "opt": true, "proc": true, "root": true, "run": true, "sbin": true,
	"snap": true, "srv": true, "sys": true, "tmp": true, "usr": true, "var": true, "lost+found": true, "efi": true,
}

// SystemDestOK vérifie une destination hors dossiers personnels.
func SystemDestOK(dest string, homes map[string]bool) bool {
	clean := filepath.Clean(dest)
	if clean != dest || !strings.HasPrefix(clean, "/") || clean == "/" {
		return false
	}
	for _, d := range systemDestDenied {
		if clean == d || strings.HasPrefix(clean, d+"/") {
			return false
		}
	}
	if homes[clean] {
		return false // dossier personnel d'un compte : copié à part
	}
	parts := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	if len(parts) == 1 && !rootReserved[parts[0]] {
		return true // dossier ajouté à la racine (/data…)
	}
	for _, a := range systemDestAllowed {
		if strings.HasSuffix(a, "/") {
			if strings.HasPrefix(clean, a) && len(clean) > len(a) {
				return true
			}
		} else if clean == a || strings.HasPrefix(clean, a+"/") {
			return true
		}
	}
	return false
}

// ownerByName traduit les noms de propriétaire de la source en numéros de
// la cible. Un compte ou un groupe absent de la cible devient root.
func ownerByName() func(source.Entry) *transfer.Owner {
	uids, gids := map[string]int{}, map[string]int{}
	lookup := func(cache map[string]int, name string, f func(string) (string, error)) int {
		if name == "" {
			return 0
		}
		if v, ok := cache[name]; ok {
			return v
		}
		v := 0
		if s, err := f(name); err == nil {
			v, _ = strconv.Atoi(s)
		}
		cache[name] = v
		return v
	}
	uid := func(n string) (string, error) {
		u, err := user.Lookup(n)
		if err != nil {
			return "", err
		}
		return u.Uid, nil
	}
	gid := func(n string) (string, error) {
		g, err := user.LookupGroup(n)
		if err != nil {
			return "", err
		}
		return g.Gid, nil
	}
	return func(e source.Entry) *transfer.Owner {
		return &transfer.Owner{UID: lookup(uids, e.User, uid), GID: lookup(gids, e.Group, gid), Mode: e.Mode, Xattrs: e.Xattrs}
	}
}

// moveAside déplace path vers aside, même d'un système de fichiers à l'autre.
func moveAside(path, aside string) error {
	if err := os.MkdirAll(filepath.Dir(aside), 0o700); err != nil {
		return err
	}
	err := os.Rename(path, aside)
	if err == nil || !errors.Is(err, os.ErrExist) && !strings.Contains(err.Error(), "cross-device") {
		return err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		l, err := os.Readlink(path)
		if err != nil {
			return err
		}
		if err := os.Symlink(l, aside); err != nil {
			return err
		}
		return os.Remove(path)
	}
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(aside, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(aside)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	out.Close()
	return os.Remove(path)
}

// asideFor renvoie un emplacement libre pour mettre dst de côté.
func asideFor(root, dst string) string {
	base := filepath.Join(root, dst)
	cand := base
	for i := 2; ; i++ {
		if _, err := os.Lstat(cand); errors.Is(err, os.ErrNotExist) {
			return cand
		}
		cand = base + "." + strconv.Itoa(i)
	}
}

// CopySystem copie les données hors des dossiers personnels choisies dans le
// plan, au même emplacement que sur l'ancien ordinateur, avec leurs
// propriétaires et leurs droits. Un fichier déjà présent sur la cible est
// mis de côté (et remis en place par l'annulation) plutôt qu'écrasé.
func CopySystem(ctx context.Context, r *engine.Receiver, p *plan.Plan, inv *inventory.Inventory) (map[string]*transfer.TreeReport, error) {
	sets := map[string]inventory.DataSet{}
	for _, d := range inv.DataSets {
		if d.Kind == "system" {
			sets[d.ID] = d
		}
	}
	homes := map[string]bool{}
	for _, u := range inv.Users {
		homes[filepath.Clean(u.Home)] = true
	}
	aside := filepath.Join(filepath.Dir(r.Journal.Path), "avant-migration")
	r.OwnerFor = ownerByName()
	r.Replace = func(dst string) error {
		bk := asideFor(aside, dst)
		if err := r.Journal.Append(journal.Record{T: journal.RecSys, Op: journal.SysReplaced, Name: dst, Dst: bk}); err != nil {
			return err
		}
		r.State.Sys = append(r.State.Sys, journal.Record{Op: journal.SysReplaced, Name: dst, Dst: bk})
		return moveAside(dst, bk)
	}
	defer func() { r.OwnerFor, r.Replace = nil, nil }()

	out := map[string]*transfer.TreeReport{}
	etc := false
	for _, act := range p.Actions {
		if !act.Selected || act.Op != plan.OpSystemData || act.Package == "" {
			continue
		}
		ds, ok := sets[act.Package]
		if !ok {
			continue
		}
		oldPath := ds.Dest
		if act.To != "" {
			ds.Dest = act.To // autre disque : destination choisie sur la cible
		}
		if !SystemDestOK(ds.Dest, homes) {
			out[ds.ID] = &transfer.TreeReport{Errors: []transfer.FileError{{Path: ds.Dest, Err: "emplacement refusé"}}}
			continue
		}
		if ds.Dest == "/etc" {
			etc = true
			var keep []string
			for _, rel := range ds.Include {
				if !linux.EtcMachine(rel) {
					keep = append(keep, rel)
				}
			}
			if len(keep) == 0 {
				continue
			}
			ds.Include = keep
		}
		svc := services.New()
		if ds.Service != "" {
			svc.Stop(ctx, ds.Service) // service de la cible arrêté pendant qu'on dépose ses données
		}
		rep, err := r.CopyDataSetAs(ctx, ds, ds.Dest, nil)
		if ds.Service != "" {
			svc.Start(ctx, ds.Service)
		}
		out[ds.ID] = rep
		var fe *source.FileError
		if errors.As(err, &fe) {
			// Refus de la source pour ce jeu (machines virtuelles allumées,
			// service impossible à arrêter) : signalé, la suite continue.
			rep.Errors = append(rep.Errors, transfer.FileError{Path: ds.Dest, Err: fe.Msg})
			continue
		}
		if err != nil {
			return out, fmt.Errorf("%s : %w", ds.Dest, err)
		}
		if ds.Dest != oldPath {
			RewriteSteam(inv, oldPath, ds.Dest)
		}
	}
	if etc {
		// Services ajoutés ou modifiés dans /etc/systemd : pris en compte.
		sysexec.Run(ctx, sysexec.Cmd{Name: "systemctl", Args: []string{"daemon-reload"}})
	}
	return out, nil
}

// UndoReplaced remet en place les fichiers de la cible mis de côté, après
// que l'annulation des fichiers a retiré ceux de l'ancien ordinateur. Un
// fichier modifié depuis la migration (donc conservé) garde sa place : la
// version d'origine reste alors dans le dossier de sauvegarde.
func UndoReplaced(st *journal.State) (restored []string, errs []string) {
	for i := len(st.Sys) - 1; i >= 0; i-- {
		r := st.Sys[i]
		if r.Op != journal.SysReplaced {
			continue
		}
		if _, err := os.Lstat(r.Dst); err != nil {
			continue
		}
		if _, err := os.Lstat(r.Name); err == nil {
			errs = append(errs, r.Name+" : modifié depuis la migration, version d'origine gardée dans "+r.Dst)
			continue
		}
		if err := moveAside(r.Dst, r.Name); err != nil {
			errs = append(errs, r.Name+" : "+err.Error())
			continue
		}
		restored = append(restored, r.Name)
	}
	return restored, errs
}

// Fichiers où Steam liste ses bibliothèques, relatifs au dossier personnel.
var steamVDF = []string{".local/share/Steam/steamapps/libraryfolders.vdf", ".steam/steam/steamapps/libraryfolders.vdf",
	".var/app/com.valvesoftware.Steam/.local/share/Steam/steamapps/libraryfolders.vdf",
	".local/share/Steam/config/libraryfolders.vdf"}

// RewriteSteam met à jour l'emplacement d'une bibliothèque Steam déplacée
// (autre disque copié ou rattaché ailleurs), pour que Steam la retrouve.
func RewriteSteam(inv *inventory.Inventory, oldPath, newPath string) {
	if oldPath == newPath || oldPath == "" {
		return
	}
	for _, u := range inv.Users {
		_, _, home, err := system.Owner(u.Login)
		if err != nil {
			continue
		}
		for _, rel := range steamVDF {
			f := filepath.Join(home, rel)
			b, err := os.ReadFile(f)
			if err != nil || !strings.Contains(string(b), `"`+oldPath) {
				continue
			}
			out := strings.ReplaceAll(string(b), `"`+oldPath+`"`, `"`+newPath+`"`)
			out = strings.ReplaceAll(out, `"`+oldPath+`/`, `"`+newPath+`/`)
			fi, _ := os.Stat(f)
			if os.WriteFile(f+".bernard-tmp", []byte(out), fi.Mode().Perm()) == nil {
				if st, ok := fi.Sys().(*syscall.Stat_t); ok {
					os.Chown(f+".bernard-tmp", int(st.Uid), int(st.Gid))
				}
				os.Rename(f+".bernard-tmp", f)
			}
		}
	}
}
