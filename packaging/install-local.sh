#!/bin/sh
# Installation d'essai à partir des binaires de dist/ (sans compilation).
# Usage : sudo ./packaging/install-local.sh
set -e
cd "$(dirname "$0")/.."
if [ "$(id -u)" -ne 0 ]; then echo "Lancez ce script avec sudo."; exit 1; fi
for b in bernard bernard-agent bernard-window; do
  for d in dist bin; do
    if [ -x "$d/$b" ]; then install -Dm755 "$d/$b" "/usr/bin/$b"; echo "installé : /usr/bin/$b"; break; fi
  done
done
install -Dm644 packaging/polkit/io.github.bernard_linux.bernard.policy /usr/share/polkit-1/actions/io.github.bernard_linux.bernard.policy
install -Dm644 packaging/applications/io.github.bernard_linux.bernard.desktop /usr/share/applications/io.github.bernard_linux.bernard.desktop
install -Dm644 packaging/icons/bernard.svg /usr/share/icons/hicolor/scalable/apps/bernard.svg
command -v bernard-window >/dev/null || echo "Fenêtre dédiée absente : Bernard s'ouvrira dans le navigateur (voir README pour la compiler)."
echo "Bernard est dans le menu des applications, ou : bernard gui"
