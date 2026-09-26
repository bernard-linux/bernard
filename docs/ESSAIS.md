# Guide d'essai entre deux ordinateurs Linux

Objectif : valider une vraie migration Linux → Linux, en particulier ce que
l'environnement de développement ne peut pas tester (Flathub, vrais réseaux,
vraie fenêtre, vraies coupures).

Utilisez une machine **cible** fraîchement installée, ou dont vous acceptez
qu'elle reçoive des comptes et des applications : l'annulation existe, mais
c'est justement l'une des choses à tester.

## 1. Installation (sur les deux machines)

```sh
unzip bernard-*.zip && cd bernard
sudo apt install ./dist/bernard_*.deb
```

Sur une base Ubuntu 24.04 (Zorin 18, Mint 22), ajoutez la fenêtre dédiée :
`sudo apt install ./dist/bernard-window_*.deb`.

Sur la cible, pour la fenêtre dédiée (facultatif) :
`sudo apt install gcc pkg-config libwebkit2gtk-4.1-dev` et Go 1.22 ou plus
récent (`sudo snap install go --classic` si celui des dépôts est trop
ancien, ce qui est le cas sur Zorin 17), puis `make window` et
`sudo install -m755 bin/bernard-window /usr/bin/`. Sans la fenêtre, Bernard
s'ouvre dans le navigateur : c'est un essai tout aussi valable.

Pare-feu : si `sudo ufw status` indique « active » sur la cible, ouvrez
`sudo ufw allow 51515/udp` et `sudo ufw allow 51516/tcp`.

## 2. Parcours à tester

1. Cible : menu → Bernard. Le mot de passe administrateur est demandé.
2. Choisir « Depuis un autre ordinateur ». Noter le code.
3. Source : `sudo bernard-agent connect`. La cible doit être trouvée seule.
   Variante à tester : lancer l'agent **avant** d'ouvrir Bernard sur la cible
   (voire avant d'installer Linux) ; il doit attendre, sans que la source se
   mette en veille, puis trouver la cible dès son apparition.
4. Saisir le code. Vérifier l'écran de choix : comptes, dossiers, applications.
5. Lancer, puis pendant le transfert :
   - débrancher le câble réseau si vous êtes en filaire avec le Wi-Fi actif :
     le bandeau « Liaison perdue » doit apparaître puis disparaître seul ;
   - ou couper le Wi-Fi quelques secondes.
6. Au bilan : se déconnecter, se connecter avec le compte migré, vérifier
   fichiers, mot de passe, applications (dont une Flatpak), fond d'écran,
   disposition du clavier, dock, Wi-Fi mémorisé, imprimante réseau.
7. Relancer Bernard et tester « Annuler la migration » sur une migration,
   si la machine peut être remise à zéro.

## 3. À noter pour chaque essai

- versions (Zorin, Ubuntu…) des deux machines ;
- liaison utilisée (Wi-Fi, câble, câble direct) et débit affiché ;
- tout message qui vous a semblé obscur ou faux ;
- le rapport `/var/lib/bernard/<…>/rapport.json` et le journal
  `journal.jsonl` du même dossier en cas de problème.

## 4. Essai sans réseau

```sh
# Source
sudo bernard-agent pack --dest /media/$USER/<disque>/migration
# Cible : menu → Bernard → « Depuis un disque externe »
```
