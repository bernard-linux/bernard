#!/bin/bash
# Banc d'essai en machines virtuelles — vérifications sur le NOUVEL
# ordinateur (en administrateur). Phase : apres | redemarrage | annulation.
# Imprime une ligne par vérification ; code de sortie = nombre d'échecs.
set -u
PHASE=$1
FAILS=0
ok()   { echo "  ok    $*"; }
bad()  { echo "  ÉCHEC $*"; FAILS=$((FAILS+1)); }
skip() { echo "  sauté $*"; }
check(){ local what=$1; shift; if "$@" >/dev/null 2>&1; then ok "$what"; else bad "$what"; fi; }
UUID=$(cat /root/banc/uuid-donnees 2>/dev/null)
sql()  { mysql -N -e "$1" 2>/dev/null; }

case $PHASE in
apres)
  check "comptes alice et bob créés"                 bash -c "id alice && id bob"
  check "alice administratrice"                      bash -c "id -nG alice | grep -qw sudo"
  check "mot de passe d'alice repris"                bash -c "grep '^alice:' /etc/shadow | cut -d: -f2 | grep -q '^\\$'"
  check "fichiers identiques (empreintes)"           bash -c "cd / && sha256sum -c --quiet /root/banc/empreintes.txt"
  check "lien dur recréé"                            test "$(stat -c %i /home/alice/Images/album.tar)" = "$(stat -c %i /home/alice/Sauvegarde/album.tar)"
  check "fichier creux resté creux"                  test "$(du -k /opt/appli/disque.img | cut -f1)" -lt 10240
  check "site web à www-data"                        test "$(stat -c %U /var/www/html/site/index.html)" = www-data
  check "MariaDB installée et active"                systemctl is-active --quiet mariadb
  if [ -f /root/banc/meme-version ]; then
    check "MariaDB : base du nouveau PC mise de côté" bash -c "ls -d /var/lib/bernard/*/avant-migration/services/var/lib/mysql/cible"
    check "MariaDB : données de l'ancien PC"         test "$(sql 'SELECT texte FROM banc.notes WHERE id=1')" = "bonjour depuis ancien-pc"
  else skip "MariaDB : données (versions de système différentes : base volontairement non copiée)"; fi
  check "Docker installé et actif"                   systemctl is-active --quiet docker
  check "Docker : image reprise"                     docker image inspect banc/image:1
  check "Docker : conteneur repris"                  bash -c "docker ps -a --format '{{.Names}}' | grep -qx banc-conteneur"
  check "Docker : volume repris"                     test -f /var/lib/docker/volumes/banc-volume/_data/fichier.txt
  check "disque déplacé rattaché dans /etc/fstab"    grep -q "UUID=$UUID" /etc/fstab
  if [ -f /root/banc/depot-tiers ]; then
    check "dépôt tiers (VS Code) repris"             bash -c "grep -rqs packages.microsoft.com /etc/apt/sources.list.d/"
    check "dépôt tiers utilisable"                   bash -c "apt-cache policy | grep -q packages.microsoft.com"
  else skip "dépôt tiers (pas d'Internet dans ce banc)"; fi
  ;;
redemarrage)
  check "disque déplacé monté au démarrage"          bash -c "findmnt -rn -S UUID=$UUID"
  check "disque déplacé : fichiers présents"         bash -c "test -f \$(findmnt -rn -o TARGET -S UUID=$UUID)/rapport.txt"
  check "compte provisoire tmp supprimé"             bash -c "! id tmp && test ! -e /home/tmp"
  check "unité de suppression effacée"               test ! -e /etc/systemd/system/bernard-supprimer-compte-tmp.service
  check "MariaDB active après redémarrage"           systemctl is-active --quiet mariadb
  [ -f /root/banc/meme-version ] && check "MariaDB : données après redémarrage" test "$(sql 'SELECT texte FROM banc.notes WHERE id=1')" = "bonjour depuis ancien-pc"
  check "Docker : conteneur après redémarrage"       bash -c "docker ps -a --format '{{.Names}}' | grep -qx banc-conteneur"
  ;;
annulation)
  check "comptes retirés"                            bash -c "! id alice && ! id bob"
  check "/etc/fstab remis"                           bash -c "! grep -q 'UUID=$UUID' /etc/fstab"
  check "/srv/projets retiré"                        test ! -e /srv/projets/plan.txt
  check "/opt/appli retiré"                          test ! -e /opt/appli/bin/appli
  check "site web retiré"                            test ! -e /var/www/html/site/index.html
  if [ -f /root/banc/depot-tiers ]; then
    check "dépôt tiers retiré"                       bash -c "! grep -rqs packages.microsoft.com /etc/apt/sources.list.d/"
  fi
  check "MariaDB du nouveau PC active"               systemctl is-active --quiet mariadb
  check "base du nouveau PC rendue intacte"          bash -c "mysql -N -e 'SHOW DATABASES' | grep -qx cible"
  check "base de l'ancien PC retirée"                bash -c "! mysql -N -e 'SHOW DATABASES' | grep -qx banc"
  ;;
esac
exit $FAILS
