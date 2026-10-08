package main

import (
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"deckwebgpu/internal/gui"
	"deckwebgpu/internal/steam"
)

// Ces tests passent par HTTP, comme le navigateur : ils couvrent le chemin
// complet, du POST de la page jusqu'à l'octet écrit dans shortcuts.vdf, avec
// la même logique que le CLI.

const testAccountID = 39734272 // 76561198000000000 - 76561197960265728

// newTestEnv prépare un faux HOME Steam : userdata du compte, loginusers.vdf
// minimal, et redirection des dossiers de l'outil.
func newTestEnv(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	data := filepath.Join(base, "data")
	root := filepath.Join(base, "steam")

	if err := os.MkdirAll(filepath.Join(root, "userdata", fmt.Sprint(testAccountID), "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	login := "\"users\"\n{\n\t\"76561198000000000\"\n\t{\n\t\t\"AccountName\"\t\t\"deck\"\n\t\t\"MostRecent\"\t\t\"1\"\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(root, "config", "loginusers.vdf"), []byte(login), 0o644); err != nil {
		t.Fatal(err)
	}

	// HOME isolé : sans lui, une vraie installation Steam de la machine de
	// test pourrait être trouvée et modifiée.
	t.Setenv("HOME", base)
	t.Setenv("SWL_DATA_DIR", data)
	t.Setenv("STEAM_ROOT", root)
	return data, root
}

func newTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	tok := "jeton-de-test"
	ts := httptest.NewServer(gui.New(guiBackend{}, tok).Handler())
	t.Cleanup(ts.Close)
	return ts, tok
}

// call envoie une requête telle que le ferait la page, avec un hôte
// falsifiable pour éprouver la protection contre le rebinding DNS.
func call(t *testing.T, ts *httptest.Server, method, path, token, host, body string) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+path+"?t="+url.QueryEscape(token), r)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if host != "" {
		req.Host = host
	}
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, data
}

type actionResponse struct {
	Outcome gui.Outcome `json:"outcome"`
	State   gui.State   `json:"state"`
}

func postJSON(t *testing.T, ts *httptest.Server, token, path, body string) actionResponse {
	t.Helper()
	code, data := call(t, ts, http.MethodPost, path, token, "", body)
	if code != http.StatusOK {
		t.Fatalf("%s : code %d, corps %s", path, code, data)
	}
	var out actionResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("%s : réponse illisible (%v) : %s", path, err, data)
	}
	return out
}

// --- accès --------------------------------------------------------------

func TestGUIRefuseSansJeton(t *testing.T) {
	newTestEnv(t)
	ts, _ := newTestServer(t)

	code, _ := call(t, ts, http.MethodGet, "/api/state", "", "", "")
	if code != http.StatusForbidden {
		t.Fatalf("sans jeton : code %d, attendu 403", code)
	}

	code, _ = call(t, ts, http.MethodGet, "/", "", "", "")
	if code != http.StatusForbidden {
		t.Fatalf("page sans jeton : code %d, attendu 403", code)
	}
}

func TestGUIRefuseHoteEtranger(t *testing.T) {
	newTestEnv(t)
	ts, tok := newTestServer(t)

	// Un nom de domaine résolu vers 127.0.0.1 doit être rejeté : c'est le
	// scénario du rebinding DNS.
	code, _ := call(t, ts, http.MethodGet, "/api/state", tok, "attaquant.example.org", "")
	if code != http.StatusForbidden {
		t.Fatalf("hôte étranger : code %d, attendu 403", code)
	}
}

func TestGUIEtatInitial(t *testing.T) {
	data, root := newTestEnv(t)
	ts, tok := newTestServer(t)

	code, body := call(t, ts, http.MethodGet, "/api/state", tok, "", "")
	if code != http.StatusOK {
		t.Fatalf("code %d : %s", code, body)
	}
	var st gui.State
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatalf("réponse illisible : %v", err)
	}
	if st.SteamRoot != root {
		t.Errorf("steam_root = %q, attendu %q", st.SteamRoot, root)
	}
	if st.SteamUser != testAccountID {
		t.Errorf("steam_user = %d, attendu %d", st.SteamUser, testAccountID)
	}
	if st.SteamRunning {
		t.Error("steam_running devrait être faux")
	}
	if st.DataDir != data {
		t.Errorf("data_dir = %q, attendu %q", st.DataDir, data)
	}
	if len(st.Games) != 0 {
		t.Errorf("registre non vide : %d jeu(x)", len(st.Games))
	}
	// Sur un registre vide, l'interface propose les deux jeux d'exemple.
	if !strings.Contains(st.Defaults, "https://skate.aaddpp.lol/") ||
		!strings.Contains(st.Defaults, "https://vel.gg/bo1z") {
		t.Errorf("liste par défaut inattendue : %q", st.Defaults)
	}
	if !strings.Contains(st.Defaults, "Skate 3") || !strings.Contains(st.Defaults, "Black Ops 1 Zombies") {
		t.Errorf("noms par défaut manquants : %q", st.Defaults)
	}
}

// --- ajout --------------------------------------------------------------

func TestGUIAjoutListeParDefaut(t *testing.T) {
	_, root := newTestEnv(t)
	ts, tok := newTestServer(t)

	// La liste que l'interface propose réellement dans la zone de texte.
	liste := "Skate 3 = https://skate.aaddpp.lol/\nBlack Ops 1 Zombies = https://vel.gg/bo1z\n"
	body, _ := json.Marshal(map[string]any{"text": liste})
	res := postJSON(t, ts, tok, "/api/add", string(body))

	if res.Outcome.Error != "" {
		t.Fatalf("erreur inattendue : %s (journal : %s)", res.Outcome.Error, res.Outcome.Log)
	}
	if res.Outcome.Added != 2 {
		t.Fatalf("added = %d, attendu 2 (journal : %s)", res.Outcome.Added, res.Outcome.Log)
	}
	if len(res.State.Games) != 2 {
		t.Fatalf("état renvoyé : %d jeu(x)", len(res.State.Games))
	}

	// Relecture du fichier écrit, avec le lecteur du paquet steam.
	path := steam.ShortcutsPath(root, testAccountID)
	sf, existed, err := steam.LoadShortcuts(path)
	if err != nil {
		t.Fatalf("relecture de %s : %v", path, err)
	}
	if !existed {
		t.Fatal("shortcuts.vdf n'a pas été créé")
	}
	if sf.Count() != 2 {
		t.Fatalf("shortcuts.vdf contient %d entrée(s), attendu 2", sf.Count())
	}

	// L'appid est recalculé ici avec la formule de référence, indépendamment
	// du code de production.
	exe := filepath.Join(res.State.DataDir, "bin", "swl")
	for _, attendu := range []struct{ nom, uri string }{
		{"Skate 3", "https://skate.aaddpp.lol/"},
		{"Black Ops 1 Zombies", "https://vel.gg/bo1z"},
	} {
		want := crc32.ChecksumIEEE([]byte(exe+attendu.nom)) | 0x80000000
		if !sf.Exists(want) {
			t.Errorf("appid %d absent pour %q", want, attendu.nom)
		}
	}

	// Le registre local est cohérent : chaque jeu enregistré porte bien son URL.
	for _, g := range res.State.Games {
		if g.URL == "" || g.Slug == "" || g.AppID == 0 {
			t.Errorf("entrée incomplète dans le registre : %+v", g)
		}
	}
}

func TestGUIAjoutIdempotent(t *testing.T) {
	_, root := newTestEnv(t)
	ts, tok := newTestServer(t)

	liste := "Skate 3 = https://skate.aaddpp.lol/\n"
	body, _ := json.Marshal(map[string]any{"text": liste})

	if r := postJSON(t, ts, tok, "/api/add", string(body)); r.Outcome.Added != 1 {
		t.Fatalf("premier ajout : added = %d (journal : %s)", r.Outcome.Added, r.Outcome.Log)
	}
	// Deuxième passage avec la même URL : rien de neuf, aucun doublon.
	r := postJSON(t, ts, tok, "/api/add", string(body))
	if r.Outcome.Added != 0 || r.Outcome.Skipped != 1 {
		t.Fatalf("second ajout : added = %d, skipped = %d", r.Outcome.Added, r.Outcome.Skipped)
	}

	sf, _, err := steam.LoadShortcuts(steam.ShortcutsPath(root, testAccountID))
	if err != nil {
		t.Fatal(err)
	}
	if sf.Count() != 1 {
		t.Fatalf("shortcuts.vdf contient %d entrée(s), attendu 1", sf.Count())
	}
}

func TestGUISimulationNEcritRien(t *testing.T) {
	_, root := newTestEnv(t)
	ts, tok := newTestServer(t)

	body, _ := json.Marshal(map[string]any{
		"text":    "Skate 3 = https://skate.aaddpp.lol/\n",
		"dry_run": true,
	})
	r := postJSON(t, ts, tok, "/api/add", string(body))
	if r.Outcome.Added != 1 {
		t.Fatalf("simulation : added = %d", r.Outcome.Added)
	}
	if _, err := os.Stat(steam.ShortcutsPath(root, testAccountID)); !os.IsNotExist(err) {
		t.Fatalf("la simulation a écrit %s", steam.ShortcutsPath(root, testAccountID))
	}
	if len(r.State.Games) != 0 {
		t.Fatal("la simulation a modifié le registre")
	}
}

func TestGUIListeVide(t *testing.T) {
	newTestEnv(t)
	ts, tok := newTestServer(t)

	// Texte entièrement vide : refusé par le serveur, qui ne se fie pas au
	// contrôle fait par la page.
	body, _ := json.Marshal(map[string]any{"text": "   \n\n"})
	code, _ := call(t, ts, http.MethodPost, "/api/add", tok, "", string(body))
	if code != http.StatusBadRequest {
		t.Fatalf("texte vide : code %d, attendu 400", code)
	}

	// Texte non vide mais sans jeu exploitable : la requête aboutit, c'est le
	// résultat qui porte l'explication, que la page affiche.
	body, _ = json.Marshal(map[string]any{"text": "# rien\n\n"})
	r := postJSON(t, ts, tok, "/api/add", string(body))
	if r.Outcome.Error == "" {
		t.Fatal("aucune erreur signalée pour une liste sans jeu valide")
	}
	if r.Outcome.Added != 0 {
		t.Fatalf("added = %d, attendu 0", r.Outcome.Added)
	}
}

// --- retrait ------------------------------------------------------------

func TestGUIRetrait(t *testing.T) {
	_, root := newTestEnv(t)
	ts, tok := newTestServer(t)

	body, _ := json.Marshal(map[string]any{"text": "Skate 3 = https://skate.aaddpp.lol/\nBlack Ops 1 Zombies = https://vel.gg/bo1z\n"})
	added := postJSON(t, ts, tok, "/api/add", string(body))
	if added.Outcome.Added != 2 {
		t.Fatalf("préparation : added = %d", added.Outcome.Added)
	}

	slug := added.State.Games[0].Slug
	rm, _ := json.Marshal(map[string]any{"slugs": []string{slug}})
	r := postJSON(t, ts, tok, "/api/remove", string(rm))

	if r.Outcome.Error != "" {
		t.Fatalf("erreur : %s", r.Outcome.Error)
	}
	if r.Outcome.Removed != 1 {
		t.Fatalf("removed = %d, attendu 1 (journal : %s)", r.Outcome.Removed, r.Outcome.Log)
	}
	if len(r.State.Games) != 1 {
		t.Fatalf("registre : %d jeu(x) restant(s), attendu 1", len(r.State.Games))
	}

	sf, _, err := steam.LoadShortcuts(steam.ShortcutsPath(root, testAccountID))
	if err != nil {
		t.Fatal(err)
	}
	if sf.Count() != 1 {
		t.Fatalf("shortcuts.vdf contient %d entrée(s), attendu 1", sf.Count())
	}
}

// --- refus quand Steam tourne ------------------------------------------

func TestGUIRefuseSiSteamTourne(t *testing.T) {
	_, root := newTestEnv(t)
	startFakeSteam(t)
	ts, tok := newTestServer(t)

	body, _ := json.Marshal(map[string]any{"text": "Skate 3 = https://skate.aaddpp.lol/\n"})
	r := postJSON(t, ts, tok, "/api/add", string(body))

	if !r.Outcome.NeedSteamClosed {
		t.Fatalf("le refus n'a pas été signalé (journal : %s)", r.Outcome.Log)
	}
	if r.Outcome.Added != 0 {
		t.Fatalf("added = %d alors que Steam tourne", r.Outcome.Added)
	}
	if !r.State.SteamRunning {
		t.Error("steam_running devrait être vrai")
	}
	// Le point capital : le fichier n'a pas été touché.
	if _, err := os.Stat(steam.ShortcutsPath(root, testAccountID)); !os.IsNotExist(err) {
		t.Fatalf("shortcuts.vdf a été écrit malgré le refus")
	}
}

// TestHelperProcessSteam sert de faux client Steam : un processus dont le nom
// est "steam", immobile, le temps du test.
func TestHelperProcessSteam(t *testing.T) {
	if os.Getenv("SWL_FAKE_STEAM") != "1" {
		return
	}
	time.Sleep(60 * time.Second)
}

// startFakeSteam lance une copie du binaire de test nommée "steam".
//
// La détection du client se fait sur /proc/<pid>/comm : le nom du fichier
// exécutable suffit donc à simuler Steam, sans toucher au système.
func startFakeSteam(t *testing.T) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Skipf("exécutable de test introuvable : %v", err)
	}
	src, err := os.Open(self)
	if err != nil {
		t.Skipf("lecture de %s : %v", self, err)
	}
	defer src.Close()

	dir := t.TempDir()
	dst := filepath.Join(dir, "steam")
	out, err := os.Create(dst)
	if err != nil {
		t.Skipf("création de %s : %v", dst, err)
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		t.Skipf("copie de l'exécutable : %v", err)
	}
	out.Close()
	if err := os.Chmod(dst, 0o755); err != nil {
		t.Skipf("chmod : %v", err)
	}

	cmd := exec.Command(dst, "-test.run=TestHelperProcessSteam")
	cmd.Env = append(os.Environ(), "SWL_FAKE_STEAM=1")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Skipf("démarrage du faux Steam : %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", cmd.Process.Pid))
		if err == nil && strings.TrimSpace(string(b)) == "steam" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Skip("le processus steam simulé n'est pas apparu dans /proc")
}
