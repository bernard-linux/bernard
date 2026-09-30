#!/bin/bash
# Banc d'essai en machines virtuelles — prépare l'ANCIEN ordinateur (en
# administrateur, dans la machine « ancien-pc ») : comptes et fichiers, site
# web, logiciel dans /opt, base MariaDB, Docker (image, conteneur, volume),
# disque de données monté par /etc/fstab, dépôt de logiciels tiers (si
# Internet est joignable). Écrit /root/banc/empreintes.txt : empreintes des
# fichiers que la cible devra avoir à l'identique.
set -eux
export DEBIAN_FRONTEND=noninteractive
mkdir -p /root/banc

apt-get update -q
apt-get install -y -q --no-install-recommends mariadb-server docker.io

# Comptes : alice (administratrice), bob. Mots de passe définis : leurs
# empreintes sont reprises, aucune question pendant la migration.
id alice 2>/dev/null || useradd -m -s /bin/bash -G sudo alice
id bob 2>/dev/null || useradd -m -s /bin/bash bob
echo 'alice:Banc-alice-1' | chpasswd
echo 'bob:Banc-bob-1' | chpasswd
sudo -u alice mkdir -p /home/alice/Documents /home/alice/Images /home/alice/Sauvegarde
echo "lettre de l'ancien PC" | sudo -u alice tee "/home/alice/Documents/lettre été.odt" >/dev/null
sudo -u alice dd if=/dev/urandom of=/home/alice/Images/album.tar bs=1M count=40 status=none
sudo -u alice ln -f /home/alice/Images/album.tar /home/alice/Sauvegarde/album.tar
echo "notes de bob" | sudo -u bob tee /home/bob/notes.txt >/dev/null

# Site web (propriétaire www-data), logiciel dans /opt (fichier creux), /srv.
mkdir -p /var/www/html/site /opt/appli/bin /srv/projets
echo "<h1>site de l'ancien PC</h1>" > /var/www/html/site/index.html
chown -R www-data:www-data /var/www/html/site
printf '#!/bin/sh\necho appli\n' > /opt/appli/bin/appli && chmod 755 /opt/appli/bin/appli
truncate -s 300M /opt/appli/disque.img
echo "données" | dd of=/opt/appli/disque.img bs=1 seek=150000000 conv=notrunc status=none
echo "plan du projet" > /srv/projets/plan.txt

# Base MariaDB avec des données.
systemctl enable --now mariadb
mysql -e "CREATE DATABASE IF NOT EXISTS banc; CREATE TABLE IF NOT EXISTS banc.notes (id INT PRIMARY KEY, texte VARCHAR(80)); REPLACE INTO banc.notes VALUES (1, 'bonjour depuis ancien-pc');"

# Docker : image importée (sans Internet), conteneur créé, volume rempli.
systemctl enable --now docker
mkdir -p /tmp/img && echo "contenu de l'image" > /tmp/img/fichier
tar -C /tmp/img -cf /tmp/img.tar . && docker import /tmp/img.tar banc/image:1
docker volume create banc-volume
docker create --name banc-conteneur -v banc-volume:/donnees banc/image:1 /fichier
echo "données du volume" > /var/lib/docker/volumes/banc-volume/_data/fichier.txt

# Disque de données (fourni par la machine virtuelle, déjà formaté,
# étiquette DONNEES), monté par /etc/fstab comme sur un vrai PC.
DEV=$(blkid -L DONNEES)
UUID=$(blkid -s UUID -o value "$DEV")
mkdir -p /mnt/donnees
grep -q "$UUID" /etc/fstab || echo "UUID=$UUID /mnt/donnees ext4 defaults 0 2" >> /etc/fstab
mount /mnt/donnees
echo "$UUID" > /root/banc/uuid-donnees

# Dépôt tiers (Visual Studio Code) : seulement si Internet est joignable.
if curl -fsS -m 20 https://packages.microsoft.com/keys/microsoft.asc -o /tmp/ms.asc 2>/dev/null; then
  gpg --dearmor < /tmp/ms.asc > /usr/share/keyrings/microsoft.gpg 2>/dev/null || apt-get install -y -q gpg && gpg --dearmor < /tmp/ms.asc > /usr/share/keyrings/microsoft.gpg
  echo "deb [arch=amd64 signed-by=/usr/share/keyrings/microsoft.gpg] https://packages.microsoft.com/repos/code stable main" > /etc/apt/sources.list.d/vscode.list
  apt-get update -q
  touch /root/banc/depot-tiers
fi

cd /
sha256sum "home/alice/Documents/lettre été.odt" home/alice/Images/album.tar home/bob/notes.txt \
  var/www/html/site/index.html opt/appli/bin/appli opt/appli/disque.img srv/projets/plan.txt \
  var/lib/docker/volumes/banc-volume/_data/fichier.txt > /root/banc/empreintes.txt
sync
echo "ANCIEN PC PRÊT"
