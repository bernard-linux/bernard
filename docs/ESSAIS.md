# Guide d'essai entre deux ordinateurs Linux

Objectif : valider une vraie migration Linux → Linux, en particulier ce que
l'environnement de développement ne peut pas tester (Flathub, vrais réseaux,
vraie fenêtre, vraies coupures).

Utilisez une machine **cible** fraîchement installée, ou dont vous acceptez
qu'elle reçoive des comptes et des applications : l'annulation existe, mais
c'est justement l'une des choses à tester.

## 1. Installation (sur les deux machines)

Téléchargez le paquet de la dernière version sur la page
[Releases](https://github.com/bernard-linux/bernard/releases/latest) du dépôt (fichier `bernard_amd64.deb`), puis
double-cliquez dessus. Ou, dans un terminal :

```sh
cd /tmp
wget https://github.com/bernard-linux/bernard/releases/latest/download/bernard_amd64.deb
sudo apt install ./bernard_amd64.deb
```

Le paquet se télécharge dans `/tmp` plutôt que dans `~/Téléchargements` : apt
peut alors le lire directement, sans afficher la remarque « téléchargement
effectué en dehors du bac à sable ».

Un seul paquet, fenêtre dédiée comprise. Sur une machine qui avait la 0.1,
l'ancien paquet `bernard-window` est retiré automatiquement.

Pare-feu : si `sudo ufw status` indique « active » sur la cible, ouvrez
`sudo ufw allow 51515/udp` et `sudo ufw allow 51516/tcp`.

## 2. Parcours à tester

Tout se fait dans l'interface graphique, sur les deux machines.

1. Source : menu → Bernard → « Ceci est l'ancien ordinateur » →
   « Directement au nouvel ordinateur ». Elle attend.
   Variante à tester : lancer la source **avant** la cible (voire avant
   d'installer Linux sur la cible) ; elle doit patienter sans se mettre en
   veille, puis trouver la cible dès son apparition.
2. Cible : menu → Bernard → « Ceci est le nouvel ordinateur » → « Depuis un
   autre ordinateur ». Le mot de passe administrateur est demandé. Noter le code.
3. Source : l'écran « Nouvel ordinateur trouvé » apparaît ; saisir le code.
   Essayer d'abord un mauvais code : le message doit être clair.
4. Cible : vérifier l'écran de choix (comptes, dossiers, applications), lancer.
   La source affiche « Faites votre choix », puis « Installation des comptes
   et des applications », puis la progression de l'envoi.
5. Pendant l'envoi, en Wi-Fi : **brancher un câble réseau** sur les deux
   machines (ou vers la box). En moins de 15 secondes, le champ « Liaison »
   de la source doit passer à « Câble réseau (RJ45) » et le débit augmenter.
   Puis débrancher le câble : bandeau « Liaison perdue », reprise seule par le
   Wi-Fi.
6. Tester « Arrêter » sur la source : la cible affiche un nouveau code ;
   « Reprendre » sur la source, saisir ce code, l'envoi reprend sans renvoyer
   ce qui est déjà arrivé.
7. Au bilan : « Redémarrer maintenant », se connecter avec le compte migré,
   vérifier fichiers, mot de passe, applications (dont une Flatpak), fond
   d'écran, dock, Wi-Fi mémorisé, imprimante réseau. La disposition du
   clavier doit être celle choisie à l'installation du nouvel ordinateur
   (sauf case cochée dans « Options avancées »).
   Navigateurs : chaque navigateur démarre du premier coup ; favoris
   présents ; **mots de passe enregistrés lisibles** (Brave/Chrome :
   Paramètres → Mots de passe ; Firefox : about:logins).
   Applications retirées : celles cochées dans « Options avancées » ont
   disparu du menu, les autres sont restées.
   Compte provisoire : si la cible ouvrait seule la session « tmp », l'écran
   de connexion apparaît au redémarrage. Tester « Supprimer au prochain
   démarrage » puis « Garder ce compte », puis de nouveau « Supprimer » et
   redémarrer : le compte a disparu de l'écran de connexion.
8. Relancer Bernard et tester « Annuler la migration » sur une migration,
   si la machine peut être remise à zéro.

Point à surveiller : la vitesse sur les dossiers pleins de petits fichiers
(`.config`, `.local/share`, jeux) doit être nettement meilleure qu'en 0.1.

## 3. À noter pour chaque essai

- versions (Zorin, Ubuntu…) des deux machines ;
- liaison utilisée (Wi-Fi, câble, câble direct) et débit affiché ;
- tout message qui vous a semblé obscur ou faux ;
- le rapport `/var/lib/bernard/<…>/rapport.json` et le journal
  `journal.jsonl` du même dossier en cas de problème.

## 4. Essai sans réseau

Source : Bernard → « Ceci est l'ancien ordinateur » → « Sur un disque
externe » : choisir le disque, la phrase de passe, écrire. Cible : Bernard →
« Ceci est le nouvel ordinateur » → « Depuis un disque externe ».

```sh
# Équivalent en ligne de commande, côté source
sudo bernard-agent pack --dest /media/$USER/<disque>/migration
```
