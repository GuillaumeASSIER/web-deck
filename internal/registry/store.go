package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Version du format de games.json.
const Version = 1

// Game est un jeu web enregistré par l'utilisateur.
type Game struct {
	Slug      string   `json:"slug"`
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	AppID     uint32   `json:"appid"`
	Exe       string   `json:"exe,omitempty"`     // chemin utilisé pour Steam au moment de l'ajout
	Browser   string   `json:"browser,omitempty"` // force un navigateur pour ce jeu
	ExtraArgs []string `json:"extra_args,omitempty"`
	Added     string   `json:"added,omitempty"` // date ISO 8601
}

// Store est le registre des jeux, persisté en JSON.
type Store struct {
	Version int    `json:"version"`
	Games   []Game `json:"games"`

	path string
}

// Load lit le registre. Un fichier absent ou vide donne un registre vide,
// ce qui évite de traiter le premier lancement comme une erreur.
func Load(path string) (*Store, error) {
	s := &Store{Version: Version, path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("lecture de %s : %w", path, err)
	}
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("%s : JSON invalide (%w)", path, err)
	}
	s.path = path
	return s, nil
}

// Save écrit le registre, trié par nom pour que le fichier reste lisible
// et les diffs stables.
func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	s.Version = Version
	sort.SliceStable(s.Games, func(i, j int) bool { return s.Games[i].Name < s.Games[j].Name })

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(data, '\n'), 0o644)
}

// Path retourne le chemin du registre.
func (s *Store) Path() string { return s.path }

// Get retourne un jeu par son slug.
func (s *Store) Get(slug string) (Game, bool) {
	for _, g := range s.Games {
		if g.Slug == slug {
			return g, true
		}
	}
	return Game{}, false
}

// Slugs retourne tous les slugs connus.
func (s *Store) Slugs() []string {
	out := make([]string, 0, len(s.Games))
	for _, g := range s.Games {
		out = append(out, g.Slug)
	}
	return out
}

// HasURL indique si une URL est déjà enregistrée.
func (s *Store) HasURL(u string) bool {
	_, ok := s.GetByURL(u)
	return ok
}

// GetByURL retourne un jeu par son URL.
func (s *Store) GetByURL(u string) (Game, bool) {
	for _, g := range s.Games {
		if g.URL == u {
			return g, true
		}
	}
	return Game{}, false
}

// Upsert remplace le jeu de même URL ou de même slug, ou l'ajoute.
// C'est ce qui rend "swl add" idempotent : réexécuter la commande après un
// échec partiel met à jour au lieu de créer un doublon.
func (s *Store) Upsert(g Game) (Game, error) {
	if g.Name == "" {
		return Game{}, fmt.Errorf("nom vide")
	}
	if g.URL == "" {
		return Game{}, fmt.Errorf("URL vide")
	}
	for i, existing := range s.Games {
		if existing.URL == g.URL || (g.Slug != "" && existing.Slug == g.Slug) {
			if g.Slug == "" {
				g.Slug = existing.Slug
			}
			if g.Added == "" {
				g.Added = existing.Added
			}
			s.Games[i] = g
			return g, nil
		}
	}
	if g.Slug == "" {
		g.Slug = Slugify(g.Name)
	}
	g.Slug = s.uniqueSlug(g.Slug)
	s.Games = append(s.Games, g)
	return g, nil
}

// Add enregistre un jeu, en garantissant l'unicité du slug.
func (s *Store) Add(g Game) (Game, error) {
	if g.Name == "" {
		return Game{}, fmt.Errorf("nom vide")
	}
	if g.URL == "" {
		return Game{}, fmt.Errorf("URL vide")
	}
	base := g.Slug
	if base == "" {
		base = Slugify(g.Name)
	}
	g.Slug = s.uniqueSlug(base)
	s.Games = append(s.Games, g)
	return g, nil
}

func (s *Store) uniqueSlug(base string) string {
	taken := map[string]bool{}
	for _, g := range s.Games {
		taken[g.Slug] = true
	}
	if !taken[base] {
		return base
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if !taken[cand] {
			return cand
		}
	}
}

// Remove supprime un jeu et retourne true s'il existait.
func (s *Store) Remove(slug string) bool {
	kept := s.Games[:0]
	removed := false
	for _, g := range s.Games {
		if g.Slug == slug {
			removed = true
			continue
		}
		kept = append(kept, g)
	}
	s.Games = kept
	return removed
}
