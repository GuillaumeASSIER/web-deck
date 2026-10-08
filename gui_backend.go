package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"deckwebgpu/internal/browser"
	"deckwebgpu/internal/gui"
	"deckwebgpu/internal/registry"
	"deckwebgpu/internal/steam"
)

// guiBackend relie l'interface à la logique métier.
//
// Il ne fait que traduire : les décisions vivent dans addGames, removeGames et
// prepareLaunch, déjà utilisées par le CLI. Les deux chemins se comportent donc
// exactement pareil, y compris pour le refus d'écrire quand Steam tourne.
type guiBackend struct{}

func (guiBackend) State() gui.State {
	var st gui.State
	st.DataDir = dataDir()
	st.LogsDir = logsDir()
	st.BinaryPath = installedBinary()
	st.BinaryInstalled = fileExists(st.BinaryPath)

	if root, err := steam.FindRoot(); err != nil {
		st.SteamError = err.Error()
	} else {
		st.SteamRoot = root
		st.SteamRunning = steam.IsRunning(root)
		if user, err := steam.LastActiveUser(root); err != nil {
			st.SteamError = err.Error()
		} else {
			st.SteamUser = user
			st.ShortcutsPath = steam.ShortcutsPath(root, user)
			sf, _, err := steam.LoadShortcuts(st.ShortcutsPath)
			if err != nil {
				st.SteamError = err.Error()
			} else {
				st.ShortcutCount = sf.Count()
			}
		}
	}

	if b, err := browser.Detect(prefBrowser()); err != nil {
		st.BrowserError = "aucun navigateur Chromium détecté"
	} else {
		st.BrowserLabel = b.Label
		st.BrowserVersion = b.Version()
		if b.Kind == browser.KindFlatpak {
			st.UdevOK = b.HasUdevOverride()
			if !st.UdevOK {
				st.UdevCommand = b.UdevOverrideCommand()
			}
		} else {
			st.UdevOK = true
		}
	}

	if store, err := loadStore(); err == nil {
		st.Games = store.Games
		// La liste d'exemple n'apparaît que sur un registre vide : elle sert à
		// valider la chaîne complète, pas à encombrer une bibliothèque en cours.
		if len(store.Games) == 0 {
			st.Defaults = defaultList
		}
	}
	return st
}

func (guiBackend) Add(text string, dryRun, force bool) gui.Outcome {
	store, err := loadStore()
	if err != nil {
		return gui.Outcome{Error: err.Error()}
	}

	res := registry.ParseList(text, store.Slugs())
	var buf bytes.Buffer
	for _, issue := range res.Issues {
		fmt.Fprintf(&buf, "avertissement : %s\n", issue)
	}
	if len(res.Games) == 0 {
		buf.WriteString("aucun jeu valide dans cette liste.\n")
		return gui.Outcome{Log: buf.String(), Error: "liste vide ou invalide"}
	}

	o := addOptions{dryRun: dryRun, force: force, yes: true}
	outcome, err := addGames(res.Games, o, &buf)
	if err != nil {
		out := gui.Outcome{Log: buf.String()}
		if errors.Is(err, errSteamRunning) {
			out.Error = "Steam est en cours d'exécution, l'écriture a été refusée pour ne pas perdre les raccourcis."
			out.NeedSteamClosed = true
			return out
		}
		out.Error = err.Error()
		return out
	}
	return gui.Outcome{
		Log:     buf.String(),
		Added:   len(outcome.Added),
		Skipped: len(outcome.Skipped),
	}
}

func (guiBackend) Remove(slugs []string) gui.Outcome {
	var buf bytes.Buffer
	n, err := removeGames(slugs, false, &buf)
	if err != nil {
		out := gui.Outcome{Log: buf.String()}
		if errors.Is(err, errSteamRunning) {
			out.Error = "Steam est en cours d'exécution, la suppression a été refusée."
			out.NeedSteamClosed = true
			return out
		}
		out.Error = err.Error()
		return out
	}
	if n == 0 {
		buf.WriteString("aucun raccourci correspondant trouvé.\n")
	}
	return gui.Outcome{Log: buf.String(), Removed: n}
}

// ShutdownSteam ferme proprement le client Steam, ce qui débloque l'écriture.
// On attend la disparition effective du processus : lancer la commande ne
// suffit pas, Steam met plusieurs secondes à s'arrêter et écrirait ses
// raccourcis après notre écriture si on se contentait de la lancer.
func (guiBackend) ShutdownSteam() gui.Outcome {
	var buf bytes.Buffer

	root, err := steam.FindRoot()
	if err != nil {
		return gui.Outcome{Error: err.Error()}
	}
	if !steam.IsRunning(root) {
		return gui.Outcome{Log: "Steam est déjà arrêté.\n"}
	}

	argv := steamShutdownCommand()
	if len(argv) == 0 {
		return gui.Outcome{Error: "commande steam introuvable, ferme Steam à la main (menu Steam puis Quitter)"}
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(&buf, "commande %v : %v\n", argv, err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if !steam.IsRunning(root) {
			buf.WriteString("Steam est fermé.\n")
			return gui.Outcome{Log: buf.String()}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return gui.Outcome{
		Log:   buf.String(),
		Error: "Steam n'a pas rendu la main en 30 s, ferme-le à la main",
	}
}

// steamShutdownCommand retourne la commande d'arrêt, native ou flatpak.
func steamShutdownCommand() []string {
	if p, err := exec.LookPath("steam"); err == nil {
		return []string{p, "-shutdown"}
	}
	if p, err := exec.LookPath("flatpak"); err == nil {
		return []string{p, "run", "com.valvesoftware.Steam", "-shutdown"}
	}
	return nil
}

// Launch ouvre un jeu comme le ferait Steam, mais sans bloquer l'interface.
func (guiBackend) Launch(slug string) gui.Outcome {
	logPath, err := guiLaunch(slug)
	if err != nil {
		return gui.Outcome{Error: err.Error()}
	}
	msg := "Ouverture de " + slug + " en plein écran.\n"
	if logPath != "" {
		msg += "Journal : " + logPath + "\n"
	}
	return gui.Outcome{Log: msg}
}
