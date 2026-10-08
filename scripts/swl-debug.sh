#!/usr/bin/env bash
# swl-debug.sh : diagnostic de DeckWebGPU sur Steam Deck.
#
# A lancer dans Konsole, en mode Bureau :
#     bash scripts/swl-debug.sh
#
# Lecture seule : rien n'est modifie, sauf le rapport ecrit dans ~/swl-debug.txt.
# Un seul test ouvre une fenetre de jeu, volontairement, pour reproduire l'appel
# que Steam fait et voir enfin l'erreur que Steam jette.
#
# Utilisable tel quel depuis un clone du depot, ou copie seul sur le Deck.

OUT="$HOME/swl-debug.txt"
if ! : > "$OUT" 2>/dev/null; then OUT="/tmp/swl-debug.txt"; : > "$OUT"; fi

say() { printf '%s\n' "$*" | tee -a "$OUT"; }
sec() { say ""; say "------------------------------------------------------------"; say "-- $*"; say "------------------------------------------------------------"; }
run() {
  say "\$ $*"
  local out rc
  out="$( "$@" 2>&1 )"; rc=$?
  [ -n "$out" ] && say "$out"
  say "   [code de sortie : $rc]"
}
have() { command -v "$1" >/dev/null 2>&1; }

# Racine du depot, pour trouver une AppImage construite sur place.
SELF_DIR="$(cd -- "$(dirname -- "$0")" 2>/dev/null && pwd)"

say "Rapport de diagnostic DeckWebGPU"
say "date    : $(date -Is 2>/dev/null || date)"
say "machine : $(uname -srm 2>/dev/null)"
say "rapport : $OUT"

sec "1. Systeme et session graphique"
if [ -r /etc/os-release ]; then
  . /etc/os-release 2>/dev/null
  say "distribution    : ${PRETTY_NAME:-inconnue}"
fi
say "utilisateur     : $(id -un 2>/dev/null)"
say "HOME            : $HOME"
say "DISPLAY         : ${DISPLAY:-(vide)}"
say "WAYLAND_DISPLAY : ${WAYLAND_DISPLAY:-(vide)}"
say "XDG_RUNTIME_DIR : ${XDG_RUNTIME_DIR:-(vide)}"
say "XDG_SESSION_TYPE: ${XDG_SESSION_TYPE:-(vide)}"
if [ -z "${DISPLAY:-}" ] && [ -z "${WAYLAND_DISPLAY:-}" ]; then
  say ""
  say "!! Aucune session graphique detectee. Relance depuis Konsole en mode Bureau."
fi

sec "2. Steam tourne-t-il ?"
pids=""
for p in /proc/[0-9]*; do
  [ -r "$p/comm" ] || continue
  [ "$(cat "$p/comm" 2>/dev/null)" = "steam" ] && pids="$pids ${p#/proc/}"
done
say "processus nommes 'steam' :${pids:- aucun}"
[ -n "$pids" ] && say "Rappel : toute ecriture dans shortcuts.vdf pendant ce temps est perdue."

sec "3. AppImage"
AI=""
for c in "$HOME"/DeckWebGPU-*.AppImage "$HOME"/Downloads/DeckWebGPU-*.AppImage \
         "$HOME"/Desktop/DeckWebGPU-*.AppImage "$HOME"/Bureau/DeckWebGPU-*.AppImage \
         "$HOME"/Téléchargements/DeckWebGPU-*.AppImage \
         "$PWD"/DeckWebGPU-*.AppImage "$SELF_DIR"/DeckWebGPU-*.AppImage \
         "$SELF_DIR"/../DeckWebGPU-*.AppImage; do
  [ -f "$c" ] && { AI="$c"; break; }
done
if [ -z "$AI" ]; then
  say "Aucune AppImage DeckWebGPU-*.AppImage trouvee."
  say "Localise-la :  find \"\$HOME\" -maxdepth 4 -name 'DeckWebGPU-*.AppImage' 2>/dev/null"
else
  say "fichier : $AI"
  run ls -l "$AI"
  [ -x "$AI" ] && say "bit executable : oui" || say "bit executable : NON  (correctif : chmod +x \"$AI\")"
  say "sha256  : $(sha256sum "$AI" 2>/dev/null | cut -d' ' -f1)"
  say ""
  say "FUSE, necessaire au montage d'une AppImage :"
  for b in fusermount fusermount3; do
    if have "$b"; then say "  $b : $(command -v "$b")"; else say "  $b : absent"; fi
  done
  [ -e /dev/fuse ] && say "  /dev/fuse : present" || say "  /dev/fuse : ABSENT"
  found=""
  for l in /usr/lib/libfuse.so.2 /usr/lib64/libfuse.so.2 /usr/lib/x86_64-linux-gnu/libfuse.so.2; do
    [ -e "$l" ] && { say "  $l"; found=1; }
  done
  [ -z "$found" ] && say "  libfuse.so.2 : non trouve"
  say ""
  say "-- essai A : montage FUSE normal"
  out="$("$AI" version 2>&1)"; rc=$?
  [ -n "$out" ] && say "$out"
  say "   [code de sortie : $rc]"
  if [ "$rc" -ne 0 ]; then
    say "-- essai B : APPIMAGE_EXTRACT_AND_RUN=1"
    out="$(APPIMAGE_EXTRACT_AND_RUN=1 "$AI" version 2>&1)"; rc=$?
    [ -n "$out" ] && say "$out"
    say "   [code de sortie : $rc]"
    [ "$rc" -eq 0 ] && say ">> FUSE indisponible, mais l'extraction fonctionne : prefixe tes commandes par APPIMAGE_EXTRACT_AND_RUN=1."
  fi
fi

sec "4. Lanceur installe"
DATA="$HOME/.local/share/steam-webgpu"
SWL="$DATA/bin/swl"
if [ -x "$SWL" ]; then
  say "present : $SWL"
  run ls -l "$SWL"
  run "$SWL" version
else
  say "ABSENT : $SWL"
  say ">> C'est la cible exacte des raccourcis Steam. Tant qu'il manque,"
  say ">> chaque jeu echoue au lancement, en silence."
  say ">> Correctif : lance l'AppImage avec l'argument install."
fi

sec "5. Navigateur Chromium"
if have flatpak; then
  say "-- applications flatpak installees"
  run flatpak list --app --columns=application,version,origin
else
  say "flatpak : absent"
fi
say ""
say "-- binaires Chromium dans le PATH"
n=0
for b in chromium chromium-browser google-chrome-stable google-chrome brave-browser \
         microsoft-edge-stable microsoft-edge; do
  if have "$b"; then say "  $b : $(command -v "$b")"; n=$((n+1)); fi
done
[ "$n" -eq 0 ] && say "  aucun"
say ""
say "-- chemins connus"
m=0
for p in /usr/bin/chromium /usr/lib/chromium/chromium /opt/google/chrome/chrome /opt/brave.com/brave/brave; do
  [ -e "$p" ] && { say "  $p"; m=$((m+1)); }
done
[ "$m" -eq 0 ] && say "  aucun"
say ""
say ">> DeckWebGPU ne pilote qu'un navigateur base sur Chromium. Firefox ne convient pas."
say ">> SteamOS n'en installe aucun d'origine."

sec "6. Raccourcis Steam"
root=""
for d in "$HOME/.steam/steam" "$HOME/.local/share/Steam" "$HOME/.steam/root"; do
  [ -d "$d/userdata" ] && { root="$d"; break; }
done
say "racine Steam : ${root:-introuvable}"
if [ -n "$root" ]; then
  for ud in "$root"/userdata/*; do
    [ -d "$ud" ] || continue
    vdf="$ud/config/shortcuts.vdf"
    say ""
    say "-- compte $(basename "$ud")"
    if [ -f "$vdf" ]; then
      run ls -l "$vdf"
      say "chaines utiles :"
      tr -c '[:print:]' '\n' < "$vdf" | grep -aiE 'swl|launch |https?://' | head -40 | sed 's/^/    /'
      if tr -c '[:print:]' '\n' < "$vdf" | grep -aq "steam-webgpu/bin/swl"; then
        exe_path="$(tr -c '[:print:]' '\n' < "$vdf" | grep -a 'steam-webgpu/bin/swl' | head -1 | tr -d '"')"
        if [ -x "$exe_path" ]; then
          say "    >> cible du raccourci : $exe_path"
          say "    >> presente et executable, donc c'est bien le chemin attendu"
        else
          say "    >> cible du raccourci : $exe_path"
          say "    >> ABSENTE : c'est la cause du plantage. Le raccourci est bon,"
          say "    >> il manque seulement l'installation du lanceur."
        fi
      else
        say "    >> ATTENTION : le chemin du lanceur installe n'apparait pas dans ce fichier."
        say "    >> Le raccourci vise peut-etre l'AppImage elle-meme, ce qui casse en mode jeu (FUSE)."
      fi
      nb_launch="$(tr -c '[:print:]' '\n' < "$vdf" | grep -ac '^launch ')"
      nb_slug="$(grep -o '"slug"' "$DATA/games.json" 2>/dev/null | wc -l)"
      say "    >> raccourcis avec options 'launch ' : $nb_launch"
      say "    >> jeux dans le registre              : $nb_slug"
      if [ "$nb_launch" -ne "$nb_slug" ] 2>/dev/null; then
        say "    >> ECART : un raccourci ou un jeu du registre est orphelin."
        say "    >> Relance l'ajout apres install pour remettre les deux listes d'accord."
      fi
    else
      say "   aucun shortcuts.vdf pour ce compte"
    fi
  done
fi

sec "7. Journaux de lancement"
LOGS="$DATA/logs"
if [ -d "$LOGS" ]; then
  run ls -lt "$LOGS"
  for f in "$LOGS"/*.log; do
    [ -f "$f" ] || continue
    say ""
    say "-- $f"
    tail -n 40 "$f" | sed 's/^/    /'
  done
else
  say "Aucun dossier de journaux."
  say ">> Indice fort : l'echec se produit avant que le lanceur n'ouvre son journal."
  say ">> Autrement dit avant meme d'avoir trouve un navigateur, ou sans avoir tourne du tout."
fi

sec "8. Registre des jeux et diagnostic du lanceur"
SLUGS=""
if [ -f "$DATA/games.json" ]; then
  run ls -l "$DATA/games.json"
  say "contenu :"
  sed 's/^/    /' "$DATA/games.json"
  SLUGS="$(grep -o '"slug": *"[^"]*"' "$DATA/games.json" | sed 's/.*"slug": *"//; s/"$//')"
else
  say "games.json absent : aucun jeu enregistre."
fi
if [ -x "$SWL" ]; then
  say ""
  say "-- swl list"
  run "$SWL" list
  say ""
  say "-- swl doctor"
  run "$SWL" doctor
fi

sec "9. Reproduction de l'appel fait par Steam"
say 'Steam execute exactement :  "<swl>" launch <slug>'
say "Une fenetre de jeu va s'ouvrir, puis se fermer. C'est normal."
if [ -x "$SWL" ] && [ -n "$SLUGS" ]; then
  sleep 3
  for slug in $SLUGS; do
    say ""
    say ">> jeu : $slug"
    out="$(timeout 25 "$SWL" launch "$slug" 2>&1)"; rc=$?
    [ -n "$out" ] && say "$out"
    say "   [code de sortie : $rc]"
    case $rc in
      0)   say "   -> le navigateur s'est ferme normalement" ;;
      3)   say "   -> jeu inconnu du registre (code 3)" ;;
      124) say "   -> encore en cours apres 25 s : une fenetre s'est ouverte, c'est le comportement attendu" ;;
      1)   say "   -> ECHEC, le message ci-dessus dit pourquoi" ;;
      *)   say "   -> code inattendu" ;;
    esac
    if [ -f "$LOGS/launch-$slug.log" ]; then
      say "   journal :"
      tail -n 25 "$LOGS/launch-$slug.log" | sed 's/^/      /'
    fi
  done
else
  say "Non testable : lanceur absent ou aucun jeu enregistre."
fi

sec "10. Verdict"
say "Reprends ci-dessus tout ce qui est marque ABSENT, NON, aucun ou ECHEC."
say ""
if [ -n "$AI" ]; then
  say "  lanceur absent            -> \"$AI\" install"
else
  say "  lanceur absent            -> <AppImage> install"
fi
say "  aucun navigateur Chromium -> flatpak install --user flathub org.chromium.Chromium"
say "  FUSE indisponible         -> prefixer par APPIMAGE_EXTRACT_AND_RUN=1"
say "  raccourci visant l'AppImage -> re-ajouter les jeux apres install"
say ""
say "Rapport complet : $OUT"
say "Envoie-moi ce fichier, ou colle son contenu."
