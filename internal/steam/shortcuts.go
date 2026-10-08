package steam

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"deckwebgpu/internal/vdf"
)

// Shortcut décrit un raccourci non-Steam tel que Steam l'écrit dans
// shortcuts.vdf. Les noms de champs reproduisent ceux du client Steam :
// "AppName" et "OpenVR" avec une majuscule, "icon" et "sortas" en minuscules.
// Une casse différente fonctionne souvent en écriture mais casse la
// détection des doublons, donc on s'aligne exactement.
type Shortcut struct {
	AppID               uint32
	AppName             string
	Exe                 string
	StartDir            string
	Icon                string
	ShortcutPath        string
	LaunchOptions       string
	IsHidden            uint32
	AllowDesktopConfig  uint32
	AllowOverlay        uint32
	OpenVR              uint32
	Devkit              uint32
	DevkitGameID        string
	DevkitOverrideAppID uint32
	LastPlayTime        uint32
	FlatpakAppID        string
	SortAs              string
	Tags                []string
}

// NewShortcut construit un raccourci avec les valeurs par défaut de Steam.
// L'appid est dérivé du chemin d'exécution et du nom : il doit donc être
// calculé avec le chemin NON quoté, celui qu'on stocke dans le champ Exe
// étant entre guillemets.
func NewShortcut(appName, exe, startDir, launchOptions, icon string) *Shortcut {
	return &Shortcut{
		AppID:              AppID(exe, appName),
		AppName:            appName,
		Exe:                quoteIfNeeded(exe),
		StartDir:           quoteIfNeeded(startDir),
		Icon:               quoteIfNeeded(icon),
		LaunchOptions:      launchOptions,
		AllowDesktopConfig: 1,
		AllowOverlay:       1,
	}
}

// quoteIfNeeded ajoute des guillemets uniquement si le chemin contient une
// espace. L'exécutable, lui, est toujours quoté : c'est ce que fait Steam.
func quoteIfNeeded(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			return `"` + s + `"`
		}
	}
	return s
}

func (s *Shortcut) node() *vdf.Node {
	o := &vdf.Object{}
	// L'ordre reproduit celui du client Steam et de RPCS3 : il n'est pas
	// fonctionnellement requis, mais garde les fichiers diffables.
	o.AddInt("appid", s.AppID)
	o.AddString("AppName", s.AppName)
	o.AddString("Exe", s.Exe)
	o.AddString("StartDir", s.StartDir)
	o.AddString("icon", s.Icon)
	o.AddString("ShortcutPath", s.ShortcutPath)
	o.AddString("LaunchOptions", s.LaunchOptions)
	o.AddInt("IsHidden", s.IsHidden)
	o.AddInt("AllowDesktopConfig", s.AllowDesktopConfig)
	o.AddInt("AllowOverlay", s.AllowOverlay)
	o.AddInt("OpenVR", s.OpenVR)
	o.AddInt("Devkit", s.Devkit)
	o.AddString("DevkitGameID", s.DevkitGameID)
	o.AddInt("DevkitOverrideAppID", s.DevkitOverrideAppID)
	o.AddInt("LastPlayTime", s.LastPlayTime)
	o.AddString("FlatpakAppID", s.FlatpakAppID)
	o.AddString("sortas", s.SortAs)

	tags := &vdf.Object{}
	for i, t := range s.Tags {
		tags.AddString(strconv.Itoa(i), t)
	}
	o.AddObject("tags", tags)

	return &vdf.Node{Type: vdf.TypeStart, Object: o}
}

// Summary décrit un raccourci existant, pour le diagnostic.
func summaryOf(e *vdf.Entry) string {
	if e.Value == nil || e.Value.Object == nil {
		return e.Key
	}
	o := e.Value.Object
	name, _ := o.StringValue("AppName")
	if name == "" {
		name, _ = o.StringValue("appname")
	}
	exe, _ := o.StringValue("Exe")
	return fmt.Sprintf("%s [%s]", name, exe)
}

// ShortcutsFile représente userdata/<id>/config/shortcuts.vdf.
type ShortcutsFile struct {
	doc    *vdf.Document
	path   string
	perms  os.FileMode
	exists bool
}

// ShortcutsPath retourne le chemin du fichier pour un compte donné.
func ShortcutsPath(root string, user uint32) string {
	return filepath.Join(ConfigDir(root, user), "shortcuts.vdf")
}

// NewShortcutsFile prépare un fichier vide (cas où shortcuts.vdf n'existe pas
// encore : Steam n'en crée pas avant qu'un premier raccourci soit ajouté à la
// main).
func NewShortcutsFile(path string) *ShortcutsFile {
	return &ShortcutsFile{
		doc:    &vdf.Document{Name: "shortcuts", Root: &vdf.Object{}},
		path:   path,
		perms:  0o644,
		exists: false,
	}
}

// LoadShortcuts lit le fichier s'il existe, sinon retourne un fichier vide.
// Le booléen indique si le fichier existait.
func LoadShortcuts(path string) (*ShortcutsFile, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewShortcutsFile(path), false, nil
		}
		return nil, false, fmt.Errorf("lecture de %s : %w", path, err)
	}
	doc, err := vdf.Parse(data)
	if err != nil {
		return nil, false, fmt.Errorf("analyse de %s : %w", path, err)
	}
	if doc.Name != "shortcuts" {
		return nil, false, fmt.Errorf("%s : objet racine %q, attendu \"shortcuts\"", path, doc.Name)
	}
	f := &ShortcutsFile{doc: doc, path: path, perms: 0o644, exists: true}
	if st, err := os.Stat(path); err == nil {
		f.perms = st.Mode().Perm()
	}
	return f, true, nil
}

// Path retourne le chemin du fichier.
func (f *ShortcutsFile) Path() string { return f.path }

// Existed indique si le fichier était présent avant chargement.
func (f *ShortcutsFile) Existed() bool { return f.exists }

// Count retourne le nombre d'entrées.
func (f *ShortcutsFile) Count() int { return len(f.doc.Root.Entries) }

// Summaries liste les raccourcis existants, pour l'affichage.
func (f *ShortcutsFile) Summaries() []string {
	var out []string
	for _, e := range f.doc.Root.Entries {
		out = append(out, summaryOf(e))
	}
	return out
}

// find retourne l'index de l'entrée correspondant à l'appid, ou -1.
func (f *ShortcutsFile) find(appID uint32) int {
	for i, e := range f.doc.Root.Entries {
		if e.Value == nil || e.Value.Object == nil {
			continue
		}
		if v, ok := e.Value.Object.UintValue("appid"); ok && v == appID {
			return i
		}
	}
	return -1
}

// Exists indique si l'appid est déjà présent.
func (f *ShortcutsFile) Exists(appID uint32) bool { return f.find(appID) >= 0 }

// AppIDs retourne tous les appid présents.
func (f *ShortcutsFile) AppIDs() []uint32 {
	var out []uint32
	for _, e := range f.doc.Root.Entries {
		if e.Value == nil || e.Value.Object == nil {
			continue
		}
		if v, ok := e.Value.Object.UintValue("appid"); ok {
			out = append(out, v)
		}
	}
	return out
}

// Add ajoute un raccourci. Le retour indique si l'ajout a eu lieu (faux si un
// raccourci portant le même appid est déjà présent).
func (f *ShortcutsFile) Add(s *Shortcut) bool {
	if f.Exists(s.AppID) {
		return false
	}
	f.doc.Root.Entries = append(f.doc.Root.Entries, &vdf.Entry{Key: "0", Value: s.node()})
	return true
}

// RemoveByAppID supprime toutes les entrées portant cet appid et retourne le
// nombre d'entrées supprimées.
func (f *ShortcutsFile) RemoveByAppID(appID uint32) int {
	kept := f.doc.Root.Entries[:0]
	removed := 0
	for _, e := range f.doc.Root.Entries {
		if e.Value != nil && e.Value.Object != nil {
			if v, ok := e.Value.Object.UintValue("appid"); ok && v == appID {
				removed++
				continue
			}
		}
		kept = append(kept, e)
	}
	f.doc.Root.Entries = kept
	return removed
}

// Marshal sérialise le fichier, en réindexant les entrées de 0 à n-1.
// La réindexation évite de laisser des trous après une suppression, ce que
// Steam tolère mais qui rend les fichiers difficiles à relire.
func (f *ShortcutsFile) Marshal() []byte {
	for i, e := range f.doc.Root.Entries {
		e.Key = strconv.Itoa(i)
	}
	return vdf.Marshal(f.doc)
}

// Save écrit le fichier de façon atomique : sauvegarde de l'original, écriture
// dans un fichier temporaire, puis renommage. Une coupure en cours d'écriture
// ne peut donc pas laisser un shortcuts.vdf tronqué.
func (f *ShortcutsFile) Save() error {
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("création de %s : %w", dir, err)
	}

	data := f.Marshal()

	if f.exists {
		backup := f.path + ".bak"
		if orig, err := os.ReadFile(f.path); err == nil {
			if err := os.WriteFile(backup, orig, f.perms); err != nil {
				return fmt.Errorf("sauvegarde vers %s : %w", backup, err)
			}
		}
	}

	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, data, f.perms); err != nil {
		return fmt.Errorf("écriture de %s : %w", tmp, err)
	}
	if err := os.Rename(tmp, f.path); err != nil {
		return fmt.Errorf("renommage de %s : %w", tmp, err)
	}
	f.exists = true
	return nil
}
