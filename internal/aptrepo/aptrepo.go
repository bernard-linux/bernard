// Package aptrepo lit et reprend les dépôts de logiciels ajoutés à la main
// (Brave, Chrome, VS Code, Docker, PPA…) et leurs clés de signature.
//
// Sans eux, les logiciels installés depuis ces dépôts sont introuvables sur
// le nouvel ordinateur. Seuls sont repris les dépôts que la cible ne connaît
// pas encore ; un dépôt qui ne répond pas sur la cible (version du système
// non prise en charge…) est retiré aussitôt, pour ne jamais laisser de
// source cassée derrière soi.
package aptrepo

import (
	"bufio"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Source est un fichier de dépôts de /etc/apt/sources.list.d.
type Source struct {
	File    string            `json:"file"`    // nom du fichier
	Content string            `json:"content"` // contenu
	URIs    []string          `json:"uris"`    // adresses normalisées
	Suites  []string          `json:"suites,omitempty"`
	Keys    map[string]string `json:"keys,omitempty"` // chemin absolu de la clé → contenu en base64
}

// Norm normalise une adresse de dépôt pour comparer.
func Norm(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	return strings.TrimRight(u, "/")
}

var optsRe = regexp.MustCompile(`^\[([^\]]*)\]\s*`)

// entries renvoie adresses, suites et clés (signed-by) d'un fichier, au
// format une ligne (.list) ou deb822 (.sources).
func entries(content string, deb822 bool) (uris, suites, keys []string) {
	if deb822 {
		sc := bufio.NewScanner(strings.NewReader(content))
		enabled := true
		var bu, bs, bk []string
		flush := func() {
			if enabled {
				uris, suites, keys = append(uris, bu...), append(suites, bs...), append(keys, bk...)
			}
			bu, bs, bk, enabled = nil, nil, nil, true
		}
		for sc.Scan() {
			l := sc.Text()
			t := strings.TrimSpace(l)
			if t == "" {
				flush()
				continue
			}
			if strings.HasPrefix(t, "#") || strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") {
				continue
			}
			k, v, ok := strings.Cut(t, ":")
			if !ok {
				continue
			}
			v = strings.TrimSpace(v)
			switch strings.ToLower(strings.TrimSpace(k)) {
			case "uris":
				bu = append(bu, strings.Fields(v)...)
			case "suites":
				bs = append(bs, strings.Fields(v)...)
			case "signed-by":
				if strings.HasPrefix(v, "/") {
					bk = append(bk, strings.Fields(v)...)
				}
			case "enabled":
				enabled = strings.ToLower(v) != "no"
			}
		}
		flush()
		return
	}
	for _, l := range strings.Split(content, "\n") {
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "deb ") && !strings.HasPrefix(t, "deb\t") {
			continue
		}
		t = strings.TrimSpace(t[3:])
		if m := optsRe.FindStringSubmatch(t); m != nil {
			for _, o := range strings.Fields(m[1]) {
				if v, ok := strings.CutPrefix(o, "signed-by="); ok {
					keys = append(keys, v)
				}
			}
			t = t[len(m[0]):]
		}
		f := strings.Fields(t)
		if len(f) >= 2 {
			uris, suites = append(uris, f[0]), append(suites, f[1])
		}
	}
	return
}

// Emplacements de clés acceptés, à la lecture comme à l'écriture.
var keyDirs = []string{"/etc/apt/keyrings/", "/usr/share/keyrings/", "/etc/apt/trusted.gpg.d/"}

// KeyPathOK vérifie un chemin de clé.
func KeyPathOK(p string) bool {
	if filepath.Clean(p) != p || strings.Contains(p, "..") {
		return false
	}
	for _, d := range keyDirs {
		if strings.HasPrefix(p, d) && len(p) > len(d) && !strings.Contains(p[len(d):], "/") {
			return true
		}
	}
	return false
}

// FileNameOK vérifie un nom de fichier de dépôt.
var fileRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+\.(list|sources)$`)

func FileNameOK(n string) bool { return fileRe.MatchString(n) }

// Read lit les fichiers de dépôts de root/etc/apt/sources.list.d, avec
// leurs clés. Lecture seule.
func Read(root string) []Source {
	files, _ := filepath.Glob(filepath.Join(root, "etc/apt/sources.list.d/*"))
	sort.Strings(files)
	var out []Source
	for _, f := range files {
		name := filepath.Base(f)
		if !FileNameOK(name) {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		uris, suites, keys := entries(string(b), strings.HasSuffix(name, ".sources"))
		if len(uris) == 0 {
			continue
		}
		src := Source{File: name, Content: string(b), Suites: suites}
		for _, u := range uris {
			src.URIs = append(src.URIs, Norm(u))
		}
		for _, k := range keys {
			if !KeyPathOK(k) {
				continue
			}
			if kb, err := os.ReadFile(filepath.Join(root, k)); err == nil {
				if src.Keys == nil {
					src.Keys = map[string]string{}
				}
				src.Keys[k] = base64.StdEncoding.EncodeToString(kb)
			}
		}
		out = append(out, src)
	}
	return out
}

// Known renvoie les adresses de dépôts configurées sous root (fichier
// principal et sources.list.d).
func Known(root string) map[string]bool {
	set := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join(root, "etc/apt/sources.list.d/*"))
	files = append(files, filepath.Join(root, "etc/apt/sources.list"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		uris, _, _ := entries(string(b), strings.HasSuffix(f, ".sources"))
		for _, u := range uris {
			set[Norm(u)] = true
		}
	}
	return set
}

// Host renvoie le nom d'hôte d'une adresse normalisée (pour l'affichage).
func Host(norm string) string {
	h, _, _ := strings.Cut(norm, "/")
	return h
}

// Retarget remplace, dans le contenu d'un fichier, le nom de code de la
// source par celui de la cible (ex. jammy → noble) : les PPA publient par
// version du système.
func Retarget(content, from, to string) string {
	if from == "" || to == "" || from == to {
		return content
	}
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(from) + `(-updates|-security|-backports|-proposed)?\b`)
	return re.ReplaceAllString(content, to+"$1")
}

// Origins lit la sortie de « apt-cache policy p1 p2… » et renvoie, pour
// chaque paquet, l'adresse du dépôt d'où vient la version installée (vide si
// elle ne vient que de dpkg).
func Origins(policy string) map[string]string {
	out := map[string]string{}
	var pkg string
	inInstalled := false
	for _, l := range strings.Split(policy, "\n") {
		if l != "" && !strings.HasPrefix(l, " ") && strings.HasSuffix(l, ":") {
			pkg, inInstalled = strings.TrimSuffix(l, ":"), false
			continue
		}
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "*** "):
			inInstalled = true
		case strings.HasPrefix(l, "     ") && !strings.HasPrefix(l, "        ") && t != "":
			inInstalled = false // version suivante du tableau
		case inInstalled && pkg != "" && out[pkg] == "":
			f := strings.Fields(t)
			if len(f) >= 2 && strings.Contains(f[1], "://") {
				out[pkg] = Norm(f[1])
			}
		}
	}
	return out
}
