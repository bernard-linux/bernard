package ui

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestWebCatalogComplete : chaque texte passé à T("…") dans app.js a sa
// traduction dans en.js, avec les mêmes {0}, {1}…
func TestWebCatalogComplete(t *testing.T) {
	js, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := os.ReadFile("web/en.js")
	if err != nil {
		t.Fatal(err)
	}
	body := string(cat)
	i, j := strings.Index(body, "= {")+2, strings.LastIndex(body, "}")
	if i < 2 || j < i {
		t.Fatal("en.js : objet introuvable")
	}
	obj := body[i : j+1]
	// Tolère une virgule finale et les commentaires de ligne.
	obj = regexp.MustCompile(`(?m)^\s*//.*$`).ReplaceAllString(obj, "")
	obj = regexp.MustCompile(`,\s*}$`).ReplaceAllString(obj, "}")
	en := map[string]string{}
	if err := json.Unmarshal([]byte(obj), &en); err != nil {
		t.Fatalf("en.js n'est pas un objet JSON valide : %v", err)
	}
	call := regexp.MustCompile(`\bT\(\s*"((?:[^"\\]|\\.)*)"`)
	ph := regexp.MustCompile(`\{\d+\}`)
	for _, m := range call.FindAllStringSubmatch(string(js), -1) {
		var key string
		if err := json.Unmarshal([]byte(`"`+m[1]+`"`), &key); err != nil {
			t.Errorf("texte illisible : %s", m[1])
			continue
		}
		tr, ok := en[key]
		if !ok {
			t.Errorf("traduction manquante : %q", key)
			continue
		}
		a, b := ph.FindAllString(key, -1), ph.FindAllString(tr, -1)
		sort.Strings(a)
		sort.Strings(b)
		if strings.Join(a, ",") != strings.Join(b, ",") {
			t.Errorf("paramètres différents : %q → %q", key, tr)
		}
	}
}
