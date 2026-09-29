# Données hors des dossiers personnels

Principe : **tout ce qui fait l'ordinateur de l'utilisateur suit, sans
concession**. Seul ce qui tient au matériel reste propre à chaque machine
(pilotes, micrologiciels, noyau, amorçage, clavier physique, profils d'écran,
appairages Bluetooth).

« Reconstruire, ne pas cloner » reste la méthode : ce qu'un paquet a installé
et que personne n'a modifié est réinstallé ; tout le reste est copié et
vérifié.

## Détection (version 0.4)

L'agent source examine tout le disque, en lecture seule
(`internal/collect/linux/sysdata.go`) :

| Zone | Règle |
| --- | --- |
| `/etc` | Fichiers de configuration modifiés (empreinte dpkg différente) et fichiers ajoutés. Exclus : identité de la machine, disques (`fstab`), amorçage, pilotes, clavier, réseau et comptes (traités à part), fichiers régénérés par les paquets. |
| `/opt/<x>`, `/srv/<x>` | Un élément par dossier, fichiers n'appartenant à aucun paquet. |
| `/usr/local` | Fichiers ajoutés à la main. |
| `/var/www` | Sites web. |
| `/var/lib/<x>` | Services reconnus (MySQL/MariaDB, PostgreSQL, MongoDB, Redis, Docker, Podman, libvirt) ; autres services au-delà de 1 Mo, à examiner. Données propres au système exclues. |
| `/opt/FileMaker/…/Data` | Serveur FileMaker. |
| Racine | Dossiers non standard (`/data`, `/projets`…) ; `/timeshift` = sauvegarde. |
| `/home` | Dossiers sans compte (partage, ancien compte). |
| `/root` | Dossier de l'administrateur, à examiner. |
| Autres disques montés | Classés : sauvegarde (copie déconseillée), bibliothèque de jeux, dossier personnel déplacé, autre disque (à rattacher tel quel ou à copier). |
| Steam | Bibliothèques déclarées dans `libraryfolders.vdf` hors du dossier personnel. |

Chaque élément porte sa taille apparente **et la place réellement occupée** :
les fichiers creux (disques virtuels) devront être copiés creux.

## Décisions (27 septembre 2026)

1. Tout doit pouvoir être transféré ; l'outil vise le plus grand nombre
   (BOB, FileMaker, Docker, machines virtuelles compris).
2. Disques supplémentaires : Bernard analyse et conseille (dossier personnel
   déplacé = données actives ; sauvegarde évaluée selon l'espace ; cible à
   plusieurs disques : choix de l'emplacement ; disque déplacé physiquement :
   rattaché tel quel).
3. Docker et machines virtuelles : copie de l'image complète, service arrêté
   pendant la copie, **fichiers creux conservés** (primordial).
4. Cible plus petite : jamais bloquant tant que les dossiers personnels
   tiennent ; priorité dossiers personnels, puis applications, puis données
   système, puis sauvegardes ; alerte, journal, et renvoi vers les options
   avancées.

## Suite

- 0.5 : copie (propriétaires, droits, ACL, attributs étendus, liens durs,
  fichiers creux), `/etc` comparé à la cible, dépôts tiers, services
  systemd, annulation.
- 0.6 : copie cohérente des bases (export/import) et des services arrêtés.
