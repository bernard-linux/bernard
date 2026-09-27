package ui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"time"
)

//go:embed web
var webFS embed.FS

// Server sert l'interface et l'API sur 127.0.0.1 uniquement.
//
// Protections : écoute sur la boucle locale ; jeton aléatoire exigé sur
// toute l'API (en-tête X-Bernard-Token, ou paramètre t pour le flux
// d'événements) ; en-tête Host vérifié pour déjouer le « DNS rebinding » ;
// aucune ressource externe chargée par la page.
type Server struct {
	Ctrl  *Controller
	Token string
	ln    net.Listener
	srv   *http.Server
}

// NewServer ouvre l'écoute sur un port libre de 127.0.0.1.
func NewServer(ctrl *Controller) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	tok := make([]byte, 32)
	if _, err := rand.Read(tok); err != nil {
		ln.Close()
		return nil, err
	}
	s := &Server{Ctrl: ctrl, Token: hex.EncodeToString(tok), ln: ln}
	s.srv = &http.Server{Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	return s, nil
}

// URL est l'adresse à ouvrir dans la fenêtre (le jeton y figure une fois ;
// la page le retire aussitôt de la barre d'adresse).
func (s *Server) URL() string {
	return fmt.Sprintf("http://%s/?t=%s", s.ln.Addr().String(), s.Token)
}

// Serve bloque jusqu'à Shutdown.
func (s *Server) Serve() error {
	err := s.srv.Serve(s.ln)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Shutdown arrête le serveur.
func (s *Server) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.srv.Shutdown(ctx)
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web")
	files := http.FileServer(http.FS(static))
	mux.Handle("/", securityHeaders(files))

	api := http.NewServeMux()
	api.HandleFunc("/api/state", s.handleState)
	api.HandleFunc("/api/events", s.handleEvents)
	api.HandleFunc("/api/start", s.post(s.handleStart))
	api.HandleFunc("/api/disk", s.post(s.handleDisk))
	api.HandleFunc("/api/submit", s.post(s.handleSubmit))
	api.HandleFunc("/api/stop", s.post(func(w http.ResponseWriter, r *http.Request) error { s.Ctrl.Stop(); return nil }))
	api.HandleFunc("/api/reset", s.post(func(w http.ResponseWriter, r *http.Request) error { s.Ctrl.Reset(); return nil }))
	api.HandleFunc("/api/undo", s.post(func(w http.ResponseWriter, r *http.Request) error { return s.Ctrl.UndoMigration() }))
	api.HandleFunc("/api/role", s.post(s.handleRole))
	api.HandleFunc("/api/source", s.post(s.handleSource))
	api.HandleFunc("/api/pair", s.post(s.handlePair))
	api.HandleFunc("/api/pack", s.post(s.handlePack))
	api.HandleFunc("/api/disks", s.post(func(w http.ResponseWriter, r *http.Request) error { s.Ctrl.RefreshDisks(); return nil }))
	api.HandleFunc("/api/reboot", s.post(func(w http.ResponseWriter, r *http.Request) error { return s.Ctrl.Reboot() }))
	api.HandleFunc("/api/quit", s.post(func(w http.ResponseWriter, r *http.Request) error { s.Ctrl.RequestQuit(); return nil }))
	mux.Handle("/api/", s.guard(api))
	return mux
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self' data:")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

func (s *Server) guard(h http.Handler) http.Handler {
	port := strconv.Itoa(s.ln.Addr().(*net.TCPAddr).Port)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "127.0.0.1:"+port && r.Host != "localhost:"+port {
			http.Error(w, "hôte refusé", http.StatusForbidden)
			return
		}
		tok := r.Header.Get("X-Bernard-Token")
		if tok == "" && r.URL.Path == "/api/events" {
			tok = r.URL.Query().Get("t")
		}
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.Token)) != 1 {
			http.Error(w, "jeton refusé", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

func (s *Server) post(f func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST attendu", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := f(w, r); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.Ctrl.Snapshot())
}

// handleEvents diffuse l'état complet à chaque changement (Server-Sent Events).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "flux non pris en charge", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	ch, unsub := s.Ctrl.Subscribe()
	defer unsub()
	send := func() bool {
		b, _ := json.Marshal(s.Ctrl.Snapshot())
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	if !send() {
		return
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	// Limite le débit : au plus 10 mises à jour par seconde.
	throttle := time.NewTicker(100 * time.Millisecond)
	defer throttle.Stop()
	pending := false
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			pending = true
		case <-throttle.C:
			if pending {
				pending = false
				if !send() {
					return
				}
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) error {
	var req struct{ Mode string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return err
	}
	switch req.Mode {
	case "network":
		return s.Ctrl.StartNetwork()
	case "disk":
		s.Ctrl.ShowDisk()
		return nil
	}
	return fmt.Errorf("mode inconnu : %q", req.Mode)
}

func (s *Server) handleDisk(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Path       string `json:"path"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return err
	}
	return s.Ctrl.StartDisk(req.Path, req.Passphrase)
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Selected  map[string]bool   `json:"selected"`
		Passwords map[string]string `json:"passwords"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return err
	}
	return s.Ctrl.Submit(req.Selected, req.Passwords)
}

func (s *Server) handleRole(w http.ResponseWriter, r *http.Request) error {
	var req struct{ Role string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return err
	}
	return s.Ctrl.ChooseRole(req.Role)
}

func (s *Server) handleSource(w http.ResponseWriter, r *http.Request) error {
	var req struct{ Mode string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return err
	}
	switch req.Mode {
	case "network":
		return s.Ctrl.StartSource()
	case "disk":
		return s.Ctrl.ShowSourceDisk()
	}
	return fmt.Errorf("mode inconnu : %q", req.Mode)
}

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Target string `json:"target"`
		Code   string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return err
	}
	return s.Ctrl.Pair(req.Target, req.Code)
}

func (s *Server) handlePack(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Disk       string `json:"disk"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return err
	}
	return s.Ctrl.StartPack(req.Disk, req.Passphrase)
}
