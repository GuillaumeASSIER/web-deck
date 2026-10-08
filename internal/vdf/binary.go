// Package vdf implémente le format binaire de Valve (Binary VDF), celui
// que Steam utilise pour userdata/<id>/config/shortcuts.vdf.
//
// Principe directeur : on lit l'arbre tel qu'il est écrit, on le modifie
// en place, et on le réécrit. Les enregistrements existants ne sont jamais
// reconstruits depuis une structure de haut niveau, ce qui garantit qu'un
// raccourci ajouté par Steam puis relu par nous ressort à l'octet près.
//
// Un type inconnu provoque une erreur plutôt qu'une réécriture approximative :
// mieux vaut refuser d'écrire que corrompre la bibliothèque de l'utilisateur.
package vdf

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Identifiants de type du format binaire Valve.
const (
	TypeStart   = 0x00 // ouverture d'un objet
	TypeString  = 0x01
	TypeInt32   = 0x02
	TypeFloat32 = 0x03
	TypeUint64  = 0x07
	TypeEnd     = 0x08 // fermeture d'un objet
)

// Node est une valeur VDF.
type Node struct {
	Type   byte
	Str    string
	Int    uint32 // TypeInt32, bits bruts (l'appid est signé côté Steam)
	U64    uint64 // TypeUint64
	F32    float32
	Object *Object // non nil si Type == TypeStart
}

// Object est une liste ordonnée de paires clé/valeur.
// L'ordre est significatif pour rester fidèle au fichier d'origine.
type Object struct {
	Entries []*Entry
}

// Entry est une paire clé/valeur.
type Entry struct {
	Key   string
	Value *Node
}

// Get retourne l'entrée associée à une clé, ou nil.
func (o *Object) Get(key string) *Entry {
	for _, e := range o.Entries {
		if e.Key == key {
			return e
		}
	}
	return nil
}

// StringValue retourne la valeur d'une entrée si c'est une chaîne.
func (o *Object) StringValue(key string) (string, bool) {
	if e := o.Get(key); e != nil && e.Value != nil && e.Value.Type == TypeString {
		return e.Value.Str, true
	}
	return "", false
}

// UintValue retourne la valeur d'une entrée si c'est un entier 32 bits.
func (o *Object) UintValue(key string) (uint32, bool) {
	if e := o.Get(key); e != nil && e.Value != nil && e.Value.Type == TypeInt32 {
		return e.Value.Int, true
	}
	return 0, false
}

// AddString ajoute une chaîne en fin d'objet.
func (o *Object) AddString(key, val string) {
	o.Entries = append(o.Entries, &Entry{Key: key, Value: &Node{Type: TypeString, Str: val}})
}

// AddInt ajoute un entier 32 bits en fin d'objet.
func (o *Object) AddInt(key string, val uint32) {
	o.Entries = append(o.Entries, &Entry{Key: key, Value: &Node{Type: TypeInt32, Int: val}})
}

// AddObject ajoute un objet imbriqué en fin d'objet.
func (o *Object) AddObject(key string, obj *Object) {
	o.Entries = append(o.Entries, &Entry{Key: key, Value: &Node{Type: TypeStart, Object: obj}})
}

// Document est la racine d'un fichier : un objet nommé.
type Document struct {
	Name string
	Root *Object
}

// ---------------------------------------------------------------------------
// Lecture
// ---------------------------------------------------------------------------

type reader struct {
	buf []byte
	pos int
}

func (r *reader) u8() (byte, error) {
	if r.pos >= len(r.buf) {
		return 0, fmt.Errorf("vdf: fin de fichier inattendue à l'offset %d", r.pos)
	}
	b := r.buf[r.pos]
	r.pos++
	return b, nil
}

func (r *reader) take(n int) ([]byte, error) {
	if r.pos+n > len(r.buf) {
		return nil, fmt.Errorf("vdf: fin de fichier inattendue à l'offset %d (voulu %d octets)", r.pos, n)
	}
	b := r.buf[r.pos : r.pos+n]
	r.pos += n
	return b, nil
}

// cstring lit une chaîne terminée par un octet nul.
func (r *reader) cstring() (string, error) {
	start := r.pos
	for r.pos < len(r.buf) {
		if r.buf[r.pos] == 0x00 {
			s := string(r.buf[start:r.pos])
			r.pos++ // consomme le terminateur
			return s, nil
		}
		r.pos++
	}
	return "", fmt.Errorf("vdf: chaîne non terminée à partir de l'offset %d", start)
}

// Parse lit un fichier VDF binaire complet.
// La structure attendue est : 0x00 "nom" <corps> 0x08 0x08.
func Parse(data []byte) (*Document, error) {
	r := &reader{buf: data}

	t, err := r.u8()
	if err != nil {
		return nil, err
	}
	if t != TypeStart {
		return nil, fmt.Errorf("vdf: octet initial 0x%02x, objet attendu (0x00)", t)
	}
	name, err := r.cstring()
	if err != nil {
		return nil, err
	}
	body, err := r.body()
	if err != nil {
		return nil, err
	}
	if t, err := r.u8(); err != nil {
		return nil, err
	} else if t != TypeEnd {
		return nil, fmt.Errorf("vdf: fin de fichier attendue (0x08), trouvé 0x%02x", t)
	}
	if r.pos != len(data) {
		return nil, fmt.Errorf("vdf: %d octets résiduels après la fin du document", len(data)-r.pos)
	}
	return &Document{Name: name, Root: body}, nil
}

func (r *reader) body() (*Object, error) {
	obj := &Object{}
	for {
		t, err := r.u8()
		if err != nil {
			return nil, err
		}
		if t == TypeEnd {
			return obj, nil
		}
		key, err := r.cstring()
		if err != nil {
			return nil, err
		}

		node := &Node{Type: t}
		switch t {
		case TypeStart:
			child, err := r.body()
			if err != nil {
				return nil, err
			}
			node.Object = child
		case TypeString:
			s, err := r.cstring()
			if err != nil {
				return nil, err
			}
			node.Str = s
		case TypeInt32:
			b, err := r.take(4)
			if err != nil {
				return nil, err
			}
			node.Int = binary.LittleEndian.Uint32(b)
		case TypeFloat32:
			b, err := r.take(4)
			if err != nil {
				return nil, err
			}
			node.F32 = math.Float32frombits(binary.LittleEndian.Uint32(b))
		case TypeUint64:
			b, err := r.take(8)
			if err != nil {
				return nil, err
			}
			node.U64 = binary.LittleEndian.Uint64(b)
		default:
			return nil, fmt.Errorf("vdf: type 0x%02x non géré (clé %q) : écriture refusée pour ne pas corrompre le fichier", t, key)
		}
		obj.Entries = append(obj.Entries, &Entry{Key: key, Value: node})
	}
}

// ---------------------------------------------------------------------------
// Écriture
// ---------------------------------------------------------------------------

type writer struct {
	buf []byte
}

func (w *writer) u8(b byte) { w.buf = append(w.buf, b) }

func (w *writer) cstring(s string) {
	w.buf = append(w.buf, s...)
	w.buf = append(w.buf, 0x00)
}

// Marshal sérialise le document.
// Le format se termine par deux 0x08 : le premier ferme l'objet racine, le
// second marque la fin du fichier. En écrire un seul produit un fichier que
// Steam comme RPCS3 refusent.
func Marshal(doc *Document) []byte {
	w := &writer{}
	w.u8(TypeStart)
	w.cstring(doc.Name)
	writeBody(w, doc.Root)
	w.u8(TypeEnd) // fermeture de l'objet racine
	w.u8(TypeEnd) // fin du fichier
	return w.buf
}

func writeBody(w *writer, obj *Object) {
	for _, e := range obj.Entries {
		w.u8(e.Value.Type)
		w.cstring(e.Key)
		switch e.Value.Type {
		case TypeStart:
			writeBody(w, e.Value.Object)
			w.u8(TypeEnd)
		case TypeString:
			w.cstring(e.Value.Str)
		case TypeInt32:
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], e.Value.Int)
			w.buf = append(w.buf, b[:]...)
		case TypeFloat32:
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], math.Float32bits(e.Value.F32))
			w.buf = append(w.buf, b[:]...)
		case TypeUint64:
			var b [8]byte
			binary.LittleEndian.PutUint64(b[:], e.Value.U64)
			w.buf = append(w.buf, b[:]...)
		}
	}
}
