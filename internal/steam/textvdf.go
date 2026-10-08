package steam

import (
	"strconv"
)

// Ce fichier contient un lecteur minimal de VDF textuel (le format lisible
// de Valve, utilisé par loginusers.vdf). On n'a besoin que de parcourir
// l'arbre pour retrouver le dernier compte utilisé.

type textValue struct {
	Str      string
	Children map[string]*textValue
}

func tokenizeText(s string) []string {
	var toks []string
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '"':
			j := i + 1
			var b []byte
			for j < len(s) && s[j] != '"' {
				if s[j] == '\\' && j+1 < len(s) {
					j++
					b = append(b, s[j])
					j++
					continue
				}
				b = append(b, s[j])
				j++
			}
			toks = append(toks, string(b))
			i = j + 1
		case c == '{' || c == '}':
			toks = append(toks, string(c))
			i++
		default:
			i++ // espaces et commentaires ignorés
		}
	}
	return toks
}

// parseTextTree construit l'arbre à partir d'une liste de jetons.
func parseTextTree(toks []string, pos *int) map[string]*textValue {
	obj := map[string]*textValue{}
	for *pos < len(toks) {
		key := toks[*pos]
		if key == "}" {
			*pos++
			return obj
		}
		*pos++
		if *pos >= len(toks) {
			return obj
		}
		if toks[*pos] == "{" {
			*pos++
			obj[key] = &textValue{Children: parseTextTree(toks, pos)}
			continue
		}
		obj[key] = &textValue{Str: toks[*pos]}
		*pos++
	}
	return obj
}

// lastActiveSteamID64 retourne le SteamID64 du compte marqué MostRecent.
// Si plusieurs comptes portent la marque, on garde le plus grand, ce qui
// correspond au dernier connecté dans la quasi-totalité des cas.
func lastActiveSteamID64(data []byte) (uint64, bool) {
	pos := 0
	root := parseTextTree(tokenizeText(string(data)), &pos)
	users, ok := root["users"]
	if !ok || users.Children == nil {
		return 0, false
	}
	var best uint64
	found := false
	for id, u := range users.Children {
		if u.Children == nil {
			continue
		}
		recent, ok := u.Children["MostRecent"]
		if !ok || recent.Str != "1" {
			continue
		}
		v, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			continue
		}
		if !found || v > best {
			best, found = v, true
		}
	}
	return best, found
}

// accountID convertit un SteamID64 en identifiant de compte (32 bits) utilisé
// dans les chemins userdata/.
func accountID(steamID64 uint64) (uint32, bool) {
	if steamID64 < steamID64Base {
		return 0, false
	}
	id := steamID64 - steamID64Base
	if id == 0 || id > 0xFFFFFFFF {
		return 0, false
	}
	return uint32(id), true
}
