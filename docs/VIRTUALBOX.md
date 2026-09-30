# Essais avec le vrai bureau (VirtualBox)

Le banc automatique (`tests/vm`, lancé par GitHub à chaque envoi) vérifie
services, disques, dépôts, réseau et annulation sur des systèmes sans bureau.
Ces essais-ci vérifient ce qu'un automate ne voit pas : la fenêtre, le bureau,
le clavier, les navigateurs, le confort d'utilisation.

## Préparer (une fois)

1. Installer VirtualBox (Mac Intel ou PC).
2. Télécharger les images d'installation : Zorin OS 17 et 18 (Core), Linux Mint
   21.3 et 22 (Cinnamon), Ubuntu 24.04.
3. **Réseau commun** : Outils → Réseau → Réseaux NAT → Créer (« BernardNet »).
   Dans chaque machine : Configuration → Réseau → Carte 1 : *Réseau NAT*,
   « BernardNet ». Les deux machines se voient et ont Internet.
4. Chaque machine : 4 Go de mémoire, 2 processeurs, 40 Go de disque,
   Affichage : 128 Mo, contrôleur VMSVGA.
5. Installer le système normalement (français, clavier belge ou français selon
   l'essai), puis **Machine → Prendre un instantané** : « installation neuve ».
   Chaque essai repart de là (Restaurer l'instantané).

## Préparer l'« ancien PC » d'un essai

Dans la machine source, en quelques minutes, faire comme un vrai utilisateur :

- quelques documents, photos, un gros fichier (film) ;
- Firefox **et** Chrome ou Brave : se connecter à un site, enregistrer un mot de
  passe, ajouter deux favoris ;
- changer le fond d'écran, la taille du texte, épingler deux applications au
  dock ou au tableau de bord ; sous Zorin, changer la disposition du tableau de
  bord (Apparence de Zorin) ;
- installer deux applications depuis la Logithèque (une en paquet, une en
  Flatpak), en désinstaller une fournie d'origine ;
- se connecter à un réseau Wi-Fi n'est pas possible en machine virtuelle : sans objet.

Puis installer Bernard (le `.deb` de la dernière version) sur les deux machines.

## Les cinq essais

| # | Ancien PC | Nouveau PC | Particularité à vérifier |
| --- | --- | --- | --- |
| 1 | Zorin 17 | Zorin 18 | Tableau de bord de Zorin, thème, extensions |
| 2 | Linux Mint 21.3 | Linux Mint 22 | Applets et panneaux Cinnamon |
| 3 | Zorin 18 | Linux Mint 22 | Bureau différent : réglages traduits GNOME → Cinnamon |
| 4 | Ubuntu 24.04 (clavier français) | Zorin 18 (clavier belge) | Le clavier du nouveau PC reste belge |
| 5 | Zorin 18, disque externe | Zorin 18 | Passage par disque externe (clé USB branchée à la machine virtuelle) |

Deux essais complémentaires, sur n'importe quelle paire :

- **coupure** : pendant la copie, décocher « Câble branché » dans la
  configuration réseau de la nouvelle machine, attendre 30 secondes, recocher :
  le transfert doit reprendre seul, sans nouveau code ;
- **disque FAT32** : passage par disque externe formaté en FAT32, avec un
  fichier de plus de 4 Go sur l'ancien PC.

## Dérouler un essai

1. Ouvrir Bernard sur les deux machines (menu des applications).
2. Nouvelle machine : « Ceci est le nouvel ordinateur » ; ancienne : « Ceci est
   l'ancien ordinateur », saisir le code.
3. Parcourir l'écran de choix : noter ce qui surprend (mots, options, valeurs).
4. Lancer, attendre la fin, lire le bilan, **redémarrer**.
5. Se connecter avec le compte migré et vérifier avec la liste du bilan
   (Documents → « Bilan de la migration (Bernard) ») : fichiers, navigateurs et
   mots de passe, fond d'écran, dock, applications, clavier.
6. Relancer Bernard → « Annuler la migration » : le nouveau PC doit revenir à
   son état d'avant.

## Rapporter

Pour chaque essai : numéro, ce qui a marché, ce qui manque ou surprend, avec
captures d'écran. Le plus utile : ce qu'un utilisateur non informaticien ne
comprendrait pas.
