package steam

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Ce fichier localise l'installation Steam, identifie le compte actif et
// détermine si le client tourne. Les trois questions ont la même réponse
// critique : où écrire, sous quel utilisateur, et a-t-on le droit d'écrire.

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// Roots retourne les emplacements candidats d'une installation Steam, dans
// l'ordre de préférence et sans doublon. On dédoublonne sur le chemin résolu
// car ~/.steam/steam est presque toujours un lien vers ~/.local/share/Steam.
func Roots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var cands []string
	if v := os.Getenv("STEAM_ROOT"); v != "" {
		cands = append(cands, v)
	}
	cands = append(cands,
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".local", "share", "Steam"),
		// Steam installé en flatpak
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
		// Steam installé en snap
		filepath.Join(home, "snap", "steam", "common", ".local", "share", "Steam"),
	)

	seen := map[string]bool{}
	var out []string
	for _, c := range cands {
		if !dirExists(c) {
			continue
		}
		key := c
		if r, err := filepath.EvalSymlinks(c); err == nil {
			key = r
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}

// FindRoot retourne l'installation Steam à utiliser.
func FindRoot() (string, error) {
	roots := Roots()
	if len(roots) == 0 {
		return "", fmt.Errorf("aucune installation Steam trouvée (essayé : ~/.steam/steam, ~/.local/share/Steam, flatpak, snap)")
	}
	// 1. une installation avec un compte déjà connecté
	for _, r := range roots {
		if fileExists(filepath.Join(r, "config", "loginusers.vdf")) {
			return r, nil
		}
	}
	// 2. une installation avec des données utilisateur
	for _, r := range roots {
		if dirExists(filepath.Join(r, "userdata")) {
			return r, nil
		}
	}
	return roots[0], nil
}

// userdataDirs liste les identifiants de compte présents dans userdata/.
// Le dossier "0" est le compte anonyme de Steam, on l'exclut.
func userdataDirs(root string) []uint32 {
	entries, err := os.ReadDir(filepath.Join(root, "userdata"))
	if err != nil {
		return nil
	}
	var out []uint32
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		v, err := strconv.ParseUint(e.Name(), 10, 32)
		if err != nil || v == 0 {
			continue
		}
		out = append(out, uint32(v))
	}
	return out
}

// LastActiveUser retourne l'identifiant de compte (SteamID3) du dernier
// profil connecté, celui dont les raccourcis sont affichés par Steam.
func LastActiveUser(root string) (uint32, error) {
	if data, err := os.ReadFile(filepath.Join(root, "config", "loginusers.vdf")); err == nil {
		if id64, ok := lastActiveSteamID64(data); ok {
			if acc, ok := accountID(id64); ok && dirExists(filepath.Join(root, "userdata", strconv.FormatUint(uint64(acc), 10))) {
				return acc, nil
			}
		}
	}

	dirs := userdataDirs(root)
	switch len(dirs) {
	case 0:
		return 0, fmt.Errorf("aucun profil dans %s/userdata : lance Steam une fois et connecte-toi, puis relance cette commande", root)
	case 1:
		return dirs[0], nil
	default:
		var ids []string
		for _, d := range dirs {
			ids = append(ids, strconv.FormatUint(uint64(d), 10))
		}
		return 0, fmt.Errorf("plusieurs profils Steam (%s) et aucun loginusers.vdf exploitable : précise le compte avec --user", strings.Join(ids, ", "))
	}
}

// ConfigDir retourne le dossier de configuration d'un compte, celui qui
// contient shortcuts.vdf et grid/.
func ConfigDir(root string, user uint32) string {
	return filepath.Join(root, "userdata", strconv.FormatUint(uint64(user), 10), "config")
}

// pidFiles liste les fichiers pid que le client Steam maintient.
func pidFiles(root string) []string {
	out := []string{filepath.Join(root, "steam.pid")}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out,
			filepath.Join(home, ".steam", "steam.pid"),
			filepath.Join(home, ".steam", "steam", "steam.pid"),
		)
	}
	return out
}

// procComm retourne le nom du programme d'un pid, ou "" si le processus a
// disparu (ou si /proc n'est pas monté).
func procComm(pid int) string {
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// IsRunning indique si le client Steam tourne.
//
// C'est la question qui décide de tout : Steam recharge shortcuts.vdf au
// démarrage et le réécrit depuis sa mémoire quand il se ferme. Écrire pendant
// qu'il tourne revient à perdre les modifications, silencieusement.
//
// Deux signaux, dans cet ordre : le fichier steam.pid (validé par /proc pour
// écarter un pid recyclé), puis un balayage de /proc.
func IsRunning(root string) bool {
	for _, p := range pidFiles(root) {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil || pid <= 0 {
			continue
		}
		if procComm(pid) == "steam" {
			return true
		}
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		if procComm(mustAtoi(e.Name())) == "steam" {
			return true
		}
	}
	return false
}

func mustAtoi(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}
