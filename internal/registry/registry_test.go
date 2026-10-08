package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseListNominal(t *testing.T) {
	text := `
# commentaire ignoré
Mon Jeu = https://exemple.com/jeu
Autre Jeu | https://autre.example.org
https://troisieme.example.net
Quatrième	https://exemple.com/4
Cinquième = https://exemple.com/5 | --force-device-scale-factor=1
`
	res := ParseList(text, nil)
	if len(res.Issues) != 0 {
		t.Fatalf("problèmes inattendus : %v", res.Issues)
	}
	if len(res.Games) != 5 {
		t.Fatalf("%d jeux analysés, attendu 5", len(res.Games))
	}

	if res.Games[0].Name != "Mon Jeu" || res.Games[0].URL != "https://exemple.com/jeu" {
		t.Fatalf("ligne 1 mal analysée : %+v", res.Games[0])
	}
	if res.Games[0].Slug != "mon-jeu" {
		t.Fatalf("slug = %q, attendu mon-jeu", res.Games[0].Slug)
	}
	// Le nom est déduit de l'URL quand il n'est pas fourni.
	if res.Games[2].Name != "troisieme.example.net" {
		t.Fatalf("nom déduit = %q", res.Games[2].Name)
	}
	// Les arguments supplémentaires sont séparés du reste.
	if len(res.Games[4].ExtraArgs) != 1 || res.Games[4].ExtraArgs[0] != "--force-device-scale-factor=1" {
		t.Fatalf("arguments = %v", res.Games[4].ExtraArgs)
	}
}

func TestParseListRejette(t *testing.T) {
	text := `
pas une url
Nom = ftp://exemple.com/fichier
https://exemple.com/2
https://exemple.com/2
`
	res := ParseList(text, nil)
	if len(res.Games) != 1 {
		t.Fatalf("%d jeux, attendu 1 (doublon et lignes invalides rejetés)", len(res.Games))
	}
	if len(res.Issues) != 3 {
		t.Fatalf("%d problèmes signalés, attendu 3 : %v", len(res.Issues), res.Issues)
	}
}

func TestParseListSlugUnique(t *testing.T) {
	text := "Mon Jeu = https://a.example.com\nMon Jeu = https://b.example.com\n"
	res := ParseList(text, nil)
	if len(res.Games) != 2 {
		t.Fatalf("%d jeux", len(res.Games))
	}
	if res.Games[0].Slug == res.Games[1].Slug {
		t.Fatalf("slugs identiques : %q", res.Games[0].Slug)
	}
	if res.Games[1].Slug != "mon-jeu-2" {
		t.Fatalf("slug du second = %q", res.Games[1].Slug)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Mon Jeu":                 "mon-jeu",
		"Été à Rennes":            "ete-a-rennes",
		"  espaces  partout  ":    "espaces-partout",
		"Nom_avec.traits/et:tout": "nom-avec-traits-et-tout",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, attendu %q", in, got, want)
		}
	}
	// Un nom non latin doit produire un slug stable et non vide.
	a, b := Slugify("ゲーム"), Slugify("ゲーム")
	if a == "" || a != b {
		t.Fatalf("slug non déterministe pour un nom non latin : %q / %q", a, b)
	}
	if Slugify("別のゲーム") == a {
		t.Fatal("deux noms non latins distincts doivent donner deux slugs distincts")
	}
}

func TestStoreUpsertIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "games.json")
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	g := Game{Name: "Mon Jeu", URL: "https://exemple.com/jeu", AppID: 1}
	g1, _ := s.Upsert(g)
	if len(s.Games) != 1 {
		t.Fatalf("%d jeux", len(s.Games))
	}
	// Deuxième passage : même URL, appid recalculé, pas de doublon.
	g.AppID = 2
	g1b, _ := s.Upsert(g)
	if len(s.Games) != 1 {
		t.Fatalf("%d jeux après second passage, attendu 1", len(s.Games))
	}
	if g1b.Slug != g1.Slug {
		t.Fatalf("slug modifié : %q -> %q", g1.Slug, g1b.Slug)
	}
	if s.Games[0].AppID != 2 {
		t.Fatalf("appid non mis à jour : %d", s.Games[0].AppID)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Relire doit donner exactement la même chose.
	s2, err := Load(path)
	if err != nil {
		t.Fatalf("relecture: %v", err)
	}
	if len(s2.Games) != 1 || s2.Games[0].URL != "https://exemple.com/jeu" {
		t.Fatalf("relecture incohérente : %+v", s2.Games)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fichier absent : %v", err)
	}
}
