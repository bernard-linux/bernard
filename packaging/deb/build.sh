#!/bin/bash
# Construit les paquets Debian dans dist/ :
#   bernard         moteur, agent, lanceur, polkit, entrée de menu (Go pur)
#   bernard-window  fenêtre dédiée (WebKitGTK) — à construire sur la version
#                   de distribution visée, car elle dépend de sa glibc.
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

# ---- bernard
S=$(mktemp -d)/bernard; stage $S
install -Dm755 bin/bernard       $S/usr/bin/bernard
install -Dm755 bin/bernard-agent $S/usr/bin/bernard-agent
install -Dm644 packaging/polkit/io.github.bernard_linux.bernard.policy $S/usr/share/polkit-1/actions/io.github.bernard_linux.bernard.policy
install -Dm644 packaging/applications/io.github.bernard_linux.bernard.desktop $S/usr/share/applications/io.github.bernard_linux.bernard.desktop
install -Dm644 packaging/icons/bernard.svg $S/usr/share/icons/hicolor/scalable/apps/bernard.svg
install -Dm644 LICENSE $S/usr/share/doc/bernard/copyright
install -Dm644 README.md $S/usr/share/doc/bernard/README.md
cat > $S/DEBIAN/control <<CTL
Package: bernard
Version: $VERSION
Architecture: $ARCH
Maintainer: $MAINT
Homepage: $URL
Section: admin
Priority: optional
Depends: pkexec | policykit-1
Recommends: bernard-window, network-manager, flatpak
Suggests: cups-client, cron, dconf-cli, dbus
Installed-Size: $(du -sk $S/usr | cut -f1)
Description: assistant de migration vers Linux
 Bernard transfère comptes, documents, réglages et applications d'un ancien
 ordinateur vers un nouvel ordinateur Linux, par le réseau (Wi-Fi, câble,
 Thunderbolt) ou par un disque externe chiffré. Chaque fichier est vérifié ;
 l'ancien ordinateur n'est jamais modifié ; tout est annulable.
CTL
dpkg-deb --build --root-owner-group $S dist/bernard_${VERSION}_${ARCH}.deb >/dev/null
echo "dist/bernard_${VERSION}_${ARCH}.deb"

# ---- bernard-window (si la fenêtre peut être compilée ici)
if pkg-config --exists webkit2gtk-4.1 2>/dev/null && make window >/dev/null 2>&1; then
  S=$(mktemp -d)/bernard-window; stage $S
  install -Dm755 bin/bernard-window $S/usr/bin/bernard-window
  install -Dm644 LICENSE $S/usr/share/doc/bernard-window/copyright
  LIBC=$(dpkg-query -W -f '${Version}' libc6 | cut -d- -f1)
  WK=$(dpkg-query -W -f '${Version}' libwebkit2gtk-4.1-0 | cut -d- -f1)
  cat > $S/DEBIAN/control <<CTL
Package: bernard-window
Version: $VERSION
Architecture: $ARCH
Maintainer: $MAINT
Homepage: $URL
Section: admin
Priority: optional
Depends: bernard (= $VERSION), libc6 (>= $LIBC), libwebkit2gtk-4.1-0 (>= $WK), libgtk-3-0
Installed-Size: $(du -sk $S/usr | cut -f1)
Description: fenêtre dédiée de Bernard
 Affiche l'assistant de migration Bernard dans sa propre fenêtre (WebKitGTK),
 avec les droits de l'utilisateur. Sans ce paquet, l'assistant s'ouvre dans
 le navigateur.
CTL
  dpkg-deb --build --root-owner-group $S dist/bernard-window_${VERSION}_${ARCH}.deb >/dev/null
  echo "dist/bernard-window_${VERSION}_${ARCH}.deb"
else
  echo "bernard-window non construit (libwebkit2gtk-4.1-dev absent)" >&2
fi
