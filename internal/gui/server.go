// Package gui sert une interface locale pour enregistrer des jeux.
//
// Pourquoi une page web servie par le binaire plutôt que GTK ou Fyne : une
// vraie boîte à outils graphique impose cgo, une bibliothèque présente sur la
// machine cible, et fait passer l'AppImage de 2 Mo à plus de 30 Mo, pour une
// fenêtre qui contient une zone de texte et deux boutons. Le binaire reste
// statique et portable, et le navigateur dont l'outil a besoin de toute façon
// est déjà là.
//
// La page est servie sur 127.0.0.1 uniquement, et chaque requête doit porter
// un jeton aléatoire transmis dans l'URL. Sans lui, n'importe quel processus
// local pourrait écrire dans la bibliothèque Steam de l'utilisateur. L'en-tête
// Host est également vérifié, ce qui bloque le rebinding DNS.
package gui

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"deckwebgpu/internal/registry"
)

//go:embed assets/index.html
var assets embed.FS

// State est l'instantané affiché par l'interface.
type State struct {
	SteamRoot       string          `json:"steam_root"`
	SteamUser       uint32          `json:"steam_user"`
	SteamRunning    bool            `json:"steam_running"`
	SteamError      string          `json:"steam_error,omitempty"`
	ShortcutsPath   string          `json:"shortcuts_path"`
	ShortcutCount   int             `json:"shortcut_count"`
	BinaryInstalled bool            `json:"binary_installed"`
	BinaryPath      string          `json:"binary_path"`
	BrowserLabel    string          `json:"browser_label"`
	BrowserVersion  string          `json:"browser_version"`
	BrowserError    string          `json:"browser_error,omitempty"`
	UdevOK          bool            `json:"udev_ok"`
	UdevCommand     string          `json:"udev_command,omitempty"`
	Games           []registry.Game `json:"games"`
	Defaults        string          `json:"defaults,omitempty"`
	DataDir         string          `json:"data_dir"`
	LogsDir         string          `json:"logs_dir"`
}

// Outcome est le résultat d'une action, tel qu'affiché dans l'interface.
type Outcome struct {
	Log             string `json:"log"`
	Added           int    `json:"added"`
	Removed         int    `json:"removed"`
	Skipped         int    `json:"skipped"`
	Error           string `json:"error,omitempty"`
	NeedSteamClosed bool   `json:"need_steam_closed,omitempty"`
}

// Backend est la logique métier, fournie par le paquet main. L'interface ne
// connaît ni Steam ni Chromium : elle se contente d'appeler ces méthodes.
type Backend interface {
	State() State
	Add(text string, dryRun, force bool) Outcome
	Remove(slugs []string) Outcome
	ShutdownSteam() Outcome
	Launch(slug string) Outcome
}

// Server est le serveur HTTP local.
type Server struct {
	backend Backend
	token   string
	srv     *http.Server
	addr    string
}

// New construit le serveur. token doit être imprévisible : il est le seul
// contrôle d'accès.
func New(backend Backend, token string) *Server {
	s := &Server{backend: backend, token: token}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/state", s.json(s.handleState))
	mux.HandleFunc("/api/add", s.json(s.handleAdd))
	mux.HandleFunc("/api/remove", s.json(s.handleRemove))
	mux.HandleFunc("/api/steam/shutdown", s.json(s.handleShutdown))
	mux.HandleFunc("/api/test", s.json(s.handleTest))

	s.srv = &http.Server{Handler: mux}
	return s
}

// Listen démarre l'écoute et retourne l'URL à ouvrir, jeton inclus.
func (s *Server) Listen(addr string) (string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	s.addr = ln.Addr().String()
	go func() {
		_ = s.srv.Serve(ln)
	}()
	host, port, _ := net.SplitHostPort(s.addr)
	if host == "::" || host == "0.0.0.0" || host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%s/?t=%s", host, port, s.token), nil
}

// Addr retourne l'adresse d'écoute effective.
func (s *Server) Addr() string { return s.addr }

// Handler expose le routeur, ce qui permet de le servir depuis un
// httptest.Server dans les tests, sans ouvrir de port fixe.
func (s *Server) Handler() http.Handler { return s.srv.Handler }

// JSONHandler est un gestionnaire qui écrit du JSON.
type JSONHandler func(r *http.Request) (any, error)

// json emballe un gestionnaire : contrôle d'accès, méthode, sérialisation,
// et traduction des erreurs en réponse JSON plutôt qu'en page HTML.
func (s *Server) json(h JSONHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(w, r) {
			return
		}
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		payload, err := h(r)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(payload)
	}
}

// authorized vérifie le jeton et l'en-tête Host.
func (s *Server) authorized(w http.ResponseWriter, r *http.Request) bool {
	// Un service local peut subir une attaque par rebinding DNS : un nom de
	// domaine résolu vers 127.0.0.1 ferait passer la requête d'une page
	// hostile pour une requête locale. Exiger un Host littéral l'écarte.
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		http.Error(w, "hôte non autorisé", http.StatusForbidden)
		return false
	}

	got := r.URL.Query().Get("t")
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
		http.Error(w, "jeton invalide", http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(w, r) {
		return
	}
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	page, err := assets.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(page)
}

func (s *Server) handleState(r *http.Request) (any, error) {
	return s.backend.State(), nil
}

type addRequest struct {
	Text   string `json:"text"`
	DryRun bool   `json:"dry_run"`
	Force  bool   `json:"force"`
}

func (s *Server) handleAdd(r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		return nil, fmt.Errorf("POST attendu")
	}
	var req addRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, fmt.Errorf("requête illisible : %w", err)
	}
	if strings.TrimSpace(req.Text) == "" {
		return nil, fmt.Errorf("la liste est vide")
	}
	out := s.backend.Add(req.Text, req.DryRun, req.Force)
	// Le State est renvoyé avec le résultat pour que l'interface se rafraîchisse
	// en une seule requête au lieu de deux.
	return map[string]any{"outcome": out, "state": s.backend.State()}, nil
}

type removeRequest struct {
	Slugs []string `json:"slugs"`
}

func (s *Server) handleRemove(r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		return nil, fmt.Errorf("POST attendu")
	}
	var req removeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, fmt.Errorf("requête illisible : %w", err)
	}
	if len(req.Slugs) == 0 {
		return nil, fmt.Errorf("aucun jeu sélectionné")
	}
	out := s.backend.Remove(req.Slugs)
	return map[string]any{"outcome": out, "state": s.backend.State()}, nil
}

func (s *Server) handleShutdown(r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		return nil, fmt.Errorf("POST attendu")
	}
	out := s.backend.ShutdownSteam()
	return map[string]any{"outcome": out, "state": s.backend.State()}, nil
}

type testRequest struct {
	Slug string `json:"slug"`
}

func (s *Server) handleTest(r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		return nil, fmt.Errorf("POST attendu")
	}
	var req testRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, fmt.Errorf("requête illisible : %w", err)
	}
	out := s.backend.Launch(req.Slug)
	return map[string]any{"outcome": out, "state": s.backend.State()}, nil
}
