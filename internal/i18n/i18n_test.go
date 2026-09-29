package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	cases := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"LANG": "fr_BE.UTF-8"}, FR},
		{map[string]string{"LANG": "en_US.UTF-8"}, EN},
		{map[string]string{"LANGUAGE": "de_DE:fr", "LANG": "fr_FR.UTF-8"}, EN},
		{map[string]string{"LC_ALL": "C", "LANG": "nl_BE.UTF-8"}, EN},
	}
	for _, c := range cases {
		if got := Detect(env(c.env), t.TempDir()); got != c.want {
			t.Errorf("%v : %s, attendu %s", c.env, got, c.want)
		}
	}
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "etc/default"), 0o755)
	os.WriteFile(filepath.Join(root, "etc/default/locale"), []byte("LANG=\"en_GB.UTF-8\"\n"), 0o644)
	if Detect(nil, root) != EN {
		t.Error("/etc/default/locale ignoré")
	}
	if Detect(nil, t.TempDir()) != FR {
		t.Error("par défaut : français")
	}
}

func TestTranslate(t *testing.T) {
	add(map[string]string{"fichier %s copié": "file %s copied"})
	defer Set(FR)
	if Tf("fichier %s copié", "a") != "fichier a copié" {
		t.Error("français")
	}
	Set("en_US")
	if Tf("fichier %s copié", "a") != "file a copied" || T("absent") != "absent" {
		t.Error("anglais")
	}
	if Plural(0, "fichier", "fichiers") != "fichiers" {
		t.Error("pluriel anglais de 0")
	}
}

var verbRe = regexp.MustCompile(`%[-+# 0]*[0-9]*(?:\.[0-9]+)?[a-zA-Z%]`)

func verbs(s string) string { return strings.Join(verbRe.FindAllString(s, -1), " ") }

// TestCatalogComplete : chaque texte passé à T, Tf, Errorf ou Plural dans le
// code a sa traduction anglaise, avec les mêmes paramètres dans le même
// ordre.
func TestCatalogComplete(t *testing.T) {
	root := "../.."
	seen := map[string]string{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.Contains(p, "internal/i18n/") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Errorf("%s : %v", p, err)
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "i18n" {
				return true
			}
			var args []ast.Expr
			switch sel.Sel.Name {
			case "T", "Tf", "Errorf", "N", "NewError":
				if len(call.Args) > 0 {
					args = call.Args[:1]
				}
			case "Plural":
				if len(call.Args) == 3 {
					args = call.Args[1:]
				}
			}
			for _, a := range args {
				lit, ok := a.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue // texte dynamique : traduit s'il est au catalogue
				}
				s, _ := strconv.Unquote(lit.Value)
				seen[s] = fset.Position(lit.Pos()).String()
			}
			return true
		})
		return nil
	})
	for s, where := range seen {
		e, ok := en[s]
		if !ok {
			t.Errorf("%s : traduction manquante pour %q", where, s)
			continue
		}
		if verbs(e) != verbs(s) {
			t.Errorf("%s : paramètres différents : %q → %q", where, s, e)
		}
		if strings.TrimSpace(e) == "" {
			t.Errorf("%s : traduction vide pour %q", where, s)
		}
	}
	if len(seen) == 0 {
		t.Log("aucun texte à traduire trouvé")
	}
}
