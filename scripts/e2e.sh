#!/usr/bin/env bash
# Vérification de bout en bout, dans un faux HOME Steam.
#
# Rien n'est écrit en dehors d'un répertoire temporaire, et aucun processus
# Steam n'est touché : le script vérifie que les raccourcis produits sont des
# VDF binaires valides, que les appid correspondent au CRC32 attendu, et que
# les garde-fous (Steam en cours, idempotence) fonctionnent.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

BIN="$ROOT/build/swl"
if [ ! -x "$BIN" ]; then
	echo "compilation préalable nécessaire : make build" >&2
	exit 1
fi

export SWL_DATA_DIR="$WORK/data"
export STEAM_ROOT="$WORK/steam"
export HOME="$WORK/home"
mkdir -p "$HOME"

ACCOUNT=39734272
mkdir -p "$STEAM_ROOT/config" "$STEAM_ROOT/userdata/$ACCOUNT/config"
cat >"$STEAM_ROOT/config/loginusers.vdf" <<'VDF'
"users"
{
	"76561198000000000"
	{
		"AccountName"	"deck"
		"MostRecent"	"1"
	}
}
VDF

echo "== 1. install"
"$BIN" install | sed 's/^/   /'

echo "== 2. add"
printf 'Vitrage 3D = https://vitrage.example.org/play\nShadertoy | https://www.shadertoy.com/new\n' |
	"$BIN" add -y | sed 's/^/   /'

VDF="$STEAM_ROOT/userdata/$ACCOUNT/config/shortcuts.vdf"
[ -f "$VDF" ] || { echo "échec : $VDF absent"; exit 1; }

echo "== 3. validation du fichier produits"
python3 - "$VDF" <<'PY'
import struct, sys, zlib

data = open(sys.argv[1], 'rb').read()
assert data[:1] == b'\x00', "le fichier doit commencer par un objet"
assert data[-2:] == b'\x08\x08', "le fichier doit finir par deux 0x08"

class R:
    def __init__(self, b): self.b, self.p = b, 0
    def u8(self):
        v = self.b[self.p]; self.p += 1; return v
    def cstr(self):
        e = self.b.index(b'\x00', self.p); s = self.b[self.p:e].decode(); self.p = e + 1; return s
    def body(self):
        obj = []
        while True:
            t = self.u8()
            if t == 0x08: return obj
            k = self.cstr()
            if t == 0x00: obj.append((k, self.body()))
            elif t == 0x01: obj.append((k, self.cstr()))
            elif t == 0x02: obj.append((k, struct.unpack('<I', self.b[self.p:self.p+4])[0])); self.p += 4
            elif t == 0x07: obj.append((k, struct.unpack('<Q', self.b[self.p:self.p+8])[0])); self.p += 8
            else: raise AssertionError(f"type 0x{t:02x} inconnu")

r = R(data)
assert r.u8() == 0x00 and r.cstr() == 'shortcuts'
root = r.body()
assert r.u8() == 0x08
assert r.p == len(data), "octets résiduels"

for i, (key, entry) in enumerate(root):
    d = dict(entry)
    exe = d['Exe'].strip('"')
    want = zlib.crc32((exe + d['AppName']).encode()) | 0x80000000
    assert d['appid'] == want, f"appid {d['appid']} != {want}"
    assert key == str(i), f"index {key} != {i}"
    assert d['LaunchOptions'].startswith('launch '), "LaunchOptions inattendu"
    assert 'tags' in d, "objet tags manquant"
print(f"   {len(root)} raccourcis valides, appid conformes au CRC32")
PY

echo "== 4. idempotence : un second add ne doit rien dupliquer"
printf 'Vitrage 3D = https://vitrage.example.org/play\n' | "$BIN" add -y | grep -q "rien de nouveau" ||
	{ echo "échec : le second add a dupliqué quelque chose"; exit 1; }
echo "   ok"

echo "== 5. garde-fou : refus d'écrire quand Steam tourne"
# La détection se fait sur /proc/<pid>/comm : il suffit donc d'un processus
# nommé "steam" pour reproduire le cas. Le fichier ne doit surtout pas
# s'appeler comme le dossier Steam, d'où le sous-répertoire bin/.
SLEEP=""
for c in /usr/bin/sleep /bin/sleep /usr/bin/busybox; do
	[ -x "$c" ] && SLEEP="$c" && break
done
if [ -z "$SLEEP" ]; then
	echo "   ignoré (/usr/bin/sleep introuvable)"
else
	mkdir -p "$WORK/bin"
	cp "$SLEEP" "$WORK/bin/steam"
	"$WORK/bin/steam" 30 &
	FAKE=$!
	sleep 0.5
	if printf 'Test = https://garde.example.com\n' | "$BIN" add -y >/dev/null 2>&1; then
		echo "échec : l'ajout aurait dû être refusé"
		kill "$FAKE" 2>/dev/null || true
		exit 1
	fi
	kill "$FAKE" 2>/dev/null || true
	wait "$FAKE" 2>/dev/null || true
	echo "   ok (refusé)"
fi

echo "== 6. remove"
"$BIN" remove shadertoy | sed 's/^/   /'
python3 - "$VDF" <<'PY'
import sys
d = open(sys.argv[1], 'rb').read()
assert d.count(b'AppName') == 1, "un raccourci aurait dû être supprimé"
assert d[-2:] == b'\x08\x08'
print("   1 raccourci restant, fichier toujours valide")
PY

echo "== 7. interface : les mêmes opérations par HTTP"
# L'interface est un serveur local. On l'interroge comme le ferait la page,
# ce qui vérifie aussi le contrôle d'accès par jeton.
PORT=8799
"$BIN" gui --no-open --addr "127.0.0.1:$PORT" --idle-timeout 60s >"$WORK/gui.log" 2>&1 &
GUI=$!
trap 'kill "$GUI" 2>/dev/null || true; rm -rf "$WORK"' EXIT

for _ in $(seq 1 60); do
	grep -q 'Interface : ' "$WORK/gui.log" 2>/dev/null && break
	sleep 0.1
done
TOKEN="$(sed -n 's|.*[?]t=\([0-9a-f]*\).*|\1|p' "$WORK/gui.log" | head -1)"
[ -n "$TOKEN" ] || { echo "échec : jeton absent du journal"; cat "$WORK/gui.log"; exit 1; }
BASE="http://127.0.0.1:$PORT"

status() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

for cas in "sans jeton:$BASE/api/state" "hôte étranger:$BASE/api/state?t=$TOKEN"; do
	nom="${cas%%:*}"
	url="${cas#*:}"
	if [ "$nom" = "hôte étranger" ]; then
		[ "$(status -H 'Host: attaquant.example.org' "$url")" = "403" ] ||
			{ echo "échec : hôte étranger accepté"; exit 1; }
	else
		[ "$(status "$url")" = "403" ] || { echo "échec : requête sans jeton acceptée"; exit 1; }
	fi
done
echo "   accès refusé sans jeton et pour un hôte étranger"

curl -s "$BASE/api/state?t=$TOKEN" >"$WORK/state.json"
python3 - "$WORK/state.json" "$STEAM_ROOT" <<'PY'
import json, sys
state = json.load(open(sys.argv[1]))
assert state["steam_root"] == sys.argv[2], state["steam_root"]
assert state["steam_running"] is False
print("   état : %d jeu(x), racine %s" % (len(state["games"]), state["steam_root"]))
PY

# Ajout de la liste par défaut, exactement le texte que la page propose.
curl -s -X POST "$BASE/api/add?t=$TOKEN" -H 'Content-Type: application/json' \
	--data-binary '{"text":"Skate 3 = https://skate.aaddpp.lol/\nBlack Ops 1 Zombies = https://vel.gg/bo1z\n"}' \
	>"$WORK/add.json"
python3 - "$WORK/add.json" <<'PY'
import json, sys
outcome = json.load(open(sys.argv[1]))["outcome"]
assert outcome.get("error", "") == "", outcome
assert outcome["added"] == 2, outcome
print("   interface : 2 jeux ajoutés")
PY

# Le fichier reste lisible par le vérificateur indépendant, qui relit les
# octets et recalcule les appid avec zlib (aucun code partagé avec le Go).
python3 "$ROOT/scripts/verif_vdf.py" "$VDF" | sed 's/^/   /'

echo "== 8. interface : simulation et retrait"
# Une URL encore inconnue : la simulation doit annoncer un ajout sans écrire.
curl -s -X POST "$BASE/api/add?t=$TOKEN" -H 'Content-Type: application/json' \
	--data-binary '{"text":"Test Simulation = https://tiers.example.net/\n","dry_run":true}' |
	python3 -c 'import json,sys; o=json.load(sys.stdin)["outcome"]; assert o["added"]==1 and o["skipped"]==0, o; print("   simulation : 1 ajout annoncé")'
if grep -q "Test Simulation" "$VDF"; then
	echo "échec : la simulation a écrit dans shortcuts.vdf"
	exit 1
fi
echo "   simulation : rien écrit"

curl -s -X POST "$BASE/api/remove?t=$TOKEN" -H 'Content-Type: application/json' \
	--data-binary '{"slugs":["skate-3"]}' |
	python3 -c 'import json,sys; o=json.load(sys.stdin)["outcome"]; assert o["removed"]==1, o; print("   retrait : 1 raccourci")'

python3 "$ROOT/scripts/verif_vdf.py" "$VDF" >/dev/null || { echo "échec : fichier invalide après retrait"; exit 1; }
echo "   fichier toujours valide"

echo
echo "Tout est vert."
