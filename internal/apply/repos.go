package apply

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/bernard-linux/bernard/internal/aptrepo"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/journal"
	"github.com/bernard-linux/bernard/internal/plan"
	"github.com/bernard-linux/bernard/internal/sysexec"
)

// Emplacements réels (remplacés dans les tests).
var (
	AptListDir = "/etc/apt/sources.list.d"
	AptRoot    = "/"
)

// addedRepo est un dépôt ajouté pendant cette exécution.
type addedRepo struct {
	label string
	file  string
	hosts []string
	keys  []string
}

// addRepos écrit les dépôts choisis et leurs clés (sans jamais remplacer une
// clé ou un fichier existant), en adaptant le nom de code de la version.
func (a *Applier) addRepos(p *plan.Plan, inv *inventory.Inventory, rep *Report) []addedRepo {
	bySrc := map[string]aptrepo.Source{}
	for _, s := range inv.AptSources {
		bySrc[s.File] = s
	}
	var out []addedRepo
	for _, act := range p.Actions {
		if !act.Selected || act.Op != plan.OpAddRepo {
			continue
		}
		src, ok := bySrc[act.Package]
		if !ok || !aptrepo.FileNameOK(src.File) || a.didAny2(journal.SysRepoAdded, src.File) {
			continue
		}
		ar := addedRepo{label: act.Label}
		for _, u := range src.URIs {
			ar.hosts = append(ar.hosts, aptrepo.Host(u))
		}
		fail := func(err error) { rep.Failed["Dépôt "+act.Label] = err.Error() }
		ok = true
		for path, b64 := range src.Keys {
			if !aptrepo.KeyPathOK(path) {
				continue
			}
			full := filepath.Join(AptRoot, path)
			if _, err := os.Lstat(full); err == nil {
				continue // clé déjà présente : gardée telle quelle
			}
			data, err := base64.StdEncoding.DecodeString(b64)
			if err == nil {
				err = os.MkdirAll(filepath.Dir(full), 0o755)
			}
			if err == nil {
				err = a.record(journal.SysKeyAdded, full)
			}
			if err == nil {
				err = writeExcl(full, data, 0o644)
			}
			if err != nil {
				fail(err)
				ok = false
				break
			}
			ar.keys = append(ar.keys, full)
		}
		if !ok {
			continue
		}
		name := src.File
		dst := filepath.Join(AptListDir, name)
		if _, err := os.Lstat(dst); err == nil {
			name = "bernard-" + name
			dst = filepath.Join(AptListDir, name)
		}
		from := inv.Source.Codename
		content := aptrepo.Retarget(src.Content, from, p.Target.Codename)
		if err := a.record(journal.SysRepoAdded, dst); err != nil {
			fail(err)
			continue
		}
		if err := writeExcl(dst, []byte(content), 0o644); err != nil {
			fail(err)
			continue
		}
		ar.file = dst
		out = append(out, ar)
		a.log("Dépôt ajouté : %s", act.Label)
	}
	return out
}

// didAny2 : une opération système de ce type a déjà été faite pour un
// fichier de ce nom (reprise).
func (a *Applier) didAny2(op, file string) bool {
	for _, r := range a.State.Sys {
		if r.Op == op && filepath.Base(r.Name) == file {
			return true
		}
	}
	return false
}

func writeExcl(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// aptUpdateChecked met à jour la liste des paquets et retire aussitôt un
// dépôt ajouté qui ne répond pas (version du système non prise en charge,
// dépôt fermé) : aucune source cassée n'est laissée sur la cible.
func (a *Applier) aptUpdateChecked(ctx context.Context, added []addedRepo, rep *Report) {
	out, err := a.Sys.Exec(ctx, sysexec.Cmd{Name: "apt-get", Args: []string{"update"}, Combined: true,
		Env: []string{"DEBIAN_FRONTEND=noninteractive"}})
	if len(added) == 0 {
		if err != nil {
			a.log("  attention : %v", err)
		}
		return
	}
	removed := false
	for _, r := range added {
		if !repoFailed(out, r.hosts) {
			continue
		}
		os.Remove(r.file)
		for _, k := range r.keys {
			os.Remove(k)
		}
		rep.Failed["Dépôt "+r.label] = "injoignable depuis cet ordinateur (version du système non prise en charge ?) : retiré"
		removed = true
	}
	if removed {
		a.Sys.Exec(ctx, sysexec.Cmd{Name: "apt-get", Args: []string{"update"}, Env: []string{"DEBIAN_FRONTEND=noninteractive"}})
	}
}

// repoFailed cherche, dans la sortie d'apt-get update, une erreur visant
// l'un des hôtes.
func repoFailed(out string, hosts []string) bool {
	for _, l := range strings.Split(out, "\n") {
		if !(strings.HasPrefix(l, "Err:") || strings.HasPrefix(l, "E:") || strings.HasPrefix(l, "W: GPG error")) {
			continue
		}
		for _, h := range hosts {
			if h != "" && strings.Contains(l, h) {
				return true
			}
		}
	}
	return false
}

// removeAdded retire un fichier de dépôt ou une clé ajouté par Bernard.
func removeAdded(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
