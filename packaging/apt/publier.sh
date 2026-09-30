#!/bin/bash
# Publie un paquet dans le dépôt APT de Bernard (branche gh-pages, servie
# par GitHub Pages) : garde les 3 dernières versions, régénère les index,
# signe avec la clé du dépôt (déjà importée dans gpg).
#
#   packaging/apt/publier.sh DOSSIER_GH_PAGES PAQUET.deb ID_CLE
set -euo pipefail
PAGES=$(realpath "$1"); DEB=$(realpath "$2"); KEY=$3
HERE=$(dirname "$(realpath "$0")")
cd "$PAGES"
mkdir -p pool/main/b/bernard dists/stable/main/binary-amd64
cp "$DEB" pool/main/b/bernard/
# Trois dernières versions seulement (taille du dépôt).
ls -1 pool/main/b/bernard/bernard_*_amd64.deb | sort -V | head -n -3 | xargs -r rm -f
apt-ftparchive packages pool > dists/stable/main/binary-amd64/Packages
gzip -9kf dists/stable/main/binary-amd64/Packages
apt-ftparchive \
  -o APT::FTPArchive::Release::Origin=Bernard \
  -o APT::FTPArchive::Release::Label=Bernard \
  -o APT::FTPArchive::Release::Suite=stable \
  -o APT::FTPArchive::Release::Codename=stable \
  -o APT::FTPArchive::Release::Architectures=amd64 \
  -o APT::FTPArchive::Release::Components=main \
  -o APT::FTPArchive::Release::Description="Bernard, l'assistant de migration vers Linux" \
  release dists/stable > dists/stable/Release
rm -f dists/stable/InRelease dists/stable/Release.gpg
gpg --batch --yes --default-key "$KEY" --clearsign -o dists/stable/InRelease dists/stable/Release
gpg --batch --yes --default-key "$KEY" -abs -o dists/stable/Release.gpg dists/stable/Release
gpg --export "$KEY" > bernard-archive-keyring.gpg
cp "$HERE/index.html" index.html
touch .nojekyll
