// Package i18n traduit les textes montrés à l'utilisateur.
//
// Le français est la langue de référence : chaque texte est écrit en
// français dans le code, et sert de clé au catalogue anglais (fichiers
// en_*.go). Bernard parle français si le système est en français, anglais
// sinon. Un texte absent du catalogue reste en français (un test vérifie
// qu'il n'en manque aucun).
package i18n

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Langues prises en charge.
const (
	FR = "fr"
	EN = "en"
)

var (
	mu   sync.RWMutex
	lang = FR
	en   = map[string]string{}
)

// add enregistre une partie du catalogue anglais (appelée par les init des
// fichiers en_*.go).
func add(m map[string]string) {
	mu.Lock()
	defer mu.Unlock()
	for k, v := range m {
		en[k] = v
	}
}

// Set choisit la langue (« fr » ou « en » ; toute autre valeur donne
// l'anglais, sauf une valeur vide qui garde le français).
func Set(l string) {
	mu.Lock()
	defer mu.Unlock()
	switch {
	case l == "":
	case strings.HasPrefix(strings.ToLower(l), "fr"):
		lang = FR
	default:
		lang = EN
	}
}

// Lang renvoie la langue en cours.
func Lang() string {
	mu.RLock()
	defer mu.RUnlock()
	return lang
}

// T traduit un texte.
func T(s string) string {
	mu.RLock()
	defer mu.RUnlock()
	if lang == EN {
		if e, ok := en[s]; ok {
			return e
		}
	}
	return s
}

// Tf traduit un format puis le remplit, comme fmt.Sprintf.
func Tf(format string, a ...any) string { return fmt.Sprintf(T(format), a...) }

// Errorf traduit un format d'erreur, comme fmt.Errorf (%w compris).
func Errorf(format string, a ...any) error { return fmt.Errorf(T(format), a...) }

// N marque un texte à traduire là où il est défini (table, variable de
// paquet), sans le traduire : la langue n'est pas encore choisie à ce
// moment. Le traduire à l'affichage avec T.
func N(s string) string { return s }

// Error est une erreur dont le message est traduit à la lecture : pour les
// erreurs de paquet (var ErrX = i18n.NewError("…")), créées avant le choix
// de la langue.
type Error struct{ msg string }

func (e *Error) Error() string { return T(e.msg) }

// NewError crée une erreur traduite à la lecture.
func NewError(msg string) error { return &Error{msg} }

// Plural choisit la forme du nom selon n (en français, 0 et 1 sont au
// singulier ; en anglais, seul 1 l'est). Les deux formes sont traduites.
func Plural(n int64, one, many string) string {
	if Lang() == EN {
		if n == 1 {
			return T(one)
		}
		return T(many)
	}
	if n > 1 {
		return many
	}
	return one
}

// Detect lit la langue du système : variables LANGUAGE, LC_ALL,
// LC_MESSAGES, LANG, puis /etc/default/locale et /etc/locale.conf (le
// moteur tourne sous pkexec, qui efface l'environnement).
func Detect(getenv func(string) string, root string) string {
	if getenv != nil {
		for _, k := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
			if v := first(getenv(k)); v != "" {
				return norm(v)
			}
		}
	}
	for _, f := range []string{"etc/default/locale", "etc/locale.conf"} {
		fh, err := os.Open(filepath.Join(root, f))
		if err != nil {
			continue
		}
		vals := map[string]string{}
		sc := bufio.NewScanner(fh)
		for sc.Scan() {
			if k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "="); ok {
				vals[k] = strings.Trim(v, `"'`)
			}
		}
		fh.Close()
		for _, k := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
			if v := first(vals[k]); v != "" {
				return norm(v)
			}
		}
	}
	return FR
}

// first : première langue d'une liste « fr_BE:fr:en », en ignorant C/POSIX.
func first(v string) string {
	for _, l := range strings.Split(v, ":") {
		if l = strings.TrimSpace(l); l != "" && l != "C" && l != "POSIX" && !strings.HasPrefix(l, "C.") {
			return l
		}
	}
	return ""
}

func norm(v string) string {
	if strings.HasPrefix(strings.ToLower(v), "fr") {
		return FR
	}
	return EN
}
