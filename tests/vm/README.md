# Banc d'essai en machines virtuelles

Deux vraies machines (« ancien-pc » et « nouveau-pc ») reliées par un réseau
privé, migration réelle, vérifications. Lancé par GitHub à chaque envoi
(`.github/workflows/banc.yml`, avec KVM) sur des images cloud officielles :
Ubuntu 22.04 et 24.04, Debian 12, et Ubuntu 22.04 → 24.04.

Ce que les conteneurs de `tests/e2e` ne peuvent pas montrer, et que ce banc
vérifie :

- la recherche automatique du nouvel ordinateur sur un vrai réseau ;
- une vraie base MariaDB (service arrêté puis relancé des deux côtés, base
  déjà présente sur le nouveau PC mise de côté en entier puis rendue par
  l'annulation) ;
- Docker (image, conteneur, volume ; images dans containerd depuis Docker 29) ;
- un disque de données sorti de l'ancien PC et mis dans le nouveau : rattaché
  par /etc/fstab, monté au redémarrage ;
- un dépôt tiers (Visual Studio Code) quand Internet est joignable ;
- la suppression du compte provisoire au redémarrage ;
- l'annulation complète.

## Lancer à la main

```sh
make build
sudo tests/vm/run.sh bin ancien.qcow2 nouveau.qcow2
```

Sans `/dev/kvm`, QEMU émule : compter une heure. `BANC_GARDER=1` laisse les
machines allumées (une relance reprend sans tout refaire), `BANC_DIR` garde
les fichiers de travail et journaux.

Les essais avec le vrai bureau (fenêtre, Zorin, Mint, navigateurs, clavier) se
font à la main : voir `docs/VIRTUALBOX.md`.
