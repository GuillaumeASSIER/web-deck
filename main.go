// Commande swl : lanceur de jeux web WebGPU intégré à Steam.
//
// Trois usages, un seul binaire :
//
//   - l'interface locale enregistre les jeux dans Steam (mode Bureau) ;
//   - lancé par Steam, swl ouvre le navigateur en kiosque sur l'URL du jeu,
//     avec WebGPU actif et la manette visible via l'API Gamepad ;
//   - le mode terminal reste disponible pour les scripts et les sessions sans
//     affichage.
//
// Aucune dépendance externe : le binaire est statique, ce qui lui permet de
// tourner dans un AppImage sans embarquer d'interpréteur.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"deckwebgpu/internal/config"
	"deckwebgpu/internal/registry"
)

const (
	appName    = "DeckWebGPU"
	appVersion = "0.2.0"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) == 0 {
		return defaultCommand()
	}
	switch args[0] {
	case "help", "-h", "--help":
		usage(os.Stdout)
		return 0
	case "version", "-v", "--version":
		fmt.Printf("%s %s\n", appName, appVersion)
		return 0
	case "gui", "--gui":
		return cmdGUI(args[1:])
	case "cli", "--cli":
		return cmdInteractive(args[1:])
	case "install":
		return cmdInstall(args[1:])
	case "uninstall":
		return cmdUninstall(args[1:])
	case "add":
		return cmdAdd(args[1:])
	case "list", "ls":
		return cmdList(args[1:])
	case "remove", "rm":
		return cmdRemove(args[1:])
	case "doctor":
		return cmdDoctor(args[1:])
	case "launch":
		return cmdLaunch(args[1:])
	case "config":
		return cmdConfig(args[1:])
	case "icon":
		return cmdIcon(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "commande inconnue : %s\n\n", args[0])
		usage(os.Stderr)
		return 2
	}
}

// defaultCommand choisit entre l'interface et le mode terminal.
//
// Une liste qui arrive sur l'entrée standard (tube ou redirection) impose le
// mode terminal : c'est le cas de "printf ... | swl". Sinon, dès qu'une session
// graphique est joignable, on ouvre l'interface, y compris depuis un terminal.
// Sans affichage, le mode terminal reste la seule possibilité.
func defaultCommand() int {
	if !isTerminal(os.Stdin) {
		return cmdInteractive(nil)
	}
	if hasDisplay() {
		return cmdGUI(nil)
	}
	return cmdInteractive(nil)
}

// hasDisplay indique qu'une session graphique est joignable.
func hasDisplay() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// ---------------------------------------------------------------------------
// Emplacements sur disque
// ---------------------------------------------------------------------------

// dataDir retourne le dossier de travail de l'outil.
// SWL_DATA_DIR permet de tout déplacer, ce qui sert surtout aux tests.
func dataDir() string {
	if v := os.Getenv("SWL_DATA_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".steam-webgpu"
	}
	return filepath.Join(home, ".local", "share", "steam-webgpu")
}

func binDir() string          { return filepath.Join(dataDir(), "bin") }
func installedBinary() string { return filepath.Join(binDir(), "swl") }
func registryPath() string    { return filepath.Join(dataDir(), "games.json") }
func configPath() string      { return filepath.Join(dataDir(), "config.json") }
func profileDir() string      { return filepath.Join(dataDir(), "profile") }
func iconsDir() string        { return filepath.Join(dataDir(), "icons") }
func logsDir() string         { return filepath.Join(dataDir(), "logs") }

// ---------------------------------------------------------------------------
// Chargement des données locales
// ---------------------------------------------------------------------------

func loadStore() (*registry.Store, error) {
	return registry.Load(registryPath())
}

func loadConfig() (*config.Config, error) {
	return config.Load(configPath())
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// ---------------------------------------------------------------------------
// Aide
// ---------------------------------------------------------------------------

func usage(w *os.File) {
	fmt.Fprint(w, `DeckWebGPU : jouer des jeux web WebGPU depuis Steam, en mode jeu.

UTILISATION
  swl                            ouvrir l'interface, ou coller une liste si
                                 une liste arrive sur l'entrée standard
  swl gui                        ouvrir l'interface explicitement
  swl cli                        mode terminal, sans interface
  swl install                    installer le lanceur et vérifier l'environnement
  swl add -f liste.txt           ajouter les jeux décrits dans un fichier
  swl list                       lister les jeux enregistrés
  swl remove <slug>              retirer un jeu de Steam
  swl doctor                     diagnostic (Steam, navigateur, manette, WebGPU)
  swl launch <slug>              ouvrir un jeu (appelé par Steam, pas à la main)
  swl config                     afficher la configuration effective
  swl icon <fichier.png>         écrire l'icône de remplacement
  swl version                    version

FORMAT DE LA LISTE (une ligne par jeu)
  # commentaire
  Mon Jeu          = https://exemple.com/jeu
  Autre Jeu        | https://autre.example.org
  https://troisieme.example.net               (le nom est déduit de l'URL)
  Quatrieme        = https://exemple.com/4 | --force-device-scale-factor=1

OPTIONS DE L'INTERFACE
  --no-open       afficher l'adresse sans ouvrir de navigateur
  --addr <a:p>    écouter sur une autre adresse (127.0.0.1:0 par défaut)
  --idle-timeout  arrêt automatique sans navigateur (30m par défaut)

OPTIONS COMMUNES
  --yes           ne pas demander de confirmation
  --dry-run       afficher les changements sans rien écrire
  --force         écrire même si Steam tourne (les changements seront perdus à
                  la fermeture de Steam, à n'utiliser qu'en connaissance)
  --user <id>     forcer le compte Steam (userdata/<id>)
  --browser <id>  forcer le navigateur (voir swl doctor)
  --no-artwork    ne pas générer les vignettes de bibliothèque

RAPPEL : Steam doit être fermé pour écrire les raccourcis. L'interface propose
un bouton qui le ferme proprement.
`)
}
