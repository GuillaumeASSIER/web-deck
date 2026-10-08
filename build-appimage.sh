#!/usr/bin/env bash
# Construit l'AppImage de DeckWebGPU.
#
# Le script est volontairement autonome : il ne suppose qu'un compilateur Go et
# curl. appimagetool est récupéré à la demande, et lancé avec
# APPIMAGE_EXTRACT_AND_RUN=1 pour fonctionner sans FUSE (fréquent en conteneur
# et en intégration continue).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

APP_NAME="DeckWebGPU"
APP_ID="steam-webgpu-launcher"
ARCH="x86_64"
VERSION="${VERSION:-0.2.0}"

BUILD_DIR="$ROOT/build"
APPDIR="$BUILD_DIR/$APP_NAME.AppDir"
OUT="$ROOT/${APP_NAME}-${VERSION}-${ARCH}.AppImage"

GO="${GO:-go}"
if ! command -v "$GO" >/dev/null 2>&1; then
	echo "erreur : le compilateur Go est introuvable." >&2
	echo "Installe-le depuis https://go.dev/dl/ ou passe son chemin : GO=/chemin/vers/go $0" >&2
	exit 1
fi

echo "==> Compilation (statique, sans cgo)"
rm -rf "$BUILD_DIR"
mkdir -p "$APPDIR/usr/bin"

# CGO_ENABLED=0 : binaire statique, donc indépendant de la glibc de SteamOS.
# -trimpath : pas de chemins de compilation dans le binaire.
# -s -w : symboles de débogage retirés, l'AppImage reste petit.
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "$GO" build \
	-trimpath -ldflags "-s -w" \
	-o "$APPDIR/usr/bin/swl" .

echo "==> Assemblage de l'AppDir"
install -m 0755 packaging/AppRun "$APPDIR/AppRun"
install -m 0644 "packaging/${APP_ID}.desktop" "$APPDIR/${APP_ID}.desktop"

# L'icône est produite par le binaire lui-même : une seule source de vérité
# pour le visuel, et pas d'image binaire à maintenir dans le dépôt.
"$APPDIR/usr/bin/swl" icon "$APPDIR/${APP_ID}.png"
cp "$APPDIR/${APP_ID}.png" "$APPDIR/.DirIcon"

echo "==> Récupération de appimagetool"
TOOL="$BUILD_DIR/appimagetool"
if [ ! -x "$TOOL" ]; then
	curl -fsSL -o "$TOOL" \
		"https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-x86_64.AppImage"
	chmod +x "$TOOL"
fi

echo "==> Création de l'AppImage"
export APPIMAGE_EXTRACT_AND_RUN=1
export ARCH
"$TOOL" --no-appstream "$APPDIR" "$OUT"

echo
echo "AppImage prête : $OUT"
ls -lh "$OUT"
