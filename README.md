# DeckWebGPU

Jouer des jeux web WebGPU depuis Steam, sur Steam Deck, comme des jeux normaux.

L'outil enregistre une URL de jeu web comme raccourci non-Steam, puis ouvre un
navigateur Chromium en kiosque plein écran avec WebGPU actif et la manette
visible par l'API Gamepad.

```
swl  ->  colle une liste d'URLs  ->  relance Steam  ->  bouton A  ->  ça joue
```

Lancé sans argument, `swl` ouvre une petite interface locale : une zone de
texte où coller la liste, les emplacements à jour (Steam, navigateur, manette),
et un bouton qui ferme Steam proprement quand c'est nécessaire.

---

## 1. Ce que l'outil fait, et ce qu'il ne fait pas

**Fait**

- lit une liste d'URLs nommées et crée les raccourcis Steam correspondants ;
- lance le navigateur en kiosque sur l'URL, avec les flags WebGPU requis ;
- accorde au sandbox flatpak l'accès `/run/udev`, nécessaire à la manette ;
- génère des vignettes de bibliothèque (une couleur par jeu) ;
- journalise chaque lancement, parce qu'un jeu lancé par Steam n'a pas de
  terminal où afficher une erreur.

**Ne fait pas (pour l'instant)**

- pas de favicon ni de visuel distant : vignette de remplacement uniquement ;
- pas de toolkit graphique natif : l'interface est une page locale servie par le
  binaire et ouverte dans Chromium en mode application (section 6) ;

---

## 2. Décisions techniques (et deux corrections par rapport à l'idée de départ)

### Navigateur piloté, pas CEF embarqué

Embarquer CEF signifierait redistribuer un moteur de 150 Mo, prendre en charge
ses mises à jour de sécurité à la place de l'utilisateur, et écrire du C++
pour un résultat identique. Le binaire fait 3 Mo, il n'a aucune dépendance et
il pilote le Chromium déjà installé sur la machine.

### Les `.desktop` dans `~/.local/share/applications` ne suffisent pas

Steam **n'importe pas** ces fichiers dans la bibliothèque. Il les propose
seulement dans la boîte de dialogue « Ajouter un jeu non-Steam », donc jamais
automatiquement. La seule méthode fiable est d'écrire directement dans le
format binaire de Valve :

```
~/.steam/steam/userdata/<id>/config/shortcuts.vdf
```

C'est ce que fait l'outil, avec deux précautions :

- **Steam doit être fermé.** Il relit `shortcuts.vdf` à son démarrage et le
  réécrit depuis sa mémoire à sa fermeture. Écrire pendant qu'il tourne perd
  les modifications, sans le moindre message d'erreur. L'outil refuse donc
  d'écrire si le client tourne (`--force` passe outre, en connaissance de cause).
- **Le fichier existant est préservé.** Le codec lit l'arbre binaire tel quel et
  ne réécrit que ce qu'il ajoute, y compris les champs qu'il ne connaît pas.
  Une sauvegarde `shortcuts.vdf.bak` est créée avant chaque écriture.

### L'appid n'est pas un identifiant aléatoire

Steam calcule l'identifiant d'un raccourci non-Steam ainsi :

```
appid = CRC32(exe + nom) | 0x80000000
```

Deux pièges, tous les deux vérifiés contre des implémentations de référence
(RPCS3, Hydra) :

- le CRC se calcule sur le chemin **non quoté**, alors que le champ `Exe` est
  stocké entre guillemets ;
- c'est cet appid, en décimal non signé, qui sert de nom de fichier aux
  vignettes `grid/<appid>p.png`.

---

## 3. WebGPU sous Linux : état réel

Vérifié sur le [wiki d'implémentation du groupe GPU
Web](https://github.com/gpuweb/gpuweb/wiki/Implementation-Status) (page mise à
jour le 2 octobre 2026) et sur la [page de dépannage de
Chrome](https://developer.chrome.com/docs/web-platform/webgpu/troubleshooting-tips).

| Plateforme | État |
|---|---|
| macOS, Windows, ChromeOS | activé par défaut depuis Chrome 113 |
| Linux, Intel Gen12+ | activé par défaut depuis Chrome 144 |
| Linux, NVIDIA (pilote 535.183.01+) sous Wayland | activé par défaut depuis Chrome 147 |
| **Linux, tout le reste, AMD compris** | **derrière un flag** |

Le Steam Deck a un GPU AMD RDNA2 (pilote RADV) : il tombe donc dans la
dernière ligne. Le jeu de flags nécessaire est **plus large** que le seul
`--enable-unsafe-webgpu` :

```
--enable-unsafe-webgpu
--ozone-platform=x11
--use-angle=vulkan
--enable-features=Vulkan,VulkanFromANGLE
--ignore-gpu-blocklist
--enable-gpu-rasterization
```

Pourquoi chacun :

- `--ozone-platform=x11` : la pile Vulkan/ANGLE validée passe par XWayland, pas
  par le backend Wayland natif. En mode jeu, gamescope fournit cet XWayland, et
  en mode Bureau XWayland est là par défaut ;
- `--use-angle=vulkan` et `VulkanFromANGLE` : on veut ANGLE par-dessus Vulkan,
  et non par-dessus OpenGL ;
- `--ignore-gpu-blocklist` : la liste de blocage vise les pilotes anciens, mais
  peut attraper RADV ;
- `--enable-gpu-rasterization` : confort, laisse le compositing sur le GPU.

Ces flags sont surchargeables sans recompiler :

```
swl config --init     # fige les valeurs actuelles dans config.json
```

### Vérifier par soi-même

```
swl doctor --open-gpu
```

Cherche la ligne `WebGPU` dans `chrome://gpu`. Elle doit indiquer une
accélération matérielle, pas `software only`.

---

## 4. Manette

Chromium énumère les manettes via **libudev**, qui lit `/run/udev`. Le sandbox
flatpak de Chromium expose `--device=all` (donc `/dev/input/event*`) mais **pas**
`/run/udev`. Résultat : le périphérique est accessible et l'API Gamepad ne voit
toujours rien.

D'où l'écrasement, que `swl install` applique automatiquement quand il détecte
un navigateur flatpak :

```sh
flatpak override --user --filesystem=/run/udev:ro org.chromium.Chromium
```

Pour l'appliquer à la main, ou pour un autre navigateur :

```sh
flatpak override --user --filesystem=/run/udev:ro com.google.Chrome
```

`swl doctor` indique si l'accès est déjà accordé, et affiche la commande exacte
sinon.

### Côté Steam Input

En mode jeu, Steam Input présente la manette du Deck au jeu comme une manette
virtuelle. Il faut donc que le modèle appliqué au raccourci envoie bien des
entrées de manette :

1. lance le jeu, puis bouton **Steam** ;
2. **Contrôleur**, puis vérifie qu'un modèle de type manette est actif
   (« Gamepad » ou « Gamepad with Mouse Trackpad »).

Avec un modèle clavier/souris, Chromium ne verra aucune manette.

---

## 5. Installation

### Depuis l'AppImage

```sh
chmod +x DeckWebGPU-0.2.0-x86_64.AppImage
./DeckWebGPU-0.2.0-x86_64.AppImage install
```

Un double-clic sur l'AppImage ouvre directement l'interface. `install` copie le
binaire vers un emplacement stable et vérifie l'environnement. C'est
indispensable : lancé depuis un AppImage, le binaire vit dans un point de
montage temporaire, que Steam ne peut pas viser dans un raccourci.

L'AppImage peut ensuite être rangée ou supprimée : Steam pointe vers la copie
installée. Garde-la sous la main pour les mises à jour.

### Depuis les sources

```sh
make build        # binaire dans build/swl
make e2e          # vérification de bout en bout, dans un faux HOME Steam
make appimage     # produit l'AppImage
```

Il faut Go, `curl`, et un `file` dans le PATH pour l'AppImage.

### Navigateur requis

Un Chromium quelconque fait l'affaire. Le plus simple sur SteamOS :

```sh
flatpak install --user flathub org.chromium.Chromium
```

L'ordre de préférence est : Chromium flatpak, Chrome flatpak, Brave flatpak,
Edge flatpak, puis un binaire système (`chromium`, `google-chrome`...).
`swl doctor` affiche celui qui sera utilisé.

---

## 6. Utilisation

### L'interface

```
swl              # l'interface, dès qu'une session graphique est joignable
swl gui          # idem, explicitement
swl gui --no-open   # afficher l'adresse sans ouvrir de fenêtre
```

`swl` sans argument ouvre l'interface. Trois exceptions, volontaires :

- une liste arrive sur l'entrée standard (`printf ... | swl`) : c'est un collage,
  le mode terminal a du sens ;
- aucune session graphique (`DISPLAY` et `WAYLAND_DISPLAY` vides) : le mode
  terminal est la seule possibilité ;
- `swl cli` : le terminal, explicitement.

Ce que contient la page :

- **l'état** : Steam arrêté ou en cours avec le profil retenu, le nombre de
  raccourcis déjà présents, le navigateur Chromium détecté, et l'accès
  `/run/udev` accordé ou non ;
- **une zone de texte**, pré-remplie sur un registre vide avec deux jeux qui
  fonctionnent tout de suite, ce qui permet de valider la chaîne complète avant
  d'ajouter les siens :

  ```
  Skate 3 = https://skate.aaddpp.lol/
  Black Ops 1 Zombies = https://vel.gg/bo1z
  ```

- **« Fermer Steam »**, qui n'apparaît que si Steam tourne, et qui attend la
  disparition effective du processus. Lancer la commande ne suffit pas : Steam
  met plusieurs secondes à s'arrêter et écraserait nos raccourcis s'il lui
  restait du temps ;
- **la liste des jeux enregistrés**, avec « Ouvrir » (lance le jeu en plein
  écran, exactement comme le fera Steam) et « Retirer » ;
- **une case « simulation »**, pour voir ce qui serait écrit sans rien écrire.

Le service s'arrête quand la fenêtre se ferme : rien ne reste en arrière-plan.

#### Pourquoi une page et non GTK

Un vrai toolkit (GTK, Fyne) impose `cgo`, une bibliothèque graphique présente
sur la machine cible, et ferait passer l'AppImage de 3 Mo à plus de 30 Mo, pour
une fenêtre qui contient une zone de texte et deux boutons. Le binaire reste
statique et portable, et le navigateur dont l'outil a besoin de toute façon est
déjà là.

#### Ce qui protège cette interface

Elle n'est pas inerte : elle écrit dans ta bibliothèque Steam. Trois mesures :

- écoute sur `127.0.0.1` uniquement, sur un port tiré au hasard ;
- un jeton aléatoire de 128 bits exigé sur chaque requête. Sans lui, n'importe
  quel processus de la machine pourrait appeler l'API ;
- l'en-tête `Host` doit être un hôte local littéral, ce qui bloque le rebinding
  DNS (un nom de domaine résolu vers `127.0.0.1` ferait passer une page hostile
  pour une requête locale).

L'interface appelle exactement les mêmes fonctions que le CLI : le refus
d'écrire quand Steam tourne, l'idempotence et le calcul d'appid sont partagés,
pas réimplémentés.

### Ajouter des jeux au terminal

**Steam doit être fermé** (mode Bureau : menu Steam puis Quitter).

```
swl cli
```

Puis colle la liste, une ligne par jeu, et termine par Ctrl-D :

```
Vitrage 3D  = https://exemple.com/jeu
Shadertoy   | https://www.shadertoy.com/new
https://threejs.example.net/demo
Quatrieme   = https://exemple.com/4 | --force-device-scale-factor=1
```

Les lignes vides et celles commençant par `#` sont ignorées. Quand le nom est
absent, il est déduit de l'URL. Ce qui suit l'URL est passé tel quel à Chromium.

Ou depuis un fichier, ce qui est plus pratique en mode Bureau :

```
swl add -f mes-jeux.txt
swl add -f mes-jeux.txt --dry-run     # voir sans écrire
swl add -f mes-jeux.txt -y            # sans confirmation
```

L'opération est idempotente : relancer `add` avec la même liste met à jour au
lieu de dupliquer.

### Puis

1. relance Steam (le bouton Steam, puis Quitter en mode Bureau) ;
2. en mode jeu, lance le jeu avec le bouton **A**.

L'interface le rappelle après chaque ajout.

### Autres commandes

```
swl gui                  # interface
swl cli                  # mode terminal
swl list                 # jeux enregistrés, avec leur appid
swl remove <slug>        # retirer un jeu (Steam fermé)
swl doctor               # diagnostic complet
swl config               # configuration effective
swl launch <slug>        # appelé par Steam, pas à la main
```

---

## 7. Dépannage

Un jeu lancé par Steam n'affiche rien : toute la sortie du dernier lancement
est dans

```
~/.local/share/steam-webgpu/logs/launch-<slug>.log
```

La commande complète passée au navigateur y est écrite en clair, ce qui permet
de la rejouer à la main.

| Symptôme | Cause probable |
|---|---|
| L'ajout est refusé | Steam tourne : ferme-le complètement |
| Le jeu n'apparaît pas dans Steam | Steam n'a pas été relancé après l'ajout |
| Le jeu se lance puis revient au menu | voir le journal de lancement |
| La manette n'est pas vue | `/run/udev` non accordé, ou modèle Steam Input clavier/souris |
| Le jeu rafraîchit au ralenti | WebGPU est tombé en logiciel : `swl doctor --open-gpu` |
| `shortcuts.vdf` corrompu | restaurer `shortcuts.vdf.bak` (Steam fermé) |
| L'interface ne s'ouvre pas | aucun navigateur détecté : `swl doctor`, puis `swl gui --no-open` et ouvrir l'adresse à la main |
| La fenêtre de l'interface affiche une erreur 403 | l'adresse a été ouverte sans son jeton : repasse par `swl gui` |
| L'interface se ferme seule au bout de 30 min | aucun navigateur n'a pu être lancé, l'arrêt d'inactivité a joué : `--idle-timeout 0` pour le désactiver |

---

## 8. Où l'outil écrit

| Chemin | Contenu |
|---|---|
| `~/.local/share/steam-webgpu/bin/swl` | binaire installé, visé par les raccourcis |
| `~/.local/share/steam-webgpu/games.json` | registre des jeux |
| `~/.local/share/steam-webgpu/config.json` | configuration, si créée |
| `~/.local/share/steam-webgpu/profile/` | profil Chromium partagé (sessions des sites) |
| `~/.local/share/steam-webgpu/icons/` | icônes de remplacement |
| `~/.local/share/steam-webgpu/logs/` | journaux de lancement |
| `~/.local/bin/swl` | lien symbolique |
| `<steam>/userdata/<id>/config/shortcuts.vdf` | raccourcis (sauvegarde `.bak` à côté) |
| `<steam>/userdata/<id>/config/grid/` | vignettes de bibliothèque |
| `~/.local/share/flatpak/overrides/<appid>` | uniquement si `flatpak override` a été appliqué |

L'interface utilise un profil Chromium temporaire, distinct du profil partagé
des jeux : ouvrir la fenêtre de configuration ne touche pas aux sessions des
sites, et l'ouvrir pendant une partie ne perturbe pas le kiosque en cours. Ce
profil est supprimé à la fermeture.

`swl uninstall --purge` retire le binaire et les données. Les raccourcis Steam
se retirent avec `swl remove <slug>`.

---

## 9. Limites connues

- **x86_64 uniquement** : Steam Deck et PC de bureau. Pas d'ARM.
- **Pas de favicon** : vignette de remplacement colorée, déterministe par jeu.
  Pour poser tes propres visuels, dépose-les dans
  `<steam>/userdata/<id>/config/grid/` sous `<appid>p.png` (portrait),
  `<appid>.png` (capsule), `<appid>_hero.png`, `<appid>_logo.png`.
- **Profil Chromium partagé** : pratique pour ne se connecter qu'une fois à un
  site, mais tous les jeux partagent cookies et stockage local.
- **Les flags WebGPU vont bouger.** AMD sous Linux finira par passer en
  activation par défaut ; le jour où ce sera le cas, `swl config --init` suivi
  de l'édition du fichier permet de retirer les flags sans recompiler.
- **Le mode Bureau de SteamOS est en lecture seule** : l'outil n'installe rien
  dans le système, tout vit dans le HOME.

---

## 10. Ce qui a été vérifié, et comment

Le codec VDF binaire, le calcul d'appid, l'analyse des listes et le stockage
sont couverts par des tests unitaires (`go test ./...`), dont un test qui
vérifie qu'un champ inconnu d'un raccourci existant survit à un aller-retour.

L'interface est testée par HTTP, comme le navigateur l'appelle, avec la vraie
logique métier et un faux HOME Steam : jeton exigé, hôte étranger rejeté,
ajout, idempotence, simulation sans écriture, retrait, et refus d'écrire quand
un processus nommé `steam` tourne.

Le harnais `scripts/e2e.sh` (`make e2e`) exerce la chaîne complète dans un faux
HOME Steam : installation, ajout au terminal, ajout par HTTP, validation du
fichier produit par `scripts/verif_vdf.py` (parseur Python indépendant, qui
recalcule les appid avec `zlib` plutôt qu'avec le `crc32` de Go), idempotence,
refus d'écrire quand Steam tourne, simulation, suppression.

L'AppImage a été exécutée dans un faux HOME (`APPIMAGE_EXTRACT_AND_RUN=1`) :
`install`, auto-copie du binaire, ouverture de l'interface, ajout des deux jeux
proposés par défaut, puis relecture du `shortcuts.vdf` produit par
`scripts/verif_vdf.py`.

Ce qui n'a **pas** pu être testé ici, faute de Steam Deck et de GPU AMD :

- la détection réelle de la manette via l'API Gamepad ;
- l'activation effective de WebGPU sur RADV (les flags viennent de la
  documentation du groupe GPU Web, ils n'ont pas été exécutés sur ce GPU) ;
- l'apparition du raccourci dans la bibliothèque de Steam après écriture de
  `shortcuts.vdf`. Le format produit est vérifié octet par octet contre la
  spécification, mais le dernier maillon reste à confirmer sur la machine.

Le premier lancement sur le Deck, avec `swl doctor --open-gpu`, tranchera les
trois points.

## Licence

MIT.
