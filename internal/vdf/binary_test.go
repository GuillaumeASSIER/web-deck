package vdf

import (
	"bytes"
	"testing"
)

// fixture construit un fichier de test dont la forme reproduit ce que Steam
// écrit réellement : racine nommée, entrées indexées par une chaîne, objet
// "tags" imbriqué, et un entier dont le bit de poids fort est à 1 (les appid
// de raccourcis non-Steam, lus comme signés côté Steam).
func fixture(t *testing.T) []byte {
	t.Helper()
	root := &Object{}
	root.AddString("comment-ce-que-je-veux", "ignoré")

	e0 := &Object{}
	e0.AddInt("appid", 0x91234567)
	e0.AddString("AppName", "Mon Jeu")
	e0.AddString("Exe", "/home/deck/.local/share/steam-webgpu/bin/swl")
	e0.AddString("StartDir", "/home/deck/.local/share/steam-webgpu/bin")
	e0.AddString("icon", "")
	e0.AddString("ShortcutPath", "")
	e0.AddString("LaunchOptions", "launch mon-jeu")
	e0.AddInt("IsHidden", 0)
	e0.AddInt("AllowDesktopConfig", 1)
	e0.AddInt("AllowOverlay", 1)
	e0.AddInt("OpenVR", 0)
	e0.AddInt("Devkit", 0)
	e0.AddString("DevkitGameID", "")
	e0.AddInt("DevkitOverrideAppID", 0)
	e0.AddInt("LastPlayTime", 0)
	e0.AddString("FlatpakAppID", "")
	e0.AddString("sortas", "")
	tags := &Object{}
	tags.AddString("0", "Web")
	tags.AddString("1", "Mes jeux")
	e0.AddObject("tags", tags)

	e1 := &Object{}
	e1.AddInt("appid", 0x80000001)
	e1.AddString("AppName", "Autre")
	e1.AddString("Exe", "/usr/bin/true")
	e1.AddObject("tags", &Object{})

	root.Entries = append(root.Entries, &Entry{Key: "0", Value: &Node{Type: TypeStart, Object: e0}})
	root.Entries = append(root.Entries, &Entry{Key: "1", Value: &Node{Type: TypeStart, Object: e1}})

	return Marshal(&Document{Name: "shortcuts", Root: root})
}

func TestRoundTripExact(t *testing.T) {
	raw := fixture(t)
	doc, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Name != "shortcuts" {
		t.Fatalf("nom racine = %q, attendu shortcuts", doc.Name)
	}
	out := Marshal(doc)
	if !bytes.Equal(raw, out) {
		t.Fatalf("aller-retour non identique (%d octets avant, %d après)", len(raw), len(out))
	}
}

func TestParseRejectsUnknownType(t *testing.T) {
	// 0x00 "shortcuts" 0x00 0x04 "clé" 0x00 ... => type Pointer non géré.
	raw := []byte{0x00}
	raw = append(raw, []byte("shortcuts")...)
	raw = append(raw, 0x00)
	raw = append(raw, 0x04)
	raw = append(raw, []byte("cle")...)
	raw = append(raw, 0x00)
	if _, err := Parse(raw); err == nil {
		t.Fatal("un type non géré doit provoquer une erreur")
	}
}

func TestParseRejectsTruncated(t *testing.T) {
	raw := fixture(t)
	if _, err := Parse(raw[:len(raw)-3]); err == nil {
		t.Fatal("un fichier tronqué doit provoquer une erreur")
	}
}

func TestParseRejectsTrailingGarbage(t *testing.T) {
	raw := append(fixture(t), 0x42)
	if _, err := Parse(raw); err == nil {
		t.Fatal("des octets après la fin du document doivent provoquer une erreur")
	}
}

func TestValuesSurvive(t *testing.T) {
	doc, err := Parse(fixture(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(doc.Root.Entries) != 3 {
		t.Fatalf("%d entrées, attendu 3", len(doc.Root.Entries))
	}
	first := doc.Root.Entries[1].Value.Object
	if v, ok := first.UintValue("appid"); !ok || v != 0x91234567 {
		t.Fatalf("appid relu = 0x%08x (ok=%v)", v, ok)
	}
	if v, ok := first.StringValue("AppName"); !ok || v != "Mon Jeu" {
		t.Fatalf("AppName relu = %q (ok=%v)", v, ok)
	}
	tags := first.Get("tags").Value.Object
	if len(tags.Entries) != 2 {
		t.Fatalf("tags: %d entrées, attendu 2", len(tags.Entries))
	}
}
