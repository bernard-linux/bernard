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

	"github.com/bernard-linux/bernard/internal/i18n"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/journal"
	"github.com/bernard-linux/bernard/internal/plan"
	"github.com/bernard-linux/bernard/internal/settings"
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

// BilanName est le nom du fichier déposé dans Documents (en français ;
// voir bilanName pour la langue en cours).
const BilanName = "Bilan de la migration (Bernard).html"

// BilanNameEN est le nom du fichier quand Bernard parle anglais.
const BilanNameEN = "Migration report (Bernard).html"

// bilanName renvoie le nom du fichier dans la langue en cours.
func bilanName() string {
	if i18n.Lang() == i18n.EN {
		return BilanNameEN
	}
	return BilanName
}

// bilanMaxLines borne les listes de fichiers (erreurs, renommés).
const bilanMaxLines = 300

// bilanText regroupe les textes fixes de la page, traduits au moment de
// l'écrire (le modèle HTML est préparé avant le choix de la langue).
type bilanText struct {
	Lang, Title, Print, Headline, Done, Failed, Manual, Checks string
	Errors, Renamed, RenamedNote, More, Undo, Report, Sep      string
}

type bilanData struct {
	T               bilanText
	Meta            string
	OK              bool
	DoneLines       []string
	Failed, Manual  []kv
	Errors, Renamed []string
	Checks          []string
	Journal         string
}

type kv struct{ K, V string }

func sortedKV(m map[string]string) []kv {
	// Préfixe « non appliqué : » des réglages volontairement laissés de côté
	// (dans les deux langues) : superflu ici, la section le dit déjà.
	skip := settings.ErrSkipped.Error()
	var out []kv
	for k, v := range m {
		for _, pre := range []string{"non appliqué : ", skip + " : ", skip + ": "} {
			v = strings.TrimPrefix(v, pre)
		}
		out = append(out, kv{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}

// Unités de taille (traduites à l'affichage).
var byteUnits = []string{i18n.N("octets"), i18n.N("Ko"), i18n.N("Mo"), i18n.N("Go"), i18n.N("To")}

func humanBytes(n int64) string {
	f, i := float64(n), 0
	for f >= 1000 && i < len(byteUnits)-1 {
		f /= 1000
		i++
	}
	if i == 0 {
		return strconv.FormatInt(n, 10) + " " + i18n.T(byteUnits[0])
	}
	num := strconv.FormatFloat(f, 'f', 1, 64)
	if i18n.Lang() != i18n.EN {
		num = strings.Replace(num, ".", ",", 1)
	}
	return num + " " + i18n.T(byteUnits[i])
}

// checks : vérifications conseillées, selon ce qui a été migré.
func checks(p *plan.Plan, inv *inventory.Inventory) []string {
	out := []string{i18n.T("Ouvrez quelques documents, photos et fichiers récents : ils doivent s'ouvrir normalement.")}
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
					out = append(out, i18n.T("Ouvrez Thunderbird : vos comptes et courriels doivent être là."))
				} else {
					out = append(out, i18n.Tf("Ouvrez %s : favoris, mots de passe enregistrés et sessions des sites.", v))
				}
			}
		}
		if strings.Contains(name, "steam") {
			steam = true
		}
		switch a.Op {
		case plan.OpImportWifi:
			out = append(out, i18n.Tf("Connectez-vous au Wi-Fi « %s » sans retaper le mot de passe.", a.Label))
		case plan.OpImportVPN:
			out = append(out, i18n.Tf("Établissez la connexion VPN « %s ».", a.Label))
		case plan.OpAddPrinter:
			out = append(out, i18n.Tf("Imprimez une page de test sur « %s ».", a.Label))
		case plan.OpSystemData:
			out = append(out, i18n.Tf("Vérifiez « %s » (%s).", a.Label, a.To))
		case plan.OpAttachDisk:
			out = append(out, i18n.Tf("Vérifiez que le disque « %s » est bien visible dans %s.", a.Label, a.To))
		}
	}
	if steam {
		out = append(out, i18n.T("Lancez Steam et un de vos jeux : ils ne doivent pas être retéléchargés."))
	}
	out = append(out, i18n.T("Gardez l'ancien ordinateur intact quelques semaines, le temps d'être sûr que rien ne manque."))
	return out
}

func bilanHTML(res *Result, s *Session, now time.Time) ([]byte, error) {
	d := bilanData{OK: res.OK(), Journal: res.JournalPath}
	d.Meta = now.Format(i18n.T("02/01/2006 à 15:04"))
	if s.Inv != nil {
		if src := strings.TrimSpace(s.Inv.Source.Hostname + " — " + s.Inv.Source.Distro + " " + s.Inv.Source.Version); src != "" {
			d.Meta += " · " + i18n.Tf("depuis %s", src)
		}
	}
	if s.Plan != nil {
		if tgt := strings.TrimSpace(s.Plan.Target.Distro + " " + s.Plan.Target.Version); tgt != "" {
			d.Meta += " · " + i18n.Tf("vers %s", tgt)
		}
		d.Checks = checks(s.Plan, s.Inv)
	}
	var files, links, vol int64
	more := 0
	var ids []string
	for id := range res.Data {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	add := func(list *[]string, line string) {
		if len(*list) < bilanMaxLines {
			*list = append(*list, line)
		} else {
			more++
		}
	}
	for _, id := range ids {
		r := res.Data[id]
		files += r.Files + r.AlreadyPresent
		links += r.Links
		vol += r.Bytes
		for _, e := range r.Errors {
			add(&d.Errors, i18n.Tf("%s : %s", e.Path, e.Err))
		}
		for _, f := range r.Renamed {
			add(&d.Renamed, f.Dst)
		}
		if !r.OK() {
			d.OK = false
		}
	}
	line := i18n.Tf("%d %s (%s copiés)", files, i18n.Plural(files, "fichier vérifié", "fichiers vérifiés"), humanBytes(vol))
	if links > 0 {
		line += i18n.Tf(", dont %d %s", links, i18n.Plural(links, "lien dur recréé", "liens durs recréés"))
	}
	d.DoneLines = append(d.DoneLines, line)
	list := func(l []string) string { return strings.Join(l, ", ") }
	if res.System != nil {
		for _, u := range res.System.UsersCreated {
			d.DoneLines = append(d.DoneLines, i18n.Tf("Compte %s créé", u))
		}
		if len(res.System.Installed) > 0 {
			d.DoneLines = append(d.DoneLines, i18n.Tf("Applications installées : %s", list(res.System.Installed)))
		}
		if len(res.System.Removed) > 0 {
			d.DoneLines = append(d.DoneLines, i18n.Tf("Retirées, comme sur l'ancien ordinateur : %s", list(res.System.Removed)))
		}
		d.Failed = sortedKV(res.System.Failed)
	}
	if res.Settings != nil {
		d.DoneLines = append(d.DoneLines, res.Settings.Applied...)
		d.Failed = append(d.Failed, sortedKV(res.Settings.Failed)...)
		d.Manual = sortedKV(res.Settings.Skipped)
	}
	if len(d.Failed) > 0 {
		d.OK = false
	}

	d.T = bilanText{
		Lang:        i18n.Lang(),
		Title:       i18n.T("Bilan de la migration"),
		Print:       i18n.T("Imprimer ou enregistrer en PDF"),
		Done:        i18n.T("Ce qui a été fait"),
		Failed:      i18n.T("Échecs"),
		Manual:      i18n.T("À faire à la main"),
		Checks:      i18n.T("Vérifications conseillées"),
		Errors:      i18n.T("Fichiers non copiés"),
		Renamed:     i18n.T("Fichiers renommés"),
		RenamedNote: i18n.T("Un fichier du même nom existait déjà ici ; il a été gardé et la copie a reçu un nouveau nom."),
		Undo:        i18n.T("Si quelque chose ne vous convient pas, relancez Bernard : « Annuler la migration » remet cet ordinateur dans son état d'avant (les fichiers modifiés depuis sont gardés)."),
		Report:      i18n.T("Rapport technique :"),
		Sep:         i18n.T(" : "),
	}
	if d.OK {
		d.T.Headline = i18n.T("Tout est arrivé : chaque fichier a été vérifié.")
	} else {
		d.T.Headline = i18n.T("Migration terminée ; quelques éléments sont à revoir (voir plus bas).")
	}
	if more > 0 {
		d.T.More = i18n.Tf("… et %d %s, dans le rapport technique.", more, i18n.Plural(int64(more), "autre ligne", "autres lignes"))
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
		path := filepath.Join(dir, bilanName())
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
<html lang="{{.T.Lang}}"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.T.Title}}</title>
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
<button class="print" onclick="window.print()">{{.T.Print}}</button>
<h1>{{.T.Title}}</h1>
<p class="meta">{{.Meta}}</p>
<p class="{{if .OK}}ok{{else}}bad{{end}}"><strong>{{.T.Headline}}</strong></p>

<h2>{{.T.Done}}</h2>
<ul>
{{range .DoneLines}}<li>{{.}}</li>{{end}}
</ul>

{{if .Failed}}<h2 class="bad">{{.T.Failed}}</h2><ul>{{range .Failed}}<li><strong>{{.K}}</strong>{{$.T.Sep}}{{.V}}</li>{{end}}</ul>{{end}}
{{if .Manual}}<h2>{{.T.Manual}}</h2><ul>{{range .Manual}}<li><strong>{{.K}}</strong>{{$.T.Sep}}{{.V}}</li>{{end}}</ul>{{end}}

<h2>{{.T.Checks}}</h2>
<ul class="check">{{range .Checks}}<li>{{.}}</li>{{end}}</ul>

{{if .Errors}}<h2 class="bad">{{.T.Errors}}</h2><ul>{{range .Errors}}<li><code>{{.}}</code></li>{{end}}</ul>{{end}}
{{if .Renamed}}<h2>{{.T.Renamed}}</h2><p class="meta">{{.T.RenamedNote}}</p><ul>{{range .Renamed}}<li><code>{{.}}</code></li>{{end}}</ul>{{end}}
{{if .T.More}}<p class="meta">{{.T.More}}</p>{{end}}

<p class="meta">{{.T.Undo}}<br>
{{.T.Report}} <code>{{.Journal}}</code></p>
</body></html>
`))
