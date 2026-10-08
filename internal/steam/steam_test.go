package steam

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"deckwebgpu/internal/vdf"
)

// Les valeurs attendues sont calculées indépendamment (zlib.crc32 côté
// Python), ce qui permet de détecter une erreur d'ordre des opérandes ou
// d'encodage plutôt que de seulement vérifier que le code est d'accord
// avec lui-même.
func TestAppIDReference(t *testing.T) {
	cases := []struct {
		exe, name string
		want      uint32
	}{
		{"", "", 0x80000000},
		{"/home/deck/.local/share/steam-webgpu/bin/swl", "Mon Jeu", 0xa1b066b8},
		{"/usr/bin/swl", "Test", 0xb96761cf},
	}
	for _, c := range cases {
		if got := AppID(c.exe, c.name); got != c.want {
			t.Errorf("AppID(%q, %q) = 0x%08x, attendu 0x%08x", c.exe, c.name, got, c.want)
		}
	}
}

func TestAccountID(t *testing.T) {
	// 76561198000000000 - 76561197960265728 = 39734272
	got, ok := accountID(76561198000000000)
	if !ok || got != 39734272 {
		t.Fatalf("accountID = %d (ok=%v), attendu 39734272", got, ok)
	}
	if _, ok := accountID(0); ok {
		t.Fatal("un identifiant trop petit doit être refusé")
	}
	if _, ok := accountID(steamID64Base); ok {
		t.Fatal("un compte nul doit être refusé")
	}
}

func TestLastActiveSteamID64(t *testing.T) {
	// Reproduit la forme réelle de config/loginusers.vdf.
	data := []byte(`
"users"
{
	"76561198000000001"
	{
		"AccountName"		"vieux"
		"PersonaName"		"Vieux"
		"MostRecent"		"0"
		"Timestamp"		"1600000000"
	}
	"76561198000000000"
	{
		"AccountName"		"actif"
		"PersonaName"		"Actif"
		"MostRecent"		"1"
		"Timestamp"		"1700000000"
	}
}
`)
	got, ok := lastActiveSteamID64(data)
	if !ok {
		t.Fatal("aucun compte actif détecté")
	}
	if got != 76561198000000000 {
		t.Fatalf("SteamID64 = %d, attendu 76561198000000000", got)
	}
}

func TestLastActiveSteamID64Absent(t *testing.T) {
	if _, ok := lastActiveSteamID64([]byte(`"users" { "1" { "MostRecent" "0" } }`)); ok {
		t.Fatal("aucun compte ne doit être retourné si MostRecent vaut 0 partout")
	}
}

func TestShortcutsLifecycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "userdata", "123", "config", "shortcuts.vdf")

	f, existed, err := LoadShortcuts(path)
	if err != nil {
		t.Fatalf("LoadShortcuts: %v", err)
	}
	if existed {
		t.Fatal("le fichier ne devrait pas exister")
	}

	s := NewShortcut("Mon Jeu", "/usr/bin/swl", "/usr/bin", "launch mon-jeu", "/tmp/i.png")
	if !f.Add(s) {
		t.Fatal("ajout refusé")
	}
	if f.Add(s) {
		t.Fatal("un doublon ne doit pas être ajouté")
	}
	if f.Count() != 1 {
		t.Fatalf("%d entrées, attendu 1", f.Count())
	}
	if err := f.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Relecture : le fichier doit être un VDF binaire valide et lisible.
	g, existed, err := LoadShortcuts(path)
	if err != nil {
		t.Fatalf("relecture: %v", err)
	}
	if !existed || g.Count() != 1 {
		t.Fatalf("relecture: existed=%v count=%d", existed, g.Count())
	}
	if !g.Exists(s.AppID) {
		t.Fatal("l'appid relu ne correspond pas")
	}

	// Suppression
	if n := g.RemoveByAppID(s.AppID); n != 1 {
		t.Fatalf("%d entrées supprimées, attendu 1", n)
	}
	if g.Count() != 0 {
		t.Fatalf("il reste %d entrées", g.Count())
	}
}

// TestShortcutsPreserveExistingEntries est le test qui protège la bibliothèque
// de l'utilisateur : les raccourcis déjà présents, y compris ceux que Steam a
// écrits et dont nous ignorons les champs, doivent ressortir intacts.
func TestShortcutsPreserveExistingEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shortcuts.vdf")

	// Fichier d'origine avec un champ exotique que nous ne modélisons pas.
	existing := NewShortcutsFile(path)
	existing.Add(&Shortcut{
		AppID: 0xDEADBEEF, AppName: "Existant", Exe: `"/usr/bin/true"`,
		StartDir: "/usr/bin", LaunchOptions: "--x", AllowDesktopConfig: 1,
		AllowOverlay: 1, Tags: []string{"Ancien"},
	})
	// Ajoute un champ que le code ne connaît pas, pour vérifier la préservation.
	obj := existing.doc.Root.Entries[0].Value.Object
	obj.AddString("ChampInconnu", "ne pas perdre")
	obj.AddInt("CompteurBizarre", 0xCAFEBABE)
	if err := existing.Save(); err != nil {
		t.Fatalf("Save initial: %v", err)
	}
	before, _ := os.ReadFile(path)

	// Charge, ajoute un second raccourci, réécrit.
	f, _, err := LoadShortcuts(path)
	if err != nil {
		t.Fatalf("LoadShortcuts: %v", err)
	}
	f.Add(NewShortcut("Nouveau", "/usr/bin/swl", "/usr/bin", "launch nouveau", ""))
	if err := f.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	after, _ := os.ReadFile(path)
	doc, err := vdf.Parse(after)
	if err != nil {
		t.Fatalf("le fichier réécrit n'est pas un VDF valide: %v", err)
	}
	if len(doc.Root.Entries) != 2 {
		t.Fatalf("%d entrées après ajout, attendu 2", len(doc.Root.Entries))
	}

	first := doc.Root.Entries[0].Value.Object
	if v, ok := first.StringValue("ChampInconnu"); !ok || v != "ne pas perdre" {
		t.Fatalf("champ inconnu perdu (ok=%v, valeur=%q)", ok, v)
	}
	if v, ok := first.UintValue("CompteurBizarre"); !ok || v != 0xCAFEBABE {
		t.Fatalf("entier inconnu perdu (ok=%v, valeur=0x%08x)", ok, v)
	}
	if v, ok := first.UintValue("appid"); !ok || v != 0xDEADBEEF {
		t.Fatalf("appid existant perdu")
	}
	tags := first.Get("tags").Value.Object
	if len(tags.Entries) != 1 || tags.Entries[0].Value.Str != "Ancien" {
		t.Fatalf("les tags existants ont été abîmés: %+v", tags.Entries)
	}

	// La sauvegarde de l'original doit exister.
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("sauvegarde absente: %v", err)
	}
	if !bytes.Contains(before, []byte("Existant")) {
		t.Fatal("le fichier d'origine ne contenait pas le raccourci attendu")
	}
}

func TestShortcutQuoting(t *testing.T) {
	// L'appid se calcule sur le chemin nu, mais le champ Exe est quoté
	// seulement s'il contient une espace.
	withSpace := NewShortcut("Jeu", "/home/deck/Mes Jeux/swl", "/home/deck/Mes Jeux", "launch jeu", "")
	if withSpace.Exe != `"/home/deck/Mes Jeux/swl"` {
		t.Fatalf("Exe = %s", withSpace.Exe)
	}
	if withSpace.AppID != AppID("/home/deck/Mes Jeux/swl", "Jeu") {
		t.Fatal("l'appid doit ignorer les guillemets ajoutés au champ Exe")
	}
	noSpace := NewShortcut("Jeu", "/home/deck/swl", "/home/deck", "launch jeu", "")
	if noSpace.Exe != "/home/deck/swl" {
		t.Fatalf("Exe = %s", noSpace.Exe)
	}
}

func TestMarshalReindexes(t *testing.T) {
	f := NewShortcutsFile("/dev/null")
	f.Add(NewShortcut("A", "/a", "/", "x", ""))
	f.Add(NewShortcut("B", "/b", "/", "y", ""))
	f.Add(NewShortcut("C", "/c", "/", "z", ""))
	f.RemoveByAppID(AppID("/b", "B"))
	_ = f.Marshal()
	for i, e := range f.doc.Root.Entries {
		if e.Key != strconv.Itoa(i) {
			t.Fatalf("clé %q à l'index %d", e.Key, i)
		}
	}
}
