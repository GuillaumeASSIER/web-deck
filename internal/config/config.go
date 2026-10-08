// Package config gère les réglages persistants de l'outil.
//
// Les flags de Chrome liés à WebGPU bougent vite : ils sont donc
// surchargeables sans recompiler. Un fichier de configuration coûte moins
// cher qu'une version corrigée à republier.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config regroupe les réglages.
type Config struct {
	// Browser force un navigateur (identifiant flatpak, nom de binaire ou
	// chemin absolu). Vide : détection automatique.
	Browser string `json:"browser,omitempty"`

	// WebGPUArgs remplace le jeu d'arguments WebGPU par défaut. nil garde
	// la valeur par défaut du code.
	WebGPUArgs []string `json:"webgpu_args,omitempty"`

	// ExtraArgs est ajouté après les arguments par défaut, pour tous les jeux.
	ExtraArgs []string `json:"extra_args,omitempty"`

	// ProfileDir force le dossier de profil Chromium. Vide :
	// <données>/profile, partagé entre les jeux (les sessions des sites
	// restent donc ouvertes d'un lancement à l'autre).
	ProfileDir string `json:"profile_dir,omitempty"`
}

// Default retourne la configuration par défaut : tout est en détection
// automatique.
func Default() *Config { return &Config{} }

// Load lit la configuration. Fichier absent : configuration par défaut.
func Load(path string) (*Config, error) {
	c := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, fmt.Errorf("lecture de %s : %w", path, err)
	}
	if len(data) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("%s : JSON invalide (%w)", path, err)
	}
	return c, nil
}

// Save écrit la configuration.
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
