package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"deckwebgpu/internal/artwork"
	"deckwebgpu/internal/registry"
	"deckwebgpu/internal/steam"
)

// errSteamRunning est le seul refus non négociable de l'outil. C'est une
// sentinelle pour que l'interface puisse proposer l'action qui débloque la
// situation (fermer Steam) au lieu d'afficher un message d'erreur sec.
var errSteamRunning = errors.New("steam-tourne")

// defaultList est proposé quand aucun jeu n'est encore enregistré : deux jeux
// jouables tout de suite, ce qui permet de valider la chaîne complète avant
// d'ajouter les siens.
const defaultList = `Skate 3 = https://skate.aaddpp.lol/
Black Ops 1 Zombies = https://vel.gg/bo1z
`

// addOptions regroupe les réglages partagés par "add", le mode terminal et
// l'interface.
type addOptions struct {
	user      uint
	force     bool
	dryRun    bool
	noArtwork bool
	browser   string
	yes       bool
}

// addOutcome résume ce qui a été écrit. La sortie détaillée est écrite dans un
// io.Writer, ce qui permet à l'interface de la renvoyer telle quelle.
type addOutcome struct {
	Added   []string
	Skipped []string
	Path    string
	User    uint32
	DryRun  bool
}

// ---------------------------------------------------------------------------
// swl  (mode terminal, en secours de l'interface)
// ---------------------------------------------------------------------------

// cmdInteractive lit une liste sur l'entrée standard. Le collage se termine
// par Ctrl-D, ce qui évite d'inventer un marqueur de fin à retenir.
func cmdInteractive(args []string) int {
	fs := flag.NewFlagSet("swl", flag.ContinueOnError)
	opts := bindAddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if isTerminal(os.Stdin) {
		fmt.Println("Colle ta liste de jeux, une ligne par jeu. Format : Nom = https://url")
		fmt.Println("Termine par Ctrl-D (ou Ctrl-Z puis Entrée sous Windows).")
		fmt.Println()
	}
	text, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lecture de l'entrée standard : %v\n", err)
		return 1
	}
	if strings.TrimSpace(string(text)) == "" {
		// Rien à coller : proposer la liste d'exemple plutôt que d'échouer.
		text = []byte(defaultList)
		fmt.Println("Aucune liste fournie, exemple proposé :")
		fmt.Println(strings.TrimRight(defaultList, "\n"))
		fmt.Println()
	}
	return addFromText(string(text), *opts, true)
}

// ---------------------------------------------------------------------------
// swl add
// ---------------------------------------------------------------------------

func cmdAdd(args []string) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	file := fs.String("f", "", "fichier contenant la liste")
	fs.StringVar(file, "file", "", "fichier contenant la liste")
	opts := bindAddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var text string
	if *file != "" {
		data, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "lecture de %s : %v\n", *file, err)
			return 1
		}
		text = string(data)
	} else {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "lecture de l'entrée standard : %v\n", err)
			return 1
		}
		text = string(data)
	}
	if strings.TrimSpace(text) == "" {
		fmt.Fprintln(os.Stderr, "aucune liste fournie (utilise swl add -f fichier, ou swl gui)")
		return 1
	}
	return addFromText(text, *opts, true)
}

// bindAddFlags déclare les options communes à "swl" et à "swl add".
// "yes" et "y" partagent la même variable : deux noms, un seul indicateur.
func bindAddFlags(fs *flag.FlagSet) *addOptions {
	o := &addOptions{}
	fs.UintVar(&o.user, "user", 0, "identifiant de compte Steam (userdata/<id>)")
	fs.BoolVar(&o.force, "force", false, "écrire même si Steam tourne")
	fs.BoolVar(&o.dryRun, "dry-run", false, "ne rien écrire, seulement afficher")
	fs.BoolVar(&o.noArtwork, "no-artwork", false, "ne pas générer les vignettes")
	fs.StringVar(&o.browser, "browser", "", "navigateur à utiliser")
	fs.BoolVar(&o.yes, "yes", false, "ne pas demander de confirmation")
	fs.BoolVar(&o.yes, "y", false, "ne pas demander de confirmation")
	_ = fs
	return o
}

// confirmYes récupère la réponse au clavier. En mode non interactif (lancé par
// Steam, script, cron), /dev/tty est indisponible : on ne devine pas, on refuse.
func confirmYes(question string) bool {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	fmt.Fprintf(f, "%s [o/N] ", question)
	line, _ := bufio.NewReader(f).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "o", "oui", "y", "yes":
		return true
	}
	return false
}

// isTerminal indique si le descripteur est un terminal (ou /dev/null), par
// opposition à un tube ou un fichier. Sert à distinguer un collage d'une liste
// d'un simple lancement de l'interface.
func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// ---------------------------------------------------------------------------
// Traitement commun
// ---------------------------------------------------------------------------

func addFromText(text string, o addOptions, allowPrompt bool) int {
	store, err := loadStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}

	res := registry.ParseList(text, store.Slugs())
	for _, issue := range res.Issues {
		fmt.Fprintf(os.Stderr, "avertissement : %s\n", issue)
	}
	if len(res.Games) == 0 {
		fmt.Fprintln(os.Stderr, "aucun jeu valide dans la liste.")
		return 1
	}

	fmt.Printf("\n%d jeu(x) à ajouter :\n\n", len(res.Games))
	fmt.Printf("  %-28s %s\n", "NOM", "URL")
	for _, g := range res.Games {
		fmt.Printf("  %-28s %s\n", truncate(g.Name, 28), g.URL)
	}
	fmt.Println()

	if allowPrompt && !o.dryRun && !o.yes && !confirmYes("Écrire ces raccourcis dans Steam ?") {
		fmt.Println("annulé.")
		return 1
	}

	outcome, err := addGames(res.Games, o, os.Stdout)
	if err != nil {
		if errors.Is(err, errSteamRunning) {
			printSteamRunningHelp(os.Stderr)
			return 1
		}
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if outcome.DryRun || len(outcome.Added) == 0 {
		return 0
	}
	printNextSteps(os.Stdout)
	return 0
}

// addGames écrit les raccourcis dans shortcuts.vdf puis met à jour le registre.
// Toute la sortie passe par out : le CLI y met stdout, l'interface y met un
// tampon qu'elle renvoie au navigateur.
func addGames(games []registry.Game, o addOptions, out io.Writer) (addOutcome, error) {
	var res addOutcome
	res.DryRun = o.dryRun

	root, err := steam.FindRoot()
	if err != nil {
		return res, err
	}

	var user uint32
	if o.user != 0 {
		user = uint32(o.user)
		if !fileExists(filepath.Join(root, "userdata", fmt.Sprint(user))) {
			return res, fmt.Errorf("le profil %d n'existe pas dans %s/userdata", user, root)
		}
	} else {
		user, err = steam.LastActiveUser(root)
		if err != nil {
			return res, err
		}
	}
	res.User = user

	// Le point de non-retour : Steam relit shortcuts.vdf à son démarrage et le
	// réécrit depuis sa mémoire à sa fermeture. Écrire pendant qu'il tourne
	// perd les modifications sans le moindre message d'erreur.
	if steam.IsRunning(root) {
		if !o.force {
			return res, errSteamRunning
		}
		fmt.Fprintln(out, "avertissement : Steam tourne, écriture forcée, les changements seront probablement écrasés.")
	}

	exe := installedBinary()
	if !fileExists(exe) {
		fmt.Fprintf(out, "avertissement : %s est absent. Lance d'abord : swl install\n", exe)
	}
	startDir := filepath.Dir(exe)

	res.Path = steam.ShortcutsPath(root, user)
	sf, existed, err := steam.LoadShortcuts(res.Path)
	if err != nil {
		return res, err
	}
	if !existed {
		fmt.Fprintf(out, "shortcuts.vdf absent pour le profil %d, il sera créé.\n", user)
	} else {
		fmt.Fprintf(out, "%d raccourci(s) déjà présents pour le profil %d.\n", sf.Count(), user)
	}

	if !o.dryRun {
		if err := os.MkdirAll(iconsDir(), 0o755); err != nil {
			return res, fmt.Errorf("création de %s : %w", iconsDir(), err)
		}
	}

	store, err := loadStore()
	if err != nil {
		return res, err
	}

	for i := range games {
		g := &games[i]

		// Idempotence : si l'URL est déjà connue, on réutilise son slug pour
		// qu'une seconde exécution mette à jour au lieu de dupliquer.
		if prev, ok := store.GetByURL(g.URL); ok {
			g.Slug = prev.Slug
		}

		iconPath := filepath.Join(iconsDir(), g.Slug+".png")
		if !o.dryRun && !o.noArtwork {
			if err := artwork.Icon(iconPath, g.Name); err != nil {
				fmt.Fprintf(out, "vignette %s : %v\n", iconPath, err)
			}
		}

		sc := steam.NewShortcut(g.Name, exe, startDir, "launch "+g.Slug, iconPath)
		g.AppID = sc.AppID
		g.Exe = exe
		if g.Browser == "" {
			g.Browser = o.browser
		}

		if sf.Exists(sc.AppID) {
			fmt.Fprintf(out, "  = %-28s déjà dans Steam, ignoré\n", truncate(g.Name, 28))
			res.Skipped = append(res.Skipped, g.Name)
			continue
		}

		if o.dryRun {
			fmt.Fprintf(out, "  + %-28s %s (appid %d)\n", truncate(g.Name, 28), g.URL, sc.AppID)
			res.Added = append(res.Added, g.Name)
			continue
		}

		sf.Add(sc)
		if !o.noArtwork {
			if _, err := artwork.Grid(filepath.Join(steam.ConfigDir(root, user), "grid"), sc.AppID, g.Name); err != nil {
				fmt.Fprintf(out, "vignettes de bibliothèque : %v\n", err)
			}
		}
		if _, err := store.Upsert(*g); err != nil {
			fmt.Fprintf(out, "registre : %v\n", err)
		}
		fmt.Fprintf(out, "  + %-28s %s\n", truncate(g.Name, 28), g.URL)
		res.Added = append(res.Added, g.Name)
	}

	if o.dryRun {
		fmt.Fprintf(out, "\n--dry-run : %d ajout(s) simulé(s), rien n'a été écrit.\n", len(res.Added))
		return res, nil
	}
	if len(res.Added) == 0 {
		fmt.Fprintln(out, "\nrien de nouveau à ajouter.")
		return res, nil
	}

	if err := sf.Save(); err != nil {
		return res, fmt.Errorf("écriture de %s : %w", res.Path, err)
	}
	if err := store.Save(); err != nil {
		return res, fmt.Errorf("écriture du registre : %w", err)
	}

	fmt.Fprintf(out, "\n%d raccourci(s) écrit(s) dans %s\n", len(res.Added), res.Path)
	if len(res.Skipped) > 0 {
		fmt.Fprintf(out, "%d ignoré(s) car déjà présent(s).\n", len(res.Skipped))
	}
	fmt.Fprintln(out, "Relance Steam pour voir les jeux en mode Bureau comme en mode Jeu.")
	return res, nil
}

func printSteamRunningHelp(w io.Writer) {
	fmt.Fprint(w, `Steam est en cours d'exécution : les raccourcis seraient perdus.

Steam recharge shortcuts.vdf à son démarrage et le réécrit à sa fermeture.
Ferme complètement Steam, puis relance l'opération :

  interface     : swl gui, puis le bouton « Fermer Steam »
  mode Bureau   : menu Steam, puis Quitter
  mode Jeu      : bouton Steam, puis Éteindre, puis Quitter Steam

--force écrit malgré tout, mais les changements seront effacés par Steam.
`)
}

func printNextSteps(w io.Writer) {
	fmt.Fprint(w, `
Il reste à faire, une fois :
  1. relance Steam
  2. lance le jeu depuis le mode Jeu avec le bouton A
  3. bouton Steam, Contrôleur, et vérifie qu'un modèle avec manette est actif

Si la manette n'est pas vue par le jeu : swl doctor
`)
}

// truncate raccourcit un nom pour l'affichage en colonnes.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// ---------------------------------------------------------------------------
// swl list / swl remove
// ---------------------------------------------------------------------------

func cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	store, err := loadStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if len(store.Games) == 0 {
		fmt.Println("aucun jeu enregistré. Utilise : swl gui  (ou swl pour le mode terminal)")
		return 0
	}
	games := append([]registry.Game(nil), store.Games...)
	sort.SliceStable(games, func(i, j int) bool { return games[i].Name < games[j].Name })

	fmt.Printf("%-28s %-24s %-12s %s\n", "NOM", "SLUG", "APPID", "URL")
	for _, g := range games {
		fmt.Printf("%-28s %-24s %-12d %s\n", truncate(g.Name, 28), g.Slug, g.AppID, g.URL)
	}
	fmt.Printf("\n%d jeu(x). Registre : %s\n", len(games), store.Path())
	return 0
}

func cmdRemove(args []string) int {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	force := fs.Bool("force", false, "retirer même si Steam tourne")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage : swl remove <slug> [<slug>...]")
		return 2
	}

	removed, err := removeGames(fs.Args(), *force, os.Stdout)
	if err != nil {
		if errors.Is(err, errSteamRunning) {
			fmt.Fprintln(os.Stderr, "Steam tourne : ferme-le d'abord (swl gui), ou utilise --force.")
			return 1
		}
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if removed > 0 {
		fmt.Println("\nRelance Steam pour que la bibliothèque soit à jour.")
	}
	return 0
}

// removeGames retire des raccourcis et met à jour le registre.
func removeGames(slugs []string, force bool, out io.Writer) (int, error) {
	store, err := loadStore()
	if err != nil {
		return 0, err
	}
	root, err := steam.FindRoot()
	if err != nil {
		return 0, err
	}
	user, err := steam.LastActiveUser(root)
	if err != nil {
		return 0, err
	}
	if steam.IsRunning(root) && !force {
		return 0, errSteamRunning
	}

	sf, _, err := steam.LoadShortcuts(steam.ShortcutsPath(root, user))
	if err != nil {
		return 0, err
	}

	removedTotal := 0
	for _, slug := range slugs {
		g, ok := store.Get(slug)
		if !ok {
			fmt.Fprintf(out, "slug inconnu : %s\n", slug)
			continue
		}
		n := sf.RemoveByAppID(g.AppID)
		removedTotal += n
		removeGrid(filepath.Join(steam.ConfigDir(root, user), "grid"), g.AppID)
		_ = os.Remove(filepath.Join(iconsDir(), g.Slug+".png"))
		store.Remove(slug)
		fmt.Fprintf(out, "  - %s (%d raccourci Steam retiré)\n", g.Name, n)
	}

	if removedTotal > 0 {
		if err := sf.Save(); err != nil {
			return removedTotal, err
		}
	}
	if err := store.Save(); err != nil {
		return removedTotal, err
	}
	return removedTotal, nil
}

// removeGrid supprime les visuels d'un jeu, sans échouer si l'un manque.
func removeGrid(dir string, appID uint32) {
	for _, suffix := range []string{"p.png", ".png", "_hero.png", "_icon.png", "_logo.png", "_library_600x900.jpg"} {
		_ = os.Remove(filepath.Join(dir, fmt.Sprintf("%d%s", appID, suffix)))
	}
}
