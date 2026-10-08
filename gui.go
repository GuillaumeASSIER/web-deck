package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"deckwebgpu/internal/browser"
	"deckwebgpu/internal/gui"
)

// cmdGUI démarre l'interface locale puis l'ouvre dans une fenêtre Chromium
// sans barre d'outils.
//
// Le serveur s'arrête quand la fenêtre se ferme : c'est ce qu'on attend d'une
// application. Si aucun navigateur n'a pu être lancé (session distante, pas de
// navigateur), on retombe sur l'URL affichée et un arrêt par Ctrl-C ou après
// une longue inactivité, pour ne pas laisser un serveur orphelin.
func cmdGUI(args []string) int {
	fs := flag.NewFlagSet("gui", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:0", "adresse d'écoute (port 0 : automatique)")
	noOpen := fs.Bool("no-open", false, "afficher l'URL sans ouvrir de navigateur")
	idle := fs.Duration("idle-timeout", 30*time.Minute, "arrêt automatique sans navigateur (0 : jamais)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	token, err := randomToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "génération du jeton : %v\n", err)
		return 1
	}

	srv := gui.New(guiBackend{}, token)
	url, err := srv.Listen(*addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "écoute impossible sur %s : %v\n", *addr, err)
		return 1
	}

	fmt.Printf("Interface : %s\n", url)
	fmt.Println("Ctrl-C pour quitter.")

	var win *guiWindow
	if !*noOpen {
		win = openGUIBrowser(url)
		if win == nil {
			fmt.Fprintln(os.Stderr, "avertissement : aucun navigateur n'a pu être ouvert, utilise l'URL ci-dessus.")
		}
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	switch {
	case win != nil && win.blocks:
		// La fenêtre fait vivre l'interface.
		done := make(chan struct{})
		go func() { _ = win.cmd.Wait(); close(done) }()
		select {
		case <-sigs:
			_ = win.cmd.Process.Signal(syscall.SIGTERM)
			<-done
		case <-done:
		}
	case *idle > 0:
		select {
		case <-sigs:
		case <-time.After(*idle):
			fmt.Println("arrêt après inactivité.")
		}
	default:
		<-sigs
	}

	if win != nil && win.profile != "" {
		_ = os.RemoveAll(win.profile)
	}
	return 0
}

// guiWindow décrit la fenêtre ouverte pour l'interface.
type guiWindow struct {
	cmd     *exec.Cmd
	blocks  bool   // true si le processus vit aussi longtemps que la fenêtre
	profile string // profil Chromium temporaire à nettoyer
}

// openGUIBrowser ouvre l'interface. Chromium d'abord, en mode application, ce
// qui donne une fenêtre sans barre d'adresse ni onglets. Le profil est
// temporaire pour ne jamais entrer en conflit avec une partie en cours, qui
// utilise le profil partagé en mode kiosque.
func openGUIBrowser(url string) *guiWindow {
	if b, err := browser.Detect(prefBrowser()); err == nil {
		profile, err := os.MkdirTemp("", "deckwebgpu-gui-")
		if err != nil {
			profile = ""
		}
		argv := b.AppCommand(url, profile)
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err == nil {
			return &guiWindow{cmd: cmd, blocks: true, profile: profile}
		}
		if profile != "" {
			_ = os.RemoveAll(profile)
		}
	}

	// xdg-open rend la main immédiatement : il ouvre dans le navigateur par
	// défaut du bureau, qui peut être Firefox. La fenêtre ne peut donc pas
	// servir de repère de durée de vie.
	if p, err := exec.LookPath("xdg-open"); err == nil {
		cmd := exec.Command(p, url)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err == nil {
			go func() { _ = cmd.Wait() }()
			return &guiWindow{cmd: cmd, blocks: false}
		}
	}
	return nil
}

// randomToken produit le jeton qui protège l'API locale.
func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
