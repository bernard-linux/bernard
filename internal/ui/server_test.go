package ui

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestAPIGuards(t *testing.T) {
	srv, err := NewServer(NewController())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.ln.Close()
	h := srv.routes()
	host := "127.0.0.1:" + strconv.Itoa(srv.ln.Addr().(*net.TCPAddr).Port)

	do := func(method, path, host, token string) int {
		r := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		r.Host = host
		if token != "" {
			r.Header.Set("X-Bernard-Token", token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	cases := []struct {
		name, method, path, host, token string
		want                            int
	}{
		{"page publique", "GET", "/", host, "", 200},
		{"API sans jeton", "GET", "/api/state", host, "", http.StatusUnauthorized},
		{"API mauvais jeton", "GET", "/api/state", host, "faux", http.StatusUnauthorized},
		{"DNS rebinding", "GET", "/api/state", "attaquant.example:80", srv.Token, http.StatusForbidden},
		{"API correcte", "GET", "/api/state", host, srv.Token, 200},
		{"action en GET", "GET", "/api/quit", host, srv.Token, http.StatusMethodNotAllowed},
		{"jeton dans l'URL hors flux", "GET", "/api/state?t=" + srv.Token, host, "", http.StatusUnauthorized},
	}
	for _, c := range cases {
		if got := do(c.method, c.path, c.host, c.token); got != c.want {
			t.Errorf("%s : code %d, attendu %d", c.name, got, c.want)
		}
	}
}

func TestSubmitRefusedWithoutPlan(t *testing.T) {
	c := NewController()
	if err := c.Submit(map[string]bool{}, nil); err == nil {
		t.Fatal("aucun plan : la validation aurait dû être refusée")
	}
}
