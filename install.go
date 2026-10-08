package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"deckwebgpu/internal/artwork"
	"deckwebgpu/internal/browser"
	"deckwebgpu/internal/config"
	"deckwebgpu/internal/steam"
)

// ---------------------------------------------------------------------------
// swl install
// ---------------------------------------------------------------------------

func cmdInstall(args []string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	noUdev := fs.Bool("no-udev-override", false, "ne pas modifier les permissions du flatpak")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if err := os.MkdirAll(binDir(), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "création de %s : %v\n", binDir(), err)
		return 1
	}
	if err := installSelf(); err != nil {
		fmt.Fprintf(os.Stderr, "installation du lanceur : %v\n", err)
		return 1
	}
	if err := artwork.Icon(filepath.Join(dataDir(), "steam-webgpu-launcher.png"), appName); err != nil {
		fmt.Fprintf(os.Stderr, "icône : %v\n", err)
	}
	linkIntoPath()

	fmt.Println()
	if b, err := browser.Detect(""); err != nil {
		fmt.Printf("navigateur : aucun\n  %v\n", err)
	} else {
		fmt.Printf("navigateur : %s\n", b.Describe())
		if v := b.Version(); v != "" {
			fmt.Printf("version    : %s\n", v)
		}
		reportUdev(b, *noUdev)
	}

	fmt.Printf("\nDonnées : %s\n", dataDir())
	fmt.Println("\nÉtape suivante :")
	fmt.Println("  swl                     (ouvre l'interface pour ajouter des jeux)")
	fmt.Println("  swl doctor              (diagnostic complet)")
	return 0
}

// installSelf copie le binaire en cours d'exécution vers un emplacement
// stable. Lancé depuis un AppImage, os.Executable() pointe dans le point de
// montage FUSE, qui disparaît à la sortie : un raccourci Steam ne peut pas
// viser ce chemin.
func installSelf() error {
	src, err := os.Executable()
	if err != nil {
		return err
	}
	dst := installedBinary()

	srcReal, err1 := filepath.EvalSymlinks(src)
	dstReal, err2 := filepath.EvalSymlinks(dst)
	if err1 == nil && err2 == nil && srcReal == dstReal {
		fmt.Printf("lanceur déjà installé : %s\n", dst)
		return nil
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("lecture de %s : %w", src, err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		return err
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		return err
	}
	fmt.Printf("lanceur installé : %s\n", dst)
	if appimage := os.Getenv("APPIMAGE"); appimage != "" {
		fmt.Printf("copie depuis     : %s\n", appimage)
	}
	return nil
}

// linkIntoPath pose un lien symbolique dans ~/.local/bin, sans écraser un
// fichier qui n'appartiendrait pas à cet outil.
func linkIntoPath() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	link := filepath.Join(dir, "swl")

	if st, err := os.Lstat(link); err == nil && st.Mode()&os.ModeSymlink == 0 {
		fmt.Printf("attention    : %s existe et n'est pas un lien, laissé intact\n", link)
		return
	}
	_ = os.Remove(link)
	if err := os.Symlink(installedBinary(), link); err != nil {
		return
	}
	fmt.Printf("raccourci    : %s\n", link)

	inPath := false
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == dir {
			inPath = true
		}
	}
	if !inPath {
		fmt.Printf("attention    : %s n'est pas dans le PATH\n", dir)
	}
}

// reportUdev explique et applique si besoin l'accès à /run/udev.
//
// Le sandbox flatpak de Chromium expose --device=all (donc les périphériques
// /dev/input/event*), mais pas la base udev. Or libudev, qui énumère les
// manettes pour l'API Gamepad, lit /run/udev. Sans cet accès, le jeu ne voit
// aucune manette même si le périphérique est accessible.
func reportUdev(b *browser.Browser, skip bool) {
	if b.Kind != browser.KindFlatpak {
		return
	}
	switch {
	case b.HasUdevOverride():
		fmt.Println("manette    : accès /run/udev déjà accordé")
	case skip:
		fmt.Println("manette    : accès /run/udev manquant (ignoré sur demande)")
		fmt.Printf("             à appliquer plus tard : %s\n", b.UdevOverrideCommand())
	default:
		changed, err := b.EnsureUdevOverride()
		if err != nil {
			fmt.Printf("manette    : échec de l'écrasement flatpak (%v)\n", err)
			fmt.Printf("             à appliquer à la main : %s\n", b.UdevOverrideCommand())
			return
		}
		if changed {
			fmt.Println("manette    : accès /run/udev accordé")
		}
	}
}

// ---------------------------------------------------------------------------
// swl uninstall
// ---------------------------------------------------------------------------

func cmdUninstall(args []string) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "supprimer aussi les données et le registre")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	home, _ := os.UserHomeDir()
	_ = os.Remove(filepath.Join(home, ".local", "bin", "swl"))

	if *purge {
		if err := os.RemoveAll(dataDir()); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		fmt.Printf("données supprimées : %s\n", dataDir())
	} else {
		_ = os.Remove(installedBinary())
		fmt.Printf("lanceur retiré. Données conservées dans %s (--purge pour tout effacer).\n", dataDir())
	}
	fmt.Println("Les raccourcis restent dans Steam : utilise swl remove <slug> avant de désinstaller si tu veux les retirer.")
	return 0
}

// ---------------------------------------------------------------------------
// swl doctor
// ---------------------------------------------------------------------------

func cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	openGPU := fs.Bool("open-gpu", false, "ouvrir chrome://gpu pour vérifier WebGPU")
	noUdev := fs.Bool("no-udev-override", false, "ne pas modifier les permissions du flatpak")
	noUdevShort := fs.Bool("no-udev", false, "ne pas modifier les permissions du flatpak")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	fmt.Printf("%s %s (%s/%s)\n\n", appName, appVersion, runtime.GOOS, runtime.GOARCH)

	fmt.Println("Steam")
	roots := steam.Roots()
	if len(roots) == 0 {
		fmt.Println("  aucune installation détectée")
	}
	for _, r := range roots {
		fmt.Printf("  trouvé        : %s\n", r)
	}
	root, err := steam.FindRoot()
	if err != nil {
		fmt.Printf("  erreur        : %v\n", err)
	} else {
		fmt.Printf("  utilisé       : %s\n", root)
		if steam.IsRunning(root) {
			fmt.Println("  client        : EN COURS D'EXÉCUTION")
			fmt.Println("                  ferme Steam avant d'ajouter ou de retirer des jeux")
		} else {
			fmt.Println("  client        : arrêté (état requis pour écrire)")
		}
		if user, err := steam.LastActiveUser(root); err != nil {
			fmt.Printf("  profil        : %v\n", err)
		} else {
			path := steam.ShortcutsPath(root, user)
			sf, existed, err := steam.LoadShortcuts(path)
			switch {
			case err != nil:
				fmt.Printf("  profil        : %d\n", user)
				fmt.Printf("  raccourcis    : illisible (%v)\n", err)
			case !existed:
				fmt.Printf("  profil        : %d\n", user)
				fmt.Println("  raccourcis    : fichier absent, il sera créé au premier ajout")
			default:
				fmt.Printf("  profil        : %d\n", user)
				fmt.Printf("  raccourcis    : %d entrée(s) dans %s\n", sf.Count(), path)
			}
		}
	}

	fmt.Println("\nNavigateur")
	pref := ""
	if cfg, err := loadConfig(); err == nil {
		pref = cfg.Browser
	}
	if v := os.Getenv("SWL_BROWSER"); v != "" {
		pref = v
	}
	b, err := browser.Detect(pref)
	if err != nil {
		fmt.Printf("  erreur        : %v\n", err)
	} else {
		fmt.Printf("  utilisé       : %s\n", b.Describe())
		if v := b.Version(); v != "" {
			fmt.Printf("  version       : %s\n", v)
		}
		if b.Kind == browser.KindFlatpak {
			reportUdev(b, *noUdev || *noUdevShort)
		}
	}

	fmt.Println("\nManette")
	fmt.Println("  Chromium lit /run/udev pour énumérer les manettes (API Gamepad).")
	fmt.Println("  En jeu, Steam Input présente la manette du Deck comme une manette")
	fmt.Println("  virtuelle : dans le jeu, bouton Steam puis Contrôleur, et vérifie")
	fmt.Println("  qu'un modèle avec manette est bien actif.")

	fmt.Println("\nWebGPU")
	fmt.Println("  Sur Linux, Chrome active WebGPU par défaut pour Intel Gen12+ et")
	fmt.Println("  pour NVIDIA sous Wayland. Sur AMD (RDNA2 du Deck, pilote RADV), il")
	fmt.Println("  reste derrière un flag. Les arguments appliqués sont :")
	for _, a := range browser.DefaultWebGPUArgs {
		fmt.Printf("    %s\n", a)
	}
	fmt.Println("  Ces arguments sont surchargeables dans", configPath())

	if *openGPU {
		if b == nil {
			fmt.Println("\n  impossible d'ouvrir chrome://gpu : aucun navigateur.")
			return 1
		}
		spec := browser.LaunchSpec{
			URL:        "chrome://gpu",
			ProfileDir: profileDir(),
			Kiosk:      false,
		}
		argv := b.Command(spec)
		fmt.Printf("\n  ouverture de chrome://gpu (%s)\n", strings.Join(argv, " "))
		fmt.Println("  cherche la ligne \"WebGPU\" : elle doit indiquer une accélération")
		fmt.Println("  matérielle, pas \"software only\".")
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "  échec : %v\n", err)
			return 1
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// swl config
// ---------------------------------------------------------------------------

func cmdConfig(args []string) int {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	init := fs.Bool("init", false, "écrire un fichier de configuration par défaut")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *init {
		cfg := config.Default()
		cfg.WebGPUArgs = browser.DefaultWebGPUArgs
		if err := cfg.Save(configPath()); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		fmt.Printf("configuration écrite : %s\n", configPath())
		fmt.Println("Les arguments WebGPU y sont figés : ce fichier devient la référence,")
		fmt.Println("même si une future version du binaire change ses valeurs par défaut.")
		return 0
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	fmt.Printf("fichier           : %s\n", configPath())
	if !fileExists(configPath()) {
		fmt.Println("                    (absent, valeurs par défaut utilisées)")
	}
	fmt.Printf("navigateur        : %s\n", orAuto(cfg.Browser))
	fmt.Printf("dossier de profil : %s\n", orAuto(cfg.ProfileDir))
	if cfg.ProfileDir == "" {
		fmt.Printf("                    %s\n", profileDir())
	}
	fmt.Println("arguments WebGPU  :")
	if cfg.WebGPUArgs == nil {
		for _, a := range browser.DefaultWebGPUArgs {
			fmt.Printf("                    %s (défaut)\n", a)
		}
	} else {
		for _, a := range cfg.WebGPUArgs {
			fmt.Printf("                    %s\n", a)
		}
	}
	fmt.Println("arguments en plus :")
	if len(cfg.ExtraArgs) == 0 {
		fmt.Println("                    aucun")
	} else {
		for _, a := range cfg.ExtraArgs {
			fmt.Printf("                    %s\n", a)
		}
	}
	fmt.Println("\nPour figer les valeurs actuelles : swl config --init")
	return 0
}

func orAuto(s string) string {
	if s == "" {
		return "détection automatique"
	}
	return s
}

// ---------------------------------------------------------------------------
// swl icon
// ---------------------------------------------------------------------------

func cmdIcon(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage : swl icon <fichier.png>")
		return 2
	}
	if err := artwork.Icon(args[0], appName); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	fmt.Printf("icône écrite : %s\n", args[0])
	return 0
}

// ---------------------------------------------------------------------------
// swl launch  (appelé par Steam, jamais à la main)
// ---------------------------------------------------------------------------

func cmdLaunch(args []string) int {
	fs := flag.NewFlagSet("launch", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage : swl launch <slug> [arguments Chromium supplémentaires]")
		return 2
	}
	slug := fs.Arg(0)
	extra := fs.Args()[1:]

	argv, logFile, _, err := prepareLaunch(slug, extra)
	if err != nil {
		if errors.Is(err, errUnknownGame) {
			fmt.Fprintf(os.Stderr, "jeu inconnu : %s (voir swl list)\n", slug)
			return 3
		}
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if logFile != nil {
		defer logFile.Close()
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	if logFile != nil {
		cmd.Stdout, cmd.Stderr = logFile, logFile
	} else {
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	}

	if err := cmd.Start(); err != nil {
		logf(logFile, "échec du lancement : %v", err)
		fmt.Fprintf(os.Stderr, "échec du lancement : %v\n", err)
		return 1
	}

	// Steam ferme le jeu en tuant le lanceur : on doit relayer le signal au
	// navigateur, sinon il survit en arrière-plan et le jeu suivant hérite
	// d'un profil verrouillé.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case s := <-sigs:
			logf(logFile, "signal %v reçu, transmission au navigateur", s)
			_ = cmd.Process.Signal(s)
		case <-done:
		}
	}()

	err = cmd.Wait()
	close(done)
	signal.Stop(sigs)

	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			logf(logFile, "navigateur terminé, code %d", ee.ExitCode())
			return ee.ExitCode()
		}
		logf(logFile, "erreur d'attente : %v", err)
		return 1
	}
	logf(logFile, "navigateur terminé normalement")
	return 0
}

// errUnknownGame permet à l'appelant de distinguer un slug inconnu (code de
// sortie 3, la convention de Steam) d'une autre erreur de préparation.
var errUnknownGame = errors.New("jeu-inconnu")

// prepareLaunch résout tout ce qu'il faut pour ouvrir un jeu : navigateur,
// profil, accès udev, arguments.
//
// Partagé par le lancement depuis Steam et par le bouton « Ouvrir » de
// l'interface : les deux chemins se comportent donc exactement pareil, y
// compris quand aucun serveur X n'est joignable.
func prepareLaunch(slug string, extra []string) (argv []string, logFile *os.File, logPath string, err error) {
	store, err := loadStore()
	if err != nil {
		return nil, nil, "", err
	}
	game, ok := store.Get(slug)
	if !ok {
		return nil, nil, "", fmt.Errorf("%w : %s", errUnknownGame, slug)
	}

	cfg, err := loadConfig()
	if err != nil {
		return nil, nil, "", err
	}

	pref := cfg.Browser
	if v := os.Getenv("SWL_BROWSER"); v != "" {
		pref = v
	}
	if game.Browser != "" {
		pref = game.Browser
	}
	b, err := browser.Detect(pref)
	if err != nil {
		return nil, nil, "", err
	}

	// Journal : lancé par Steam, le programme n'a pas de terminal, donc toute
	// explication doit atterrir dans un fichier consultable après coup.
	logFile, logPath = openLog(slug)
	if logFile != nil {
		logf(logFile, "%s %s : %s", appName, appVersion, game.Name)
		logf(logFile, "navigateur : %s", b.Describe())
	}

	// Best-effort : la manette ne fonctionnera pas sans cet accès, mais un
	// échec ici ne doit pas empêcher de lancer le jeu.
	if b.Kind == browser.KindFlatpak {
		changed, err := b.EnsureUdevOverride()
		switch {
		case err != nil:
			logf(logFile, "avertissement : accès /run/udev non appliqué (%v)", err)
		case changed:
			logf(logFile, "accès /run/udev accordé au sandbox flatpak")
		}
	}

	profile := cfg.ProfileDir
	if profile == "" {
		profile = profileDir()
	}
	if err := os.MkdirAll(profile, 0o755); err != nil {
		logf(logFile, "avertissement : dossier de profil %s : %v", profile, err)
	}

	allExtra := append([]string{}, cfg.ExtraArgs...)
	allExtra = append(allExtra, game.ExtraArgs...)
	allExtra = append(allExtra, extra...)

	webgpu := cfg.WebGPUArgs
	if webgpu == nil {
		webgpu = browser.DefaultWebGPUArgs
	}
	if adjusted, dropped := browser.AdjustForDisplay(webgpu); dropped {
		webgpu = adjusted
		logf(logFile, "avertissement : aucun DISPLAY, --ozone-platform=x11 retiré (WebGPU risque d'être indisponible)")
	}

	argv = b.Command(browser.LaunchSpec{
		URL:        game.URL,
		ProfileDir: profile,
		Kiosk:      true,
		WebGPUArgs: webgpu,
		ExtraArgs:  allExtra,
	})
	logf(logFile, "commande : %s", strings.Join(argv, " "))
	logf(logFile, "journal : %s", logPath)
	return argv, logFile, logPath, nil
}

// guiLaunch ouvre un jeu sans bloquer : l'interface HTTP doit répondre tout de
// suite, sinon la page attendrait la fin de la partie.
func guiLaunch(slug string) (string, error) {
	argv, logFile, logPath, err := prepareLaunch(slug, nil)
	if err != nil {
		return "", err
	}
	if logFile != nil {
		defer logFile.Close()
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	if logFile != nil {
		cmd.Stdout, cmd.Stderr = logFile, logFile
	} else {
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	}
	// Nouvelle session : le jeu survit à la fermeture de la fenêtre de
	// configuration et ne reçoit pas les signaux qui la visent.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logf(logFile, "échec du lancement : %v", err)
		return logPath, err
	}
	go func() { _ = cmd.Wait() }()
	return logPath, nil
}

// prefBrowser résout le navigateur souhaité : variable d'environnement
// d'abord, puis configuration.
func prefBrowser() string {
	pref := ""
	if cfg, err := loadConfig(); err == nil {
		pref = cfg.Browser
	}
	if v := os.Getenv("SWL_BROWSER"); v != "" {
		pref = v
	}
	return pref
}

// openLog ouvre (et tronque) le journal de lancement d'un jeu.
func openLog(slug string) (*os.File, string) {
	if err := os.MkdirAll(logsDir(), 0o755); err != nil {
		return nil, ""
	}
	path := filepath.Join(logsDir(), "launch-"+slug+".log")
	f, err := os.Create(path)
	if err != nil {
		return nil, ""
	}
	return f, path
}

func logf(f *os.File, format string, args ...any) {
	if f == nil {
		return
	}
	fmt.Fprintf(f, format+"\n", args...)
}
