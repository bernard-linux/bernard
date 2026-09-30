#!/bin/bash
# Construit le paquet Debian dans dist/ : moteur, agent, lanceur, fenêtre
# dédiée (WebKitGTK), polkit, entrée de menu. Il remplace l'ancien paquet
# séparé « bernard-window » (versions 0.1.x).
#
#   packaging/deb/build.sh 1.0.0
set -euo pipefail
cd "$(dirname "$0")/../.."
VERSION=${1:?version attendue, ex. 1.0.0}
ARCH=$(dpkg --print-architecture)
MAINT="Bernard <bernard-linux@users.noreply.github.com>"
URL="https://github.com/bernard-linux/bernard"
mkdir -p dist
make build VERSION=$VERSION >/dev/null

stage() { rm -rf "$1" && mkdir -p "$1/DEBIAN"; }

# ---- bernard : un seul paquet, fenêtre dédiée comprise.
# La fenêtre dépend de la glibc et de WebKitGTK de la machine de
# construction : la publication construit sur la plus ancienne base prise en
# charge (Ubuntu 22.04), dont le binaire fonctionne aussi sur les suivantes.
S=$(mktemp -d)/bernard; stage $S
install -Dm755 bin/bernard       $S/usr/bin/bernard
install -Dm755 bin/bernard-agent $S/usr/bin/bernard-agent
install -Dm644 packaging/polkit/io.github.bernard_linux.bernard.policy $S/usr/share/polkit-1/actions/io.github.bernard_linux.bernard.policy
install -Dm644 packaging/applications/io.github.bernard_linux.bernard.desktop $S/usr/share/applications/io.github.bernard_linux.bernard.desktop
install -Dm644 packaging/icons/bernard.svg $S/usr/share/icons/hicolor/scalable/apps/bernard.svg
install -Dm644 LICENSE $S/usr/share/doc/bernard/copyright
install -Dm644 README.md $S/usr/share/doc/bernard/README.md

# Dépôt APT de Bernard (mises à jour), si la clé publique du dépôt est
# présente dans les sources (packaging/apt/bernard-archive-keyring.gpg).
CONFFILES=""
if [ -s packaging/apt/bernard-archive-keyring.gpg ]; then
  install -Dm644 packaging/apt/bernard-archive-keyring.gpg $S/usr/share/keyrings/bernard-archive-keyring.gpg
  install -Dm644 packaging/apt/bernard.sources $S/etc/apt/sources.list.d/bernard.sources
  CONFFILES="/etc/apt/sources.list.d/bernard.sources"
fi

DEPS="pkexec | policykit-1"
if pkg-config --exists webkit2gtk-4.1 2>/dev/null && make window >/dev/null 2>&1; then
  install -Dm755 bin/bernard-window $S/usr/bin/bernard-window
  LIBC=$(dpkg-query -W -f '${Version}' libc6 | cut -d- -f1)
  WK=2.36 # API utilisée présente depuis WebKitGTK 2.36 (Ubuntu 22.04)
  DEPS="$DEPS, libc6 (>= $LIBC), libwebkit2gtk-4.1-0 (>= $WK), libgtk-3-0"
else
  echo "Attention : fenêtre dédiée non construite (libwebkit2gtk-4.1-dev absent) ; l'assistant s'ouvrira dans le navigateur." >&2
fi

cat > $S/DEBIAN/control <<CTL
Package: bernard
Version: $VERSION
Architecture: $ARCH
Maintainer: $MAINT
Homepage: $URL
Section: admin
Priority: optional
Depends: $DEPS
Recommends: network-manager, flatpak
Suggests: cups-client, cron, dconf-cli, dbus
Conflicts: bernard-window
Replaces: bernard-window
Provides: bernard-window
Installed-Size: $(du -sk $S/usr | cut -f1)
Description: assistant de migration vers Linux
 Bernard transfère comptes, documents, réglages et applications d'un ancien
 ordinateur vers un nouvel ordinateur Linux, par le réseau (Wi-Fi, câble,
 Thunderbolt) ou par un disque externe chiffré. Chaque fichier est vérifié ;
 l'ancien ordinateur n'est jamais modifié ; tout est annulable. Le même
 programme sert sur les deux ordinateurs, avec une interface graphique.
CTL
[ -n "$CONFFILES" ] && echo "$CONFFILES" > $S/DEBIAN/conffiles
dpkg-deb --build --root-owner-group $S dist/bernard_${VERSION}_${ARCH}.deb >/dev/null
echo "dist/bernard_${VERSION}_${ARCH}.deb"
