package registry

import (
	"fmt"
	"hash/fnv"
	"net/url"
	"strings"
)

// ParseResult est le résultat de l'analyse d'une liste collée.
type ParseResult struct {
	Games  []Game
	Issues []string // lignes rejetées, avec numéro et raison
}

// Format accepté, une ligne par jeu :
//
//	Nom = https://exemple.com/jeu
//	Nom | https://exemple.com/jeu
//	Nom	https://exemple.com/jeu
//	https://exemple.com/jeu                 (le nom est déduit de l'URL)
//	Nom = https://exemple.com/jeu | --flag  (arguments Chrome supplémentaires)
//
// Les lignes vides et celles commençant par # ou // sont ignorées.
// existing permet d'écarter les URL déjà enregistrées (le slug reste unique).
func ParseList(text string, existing []string) ParseResult {
	var res ParseResult
	knownSlug := map[string]bool{}
	for _, s := range existing {
		knownSlug[s] = true
	}
	knownURL := map[string]bool{}

	for i, raw := range strings.Split(text, "\n") {
		num := i + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		name, rawURL, extras, err := splitLine(line)
		if err != nil {
			res.Issues = append(res.Issues, fmt.Sprintf("ligne %d : %s", num, err))
			continue
		}

		u, err := url.Parse(rawURL)
		if err != nil || u.Host == "" {
			res.Issues = append(res.Issues, fmt.Sprintf("ligne %d : URL invalide %q", num, rawURL))
			continue
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			res.Issues = append(res.Issues, fmt.Sprintf("ligne %d : schéma %q non supporté (http ou https attendu)", num, u.Scheme))
			continue
		}
		if knownURL[rawURL] {
			res.Issues = append(res.Issues, fmt.Sprintf("ligne %d : URL déjà présente dans cette liste, ignorée", num))
			continue
		}
		knownURL[rawURL] = true

		if name == "" {
			name = inferName(rawURL)
		}

		slug := uniqueSlug(Slugify(name), knownSlug)
		knownSlug[slug] = true

		res.Games = append(res.Games, Game{
			Slug:      slug,
			Name:      name,
			URL:       rawURL,
			ExtraArgs: extras,
		})
	}
	return res
}

// splitLine sépare le nom, l'URL et les éventuels arguments.
func splitLine(line string) (name, rawURL string, extras []string, err error) {
	left := ""
	right := line

	// On découpe sur le séparateur qui apparaît le plus tôt dans la ligne, pas
	// selon un ordre de priorité : sinon "Nom = URL | --flag" serait coupé sur
	// le second séparateur et l'URL resterait collée au nom. À index égal, le
	// séparateur le plus long gagne (" | " plutôt que "|").
	seps := []string{"	", " | ", " = ", "|"}
	bestIdx, bestLen := -1, 0
	for _, sep := range seps {
		idx := strings.Index(line, sep)
		if idx < 0 {
			continue
		}
		if bestIdx == -1 || idx < bestIdx || (idx == bestIdx && len(sep) > bestLen) {
			bestIdx, bestLen = idx, len(sep)
		}
	}
	if bestIdx >= 0 {
		left = line[:bestIdx]
		right = line[bestIdx+bestLen:]
	}

	fields := strings.Fields(right)
	idx := -1
	for i, f := range fields {
		if strings.HasPrefix(f, "http://") || strings.HasPrefix(f, "https://") {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", "", nil, fmt.Errorf("aucune URL http(s) trouvée")
	}

	rawURL = fields[idx]
	extras = fields[idx+1:]
	// Retire un séparateur résiduel quand la ligne est "Nom | URL | --flag".
	for len(extras) > 0 && (extras[0] == "|" || extras[0] == "=") {
		extras = extras[1:]
	}

	name = strings.TrimSpace(left)
	name = strings.Trim(name, "|=\"'	 ")
	return name, rawURL, extras, nil
}

// inferName déduit un nom lisible d'une URL : hôte sans "www." suivi du
// chemin, ce qui donne "exemple.com/jeu" pour https://exemple.com/jeu.
func inferName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	host := strings.TrimPrefix(u.Host, "www.")
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return host
	}
	return host + "/" + path
}

var accentFolder = strings.NewReplacer(
	"à", "a", "â", "a", "ä", "a", "á", "a", "ã", "a", "å", "a",
	"ç", "c",
	"è", "e", "é", "e", "ê", "e", "ë", "e",
	"ì", "i", "í", "i", "î", "i", "ï", "i",
	"ò", "o", "ó", "o", "ô", "o", "õ", "o", "ö", "o",
	"ù", "u", "ú", "u", "û", "u", "ü", "u",
	"ý", "y", "ÿ", "y",
	"ñ", "n",
	"œ", "oe", "æ", "ae",
	"ß", "ss",
)

// Slugify produit un identifiant utilisable dans une ligne de commande et
// dans un nom de fichier.
func Slugify(s string) string {
	lowered := accentFolder.Replace(strings.ToLower(s))

	var b strings.Builder
	dash := true // évite un tiret en tête
	for _, r := range lowered {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")

	// Un nom entièrement non latin ne doit pas produire un slug vide ni un
	// slug identique pour deux jeux différents, donc on garde une empreinte.
	if out == "" {
		h := fnv.New32a()
		h.Write([]byte(s))
		out = fmt.Sprintf("jeu-%06x", h.Sum32()&0xFFFFFF)
	}
	if len(out) > 48 {
		out = strings.Trim(out[:48], "-")
	}
	return out
}

func uniqueSlug(base string, taken map[string]bool) string {
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
