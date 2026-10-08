package steam

import "hash/crc32"

// steamID64Base est l'offset entre le SteamID64 et l'identifiant de compte
// (SteamID3) utilisé dans les chemins userdata/<id>/.
const steamID64Base uint64 = 76561197960265728

// AppID reproduit l'identifiant que Steam attribue à un raccourci non-Steam :
// CRC32 IEEE de (chemin de l'exécutable + nom du jeu), bit de poids fort forcé.
//
// Deux subtilités qui font échouer la plupart des réimplémentations :
//   - le CRC se calcule sur le chemin NON quoté, alors que le champ Exe est
//     stocké entre guillemets dans le fichier ;
//   - l'ordre est exe puis nom, sans séparateur.
//
// L'identifiant sert aussi de nom de fichier pour les vignettes de
// bibliothèque : userdata/<id>/config/grid/<appid>p.png, etc.
func AppID(exe, appName string) uint32 {
	return crc32.ChecksumIEEE([]byte(exe+appName)) | 0x80000000
}
