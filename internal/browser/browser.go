// Package browser détecte un navigateur basé sur Chromium et construit la
// ligne de commande qui ouvre une URL en plein écran avec WebGPU actif.
//
// On n'embarque pas CEF : sur SteamOS un Chromium à jour existe déjà en
// flatpak, et l'embarquer signifierait redistribuer 150 Mo de moteur, gérer
// ses mises à jour de sécurité à la place de l'utilisateur, et écrire du
// C++ pour rien. Un lanceur qui pilote le navigateur du système fait le même
// travail avec 3 Mo et zéro dette de mise à jour.
package browser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Kind distingue un navigateur installé en flatpak d'un binaire classique.
type Kind string

const (
	KindFlatpak Kind = "flatpak"
	KindNative  Kind = "native"
)

// Browser est un navigateur Chromium utilisable.
type Browser struct {
	ID    string // identifiant flatpak ("org.chromium.Chromium") ou nom du binaire
	Kind  Kind
	Path  string // chemin de "flatpak" ou du binaire
	Label string // nom lisible
}

// DefaultWebGPUArgs active WebGPU sous Linux sur un GPU AMD (RDNA2 du Steam
// Deck, donc RADV).
//
// État du support, vérifié sur le wiki d'implémentation du groupe GPU Web :
// Chrome active WebGPU par défaut sur macOS, Windows, ChromeOS, sur Intel
// Gen12+ sous Linux depuis la version 144, et sur NVIDIA/Wayland depuis la
// 147. Tout le reste, AMD compris, reste derrière un flag. La combinaison
// minimale qui fonctionne est celle-ci, et elle compte plus de flags que le
// seul --enable-unsafe-webgpu :

//   - --ozone-platform=x11 : la pile Vulkan/ANGLE validée passe par XWayland,
//     pas par le backend Wayland natif ;
//   - --use-angle=vulkan + VulkanFromANGLE : on veut ANGLE par-dessus Vulkan,
//     et non par-dessus GL ;
//   - --ignore-gpu-blocklist : écarte la liste de blocage logicielle, qui
//     vise les pilotes anciens mais peut attraper RADV ;
//   - --enable-gpu-rasterization : confort, laisse le compositing sur le GPU.
var DefaultWebGPUArgs = []string{
	"--enable-unsafe-webgpu",
	"--ozone-platform=x11",
	"--use-angle=vulkan",
	"--enable-features=Vulkan,VulkanFromANGLE",
	"--ignore-gpu-blocklist",
	"--enable-gpu-rasterization",
}

// kioskArgs supprime tout ce qui peut interrompre une partie : barres,
// bulles de premier lancement, restauration après plantage, invitation du
// trousseau de clés (en mode jeu, personne ne saisit de mot de passe).
var kioskArgs = []string{
	"--kiosk",
	"--no-first-run",
	"--no-default-browser-check",
	"--disable-session-crashed-bubble",
	"--hide-crash-restore-bubble",
	"--autoplay-policy=no-user-gesture-required",
	"--password-store=basic",
}

var (
	flatpakCandidates = []struct{ ID, Label string }{
		{"org.chromium.Chromium", "Chromium (flatpak)"},
		{"com.google.Chrome", "Google Chrome (flatpak)"},
		{"com.brave.Browser", "Brave (flatpak)"},
		{"com.microsoft.Edge", "Microsoft Edge (flatpak)"},
	}
	nativeCandidates = []struct{ Bin, Label string }{
		{"chromium", "Chromium"},
		{"chromium-browser", "Chromium"},
		{"google-chrome-stable", "Google Chrome"},
		{"google-chrome", "Google Chrome"},
		{"brave-browser", "Brave"},
		{"microsoft-edge-stable", "Microsoft Edge"},
		{"microsoft-edge", "Microsoft Edge"},
	}
	extraPaths = []struct{ Path, Label string }{
		{"/app/bin/chromium", "Chromium (dans un sandbox)"},
		{"/usr/lib/chromium/chromium", "Chromium"},
		{"/usr/bin/chromium", "Chromium"},
		{"/opt/google/chrome/chrome", "Google Chrome"},
		{"/opt/google/chrome/google-chrome", "Google Chrome"},
		{"/opt/brave.com/brave/brave", "Brave"},
	}
)

// Detect retourne le navigateur à utiliser. Si preferred est non vide, il est
// respecté tel quel (identifiant flatpak, nom de binaire ou chemin absolu).
func Detect(preferred string) (*Browser, error) {
	if preferred != "" {
		return resolve(preferred)
	}

	if installed := installedFlatpaks(); installed != nil {
		for _, c := range flatpakCandidates {
			if installed[c.ID] {
				if p, err := exec.LookPath("flatpak"); err == nil {
					return &Browser{ID: c.ID, Kind: KindFlatpak, Path: p, Label: c.Label}, nil
				}
			}
		}
	}

	for _, c := range nativeCandidates {
		if p, err := exec.LookPath(c.Bin); err == nil {
			return &Browser{ID: c.Bin, Kind: KindNative, Path: p, Label: c.Label}, nil
		}
	}
	for _, c := range extraPaths {
		if st, err := os.Stat(c.Path); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return &Browser{ID: c.Path, Kind: KindNative, Path: c.Path, Label: c.Label}, nil
		}
	}

	return nil, fmt.Errorf("aucun navigateur Chromium détecté.\n" +
		"Installe Chromium en flatpak, puis relance :\n" +
		"  flatpak install --user flathub org.chromium.Chromium")
}

// resolve interprète une préférence saisie par l'utilisateur.
func resolve(pref string) (*Browser, error) {
	if strings.ContainsRune(pref, '/') {
		if _, err := os.Stat(pref); err != nil {
			return nil, fmt.Errorf("navigateur introuvable : %s", pref)
		}
		return &Browser{ID: pref, Kind: KindNative, Path: pref, Label: pref}, nil
	}
	if strings.Count(pref, ".") >= 2 {
		if installed := installedFlatpaks(); installed[pref] {
			p, err := exec.LookPath("flatpak")
			if err != nil {
				return nil, fmt.Errorf("flatpak introuvable pour lancer %s", pref)
			}
			return &Browser{ID: pref, Kind: KindFlatpak, Path: p, Label: pref}, nil
		}
		return nil, fmt.Errorf("le flatpak %s n'est pas installé", pref)
	}
	if p, err := exec.LookPath(pref); err == nil {
		return &Browser{ID: pref, Kind: KindNative, Path: p, Label: pref}, nil
	}
	return nil, fmt.Errorf("navigateur %q introuvable (ni flatpak, ni binaire dans le PATH)", pref)
}

// installedFlatpaks retourne les identifiants des applications flatpak
// installées. Une seule invocation, plusieurs dizaines de millisecondes :
// on ne veut pas interroger flatpak une fois par candidat.
func installedFlatpaks() map[string]bool {
	p, err := exec.LookPath("flatpak")
	if err != nil {
		return nil
	}
	out, err := exec.Command(p, "list", "--app", "--columns=application").Output()
	if err != nil {
		return nil
	}
	set := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			set[line] = true
		}
	}
	return set
}

// Describe retourne une description lisible du navigateur.
func (b *Browser) Describe() string {
	return fmt.Sprintf("%s (%s)", b.Label, b.ID)
}

// Version retourne la version du navigateur, ou une chaîne vide.
func (b *Browser) Version() string {
	switch b.Kind {
	case KindFlatpak:
		out, err := exec.Command(b.Path, "info", b.ID).Output()
		if err != nil {
			return ""
		}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Version:") {
				return strings.TrimSpace(strings.TrimPrefix(line, "Version:"))
			}
		}
		return ""
	default:
		out, err := exec.Command(b.Path, "--version").Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
}

// AppCommand construit une ligne de commande qui ouvre une fenêtre sans barre
// d'outils, utilisée pour l'interface de configuration.
//
// Aucun flag WebGPU n'est ajouté : la page ne dessine rien en 3D, et forcer
// --ozone-platform=x11 ferait échouer Chromium sur une session sans serveur X.
func (b *Browser) AppCommand(url, profileDir string) []string {
	var args []string
	if profileDir != "" {
		args = append(args, "--user-data-dir="+profileDir)
	}
	args = append(args,
		"--app="+url,
		"--no-first-run",
		"--no-default-browser-check",
		"--window-size=960,860",
	)
	if b.Kind == KindFlatpak {
		return append([]string{b.Path, "run", b.ID}, args...)
	}
	return append([]string{b.Path}, args...)
}

// AdjustForDisplay retire --ozone-platform=x11 quand aucun serveur X n'est
// joignable.
//
// Ce flag est nécessaire à WebGPU sur AMD, mais Chromium refuse de démarrer
// sans $DISPLAY quand il est présent. Un jeu qui ne démarre pas est plus grave
// qu'un jeu qui démarre sans WebGPU : on préfère donc le retirer et laisser
// Chromium choisir son backend. Le retour indique si le retrait a eu lieu, pour
// que l'appelant puisse l'expliquer dans le journal.
func AdjustForDisplay(args []string) ([]string, bool) {
	if os.Getenv("DISPLAY") != "" {
		return args, false
	}
	out := make([]string, 0, len(args))
	dropped := false
	for _, a := range args {
		if a == "--ozone-platform=x11" {
			dropped = true
			continue
		}
		out = append(out, a)
	}
	return out, dropped
}

// LaunchSpec décrit une ouverture de fenêtre.
type LaunchSpec struct {
	URL        string
	ProfileDir string
	Kiosk      bool
	WebGPUArgs []string // nil = DefaultWebGPUArgs
	ExtraArgs  []string
}

// Args construit les arguments passés au navigateur, sans l'exécutable.
func (b *Browser) Args(s LaunchSpec) []string {
	var args []string

	if s.ProfileDir != "" {
		args = append(args, "--user-data-dir="+s.ProfileDir)
	}

	webgpu := s.WebGPUArgs
	if webgpu == nil {
		webgpu = DefaultWebGPUArgs
	}
	args = append(args, webgpu...)

	if s.Kiosk {
		args = append(args, kioskArgs...)
	}

	// Les arguments de l'utilisateur passent en dernier : Chrome retient la
	// dernière valeur d'une option répétée, ce qui permet de corriger un flag
	// sans recompiler.
	args = append(args, s.ExtraArgs...)

	if s.URL != "" {
		args = append(args, s.URL)
	}
	return args
}

// Command construit la ligne de commande complète.
func (b *Browser) Command(s LaunchSpec) []string {
	args := b.Args(s)
	if b.Kind == KindFlatpak {
		return append([]string{b.Path, "run", b.ID}, args...)
	}
	return append([]string{b.Path}, args...)
}

// ---------------------------------------------------------------------------
// Manette : accès à /run/udev dans le sandbox flatpak
// ---------------------------------------------------------------------------

// OverrideFile retourne le chemin du fichier d'écrasement flatpak du
// navigateur (écrit par "flatpak override --user").
func (b *Browser) OverrideFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "flatpak", "overrides", b.ID)
}

func globalOverrideFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "flatpak", "overrides", "global")
}

// overrideGrantsUdev indique si un fichier d'écrasement accorde /run/udev.
func overrideGrantsUdev(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "filesystems=") && strings.Contains(line, "udev") {
			return true
		}
	}
	return false
}

// HasUdevOverride indique si le sandbox a déjà accès à /run/udev, la base de
// données que libudev interroge pour énumérer les manettes. Sans elle, l'API
// Gamepad ne voit aucun périphérique, même avec --device=all.
func (b *Browser) HasUdevOverride() bool {
	if b.Kind != KindFlatpak {
		return true
	}
	return overrideGrantsUdev(b.OverrideFile()) || overrideGrantsUdev(globalOverrideFile())
}

// EnsureUdevOverride applique l'écrasement si nécessaire.
// Retourne true si une modification a été faite.
func (b *Browser) EnsureUdevOverride() (bool, error) {
	if b.Kind != KindFlatpak || b.HasUdevOverride() {
		return false, nil
	}
	cmd := exec.Command(b.Path, "override", "--user", "--filesystem=/run/udev:ro", b.ID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("flatpak override a échoué : %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

// UdevOverrideCommand retourne la commande à documenter ou à afficher.
func (b *Browser) UdevOverrideCommand() string {
	return fmt.Sprintf("flatpak override --user --filesystem=/run/udev:ro %s", b.ID)
}
