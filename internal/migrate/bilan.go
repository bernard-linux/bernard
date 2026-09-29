package migrate

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/journal"
	"github.com/bernard-linux/bernard/internal/plan"
	"github.com/bernard-linux/bernard/internal/system"
	"github.com/bernard-linux/bernard/internal/transfer"
)

// Bilan lisible de la migration.
//
// Le rapport technique (rapport.json) reste dans le dossier de Bernard. Le
// bilan est une page claire, rangée dans le dossier Documents de chaque
// compte migré : ce qui est arrivé, ce qui reste à faire à la main, et une
// courte liste de vérifications. Elle s'ouvre dans le navigateur et
// s'imprime (ou s'enregistre en PDF) depuis celui-ci.

// BilanName est le nom du fichier déposé dans Documents.
const BilanName = "Bilan de la migration (Bernard).html"

// bilanMaxLines borne les listes de fichiers (erreurs, renommés).
const bilanMaxLines = 300

type bilanData struct {
	Date, Source, Target string
	OK                   bool
	Files                int64
	Bytes                string
	Links                int64
	Users, Installed     []string
	Removed, Applied     []string
	Failed, Manual       []kv
	Errors, Renamed      []string
	More                 int
	Checks               []string
	Journal              string
}

type kv struct{ K, V string }

func sortedKV(m map[string]string) []kv {
	var out []kv
	for k, v := range m {
		out = append(out, kv{k, strings.TrimPrefix(v, "non appliqué : ")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}

func humanBytes(n int64) string {
	units := []string{"octets", "Ko", "Mo", "Go", "To"}
	f, i := float64(n), 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	if i == 0 {
		return strconv.FormatInt(n, 10) + " octets"
	}
	return strings.Replace(strconv.FormatFloat(f, 'f', 1, 64), ".", ",", 1) + " " + units[i]
}

// checks : vérifications conseillées, selon ce qui a été migré.
func checks(p *plan.Plan, inv *inventory.Inventory) []string {
	out := []string{"Ouvrez quelques documents, photos et fichiers récents : ils doivent s'ouvrir normalement."}
	browsers := map[string]string{"firefox": "Firefox", "google-chrome": "Google Chrome", "chrome": "Google Chrome",
		"brave": "Brave", "chromium": "Chromium", "vivaldi": "Vivaldi", "opera": "Opera", "thunderbird": "Thunderbird"}
	seen := map[string]bool{}
	steam := false
	for _, a := range p.Actions {
		if !a.Selected {
			continue
		}
		name := strings.ToLower(a.Label + " " + a.Package)
		for k, v := range browsers {
			if strings.Contains(name, k) && !seen[v] {
				seen[v] = true
				if v == "Thunderbird" {
					out = append(out, "Ouvrez Thunderbird : vos comptes et courriels doivent être là.")
				} else {
					out = append(out, "Ouvrez "+v+" : favoris, mots de passe enregistrés et sessions des sites.")
				}
			}
		}
		if strings.Contains(name, "steam") {
			steam = true
		}
		switch a.Op {
		case plan.OpImportWifi:
			out = append(out, "Connectez-vous au Wi-Fi « "+a.Label+" » sans retaper le mot de passe.")
		case plan.OpImportVPN:
			out = append(out, "Établissez la connexion VPN « "+a.Label+" ».")
		case plan.OpAddPrinter:
			out = append(out, "Imprimez une page de test sur « "+a.Label+" ».")
		case plan.OpSystemData:
			out = append(out, "Vérifiez « "+a.Label+" » ("+a.To+").")
		case plan.OpAttachDisk:
			out = append(out, "Vérifiez que le disque « "+a.Label+" » est bien visible dans "+a.To+".")
		}
	}
	if steam {
		out = append(out, "Lancez Steam et un de vos jeux : ils ne doivent pas être retéléchargés.")
	}
	out = append(out, "Gardez l'ancien ordinateur intact quelques semaines, le temps d'être sûr que rien ne manque.")
	return out
}

func bilanHTML(res *Result, s *Session, now time.Time) ([]byte, error) {
	d := bilanData{Date: now.Format("02/01/2006 à 15:04"), OK: res.OK(), Journal: res.JournalPath}
	if s.Inv != nil {
		d.Source = strings.TrimSpace(s.Inv.Source.Hostname + " — " + s.Inv.Source.Distro + " " + s.Inv.Source.Version)
	}
	if s.Plan != nil {
		d.Target = strings.TrimSpace(s.Plan.Target.Distro + " " + s.Plan.Target.Version)
		d.Checks = checks(s.Plan, s.Inv)
	}
	var vol int64
	var ids []string
	for id := range res.Data {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	add := func(list *[]string, line string) {
		if len(*list) < bilanMaxLines {
			*list = append(*list, line)
		} else {
			d.More++
		}
	}
	for _, id := range ids {
		r := res.Data[id]
		d.Files += r.Files + r.AlreadyPresent
		d.Links += r.Links
		vol += r.Bytes
		for _, e := range r.Errors {
			add(&d.Errors, e.Path+" : "+e.Err)
		}
		for _, f := range r.Renamed {
			add(&d.Renamed, f.Dst)
		}
		if !r.OK() {
			d.OK = false
		}
	}
	d.Bytes = humanBytes(vol)
	if res.System != nil {
		d.Users, d.Installed, d.Removed = res.System.UsersCreated, res.System.Installed, res.System.Removed
		d.Failed = sortedKV(res.System.Failed)
	}
	if res.Settings != nil {
		d.Applied = res.Settings.Applied
		d.Failed = append(d.Failed, sortedKV(res.Settings.Failed)...)
		d.Manual = sortedKV(res.Settings.Skipped)
	}
	if len(d.Failed) > 0 {
		d.OK = false
	}
	var b bytes.Buffer
	err := bilanTmpl.Execute(&b, d)
	return b.Bytes(), err
}

// WriteBilan dépose le bilan dans le dossier Documents de chaque compte
// migré, au nom de ce compte. Chaque fichier est consigné au journal :
// l'annulation le retire s'il n'a pas été modifié.
func WriteBilan(res *Result, s *Session, j *journal.Journal) []string {
	body, err := bilanHTML(res, s, time.Now())
	if err != nil || s.Plan == nil {
		return nil
	}
	done := map[string]bool{}
	var out []string
	for _, a := range s.Plan.Actions {
		if !a.Selected || a.Op != plan.OpCopy || a.Login == "" || done[a.Login] {
			continue
		}
		done[a.Login] = true
		uid, gid, home, err := system.Owner(a.Login)
		if err != nil {
			continue
		}
		dir := filepath.Join(home, userDir(home, "DOCUMENTS", "Documents"))
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			continue
		}
		path := filepath.Join(dir, BilanName)
		tmp := path + ".bernard-tmp"
		if err := os.WriteFile(tmp, body, 0o644); err != nil {
			continue
		}
		os.Lchown(tmp, uid, gid)
		if err := os.Rename(tmp, path); err != nil {
			os.Remove(tmp)
			continue
		}
		h, n, _ := transfer.HashFile(path)
		if j != nil {
			j.Append(journal.Record{T: journal.RecDone, Key: "bilan/" + a.Login, Dst: path, Hash: h, Size: n, Status: string(transfer.StatusCopied)})
		}
		out = append(out, path)
	}
	return out
}

// userDir lit le nom du dossier Documents dans user-dirs.dirs (« Documents »
// en français comme en anglais, mais personnalisable).
func userDir(home, key, def string) string {
	b, err := os.ReadFile(filepath.Join(home, ".config/user-dirs.dirs"))
	if err != nil {
		return def
	}
	for _, l := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), "XDG_"+key+"_DIR="); ok {
			v = strings.Trim(v, `"`)
			if rel, ok := strings.CutPrefix(v, "$HOME/"); ok && filepath.IsLocal(rel) {
				return rel
			}
		}
	}
	return def
}

var bilanTmpl = template.Must(template.New("bilan").Parse(`<!doctype html>
<html lang="fr"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Bilan de la migration</title>
<style>
:root{--ink:#1d2433;--muted:#5b6475;--line:#dfe3ea;--ok:#1f7a4d;--bad:#b3261e;--bg:#fff}
body{font:15px/1.5 system-ui,-apple-system,"Segoe UI",Cantarell,sans-serif;color:var(--ink);background:var(--bg);max-width:52rem;margin:2rem auto;padding:0 1rem}
h1{font-size:1.6rem;margin:0 0 .25rem}h2{font-size:1.1rem;margin:1.8rem 0 .5rem;border-bottom:1px solid var(--line);padding-bottom:.25rem}
.meta{color:var(--muted)}.ok{color:var(--ok)}.bad{color:var(--bad)}
ul{padding-left:1.2rem}li{margin:.15rem 0}
ul.check{list-style:none;padding-left:0}ul.check li::before{content:"☐ ";font-size:1.1em}
code{font-size:.85em;word-break:break-all}
.print{float:right;font:inherit;padding:.4rem .9rem;border:1px solid var(--line);border-radius:6px;background:#f5f7fa;cursor:pointer}
@media print{.print{display:none}body{margin:0;max-width:none}h2{break-after:avoid}}
</style></head><body>
<button class="print" onclick="window.print()">Imprimer ou enregistrer en PDF</button>
<h1>Bilan de la migration</h1>
<p class="meta">{{.Date}}{{if .Source}} · depuis {{.Source}}{{end}}{{if .Target}} · vers {{.Target}}{{end}}</p>
<p class="{{if .OK}}ok{{else}}bad{{end}}"><strong>{{if .OK}}Tout est arrivé : chaque fichier a été vérifié.{{else}}Migration terminée ; quelques éléments sont à revoir (voir plus bas).{{end}}</strong></p>

<h2>Ce qui a été fait</h2>
<ul>
<li>{{.Files}} fichiers vérifiés ({{.Bytes}} copiés){{if .Links}}, dont {{.Links}} liens durs recréés{{end}}</li>
{{range .Users}}<li>Compte {{.}} créé</li>{{end}}
{{if .Installed}}<li>Applications installées : {{range $i, $a := .Installed}}{{if $i}}, {{end}}{{$a}}{{end}}</li>{{end}}
{{if .Removed}}<li>Retirées, comme sur l'ancien ordinateur : {{range $i, $a := .Removed}}{{if $i}}, {{end}}{{$a}}{{end}}</li>{{end}}
{{range .Applied}}<li>{{.}}</li>{{end}}
</ul>

{{if .Failed}}<h2 class="bad">Échecs</h2><ul>{{range .Failed}}<li><strong>{{.K}}</strong> : {{.V}}</li>{{end}}</ul>{{end}}
{{if .Manual}}<h2>À faire à la main</h2><ul>{{range .Manual}}<li><strong>{{.K}}</strong> : {{.V}}</li>{{end}}</ul>{{end}}

<h2>Vérifications conseillées</h2>
<ul class="check">{{range .Checks}}<li>{{.}}</li>{{end}}</ul>

{{if .Errors}}<h2 class="bad">Fichiers non copiés</h2><ul>{{range .Errors}}<li><code>{{.}}</code></li>{{end}}</ul>{{end}}
{{if .Renamed}}<h2>Fichiers renommés</h2><p class="meta">Un fichier du même nom existait déjà ici ; il a été gardé et la copie a reçu un nouveau nom.</p><ul>{{range .Renamed}}<li><code>{{.}}</code></li>{{end}}</ul>{{end}}
{{if .More}}<p class="meta">… et {{.More}} autres lignes, dans le rapport technique.</p>{{end}}

<p class="meta">Si quelque chose ne vous convient pas, relancez Bernard : « Annuler la migration » remet cet ordinateur dans son état d'avant (les fichiers modifiés depuis sont gardés).<br>
Rapport technique : <code>{{.Journal}}</code></p>
</body></html>
`))
