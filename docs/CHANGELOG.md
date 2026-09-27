# Historique des versions

## 0.3.1 — à venir

- Publication : les versions 0.x ne sont plus marquées « préversion », pour
  que le lien fixe `releases/latest/download/bernard_amd64.deb` désigne
  bien la dernière version.
- Intégration continue : actions GitHub passées à leurs versions Node.js 24
  (checkout v5, setup-go v6, upload-artifact v6, download-artifact v7).

## 0.3.0 — 27 septembre 2026

Corrections issues du premier essai réel Zorin OS → Zorin OS (deux PC
différents). **Installez la 0.3.0 sur les deux ordinateurs** : l'ancien
ordinateur doit aussi l'avoir pour que les nouveautés marquées (★)
fonctionnent.

**Navigateurs**

- (★) Les verrous des navigateurs ne sont plus copiés (`SingletonLock` de
  Brave, Chrome, Chromium, Edge, Vivaldi ; `lock` et `.parentlock` de
  Firefox et Thunderbird). Ils désignaient l'ancien ordinateur et pouvaient
  empêcher le navigateur de démarrer.
- (★) Les caches graphiques des navigateurs (`GPUCache`, `ShaderCache`,
  `GrShaderCache`…) et leurs autres caches ne sont plus copiés : compilés
  pour la carte graphique de l'ancien ordinateur, ils pouvaient faire
  planter le démarrage sur une autre carte. Favoris, historique, mots de
  passe, extensions, cookies et réglages sont bien repris. Versions paquet,
  Flatpak et Snap.
- Compte qui existait déjà sur un système fraîchement installé (moins de
  45 jours) : le trousseau de clés et les fichiers de profil des navigateurs
  de l'ancien ordinateur prennent la place de ceux, vierges, du nouveau, au
  lieu d'arriver sous un nom « (bernard 1) ». Sans cela, les mots de passe
  enregistrés dans Brave, Chrome ou Chromium restaient illisibles, et
  Firefox pouvait continuer d'utiliser un profil vide. Les fichiers du
  nouvel ordinateur sont gardés dans `~/.local/share/bernard/avant-migration`,
  et « Annuler la migration » les remet en place.
- Écran de fin : bouton « Redémarrer maintenant » ; le trousseau et les
  réglages du bureau ne sont pris en compte qu'à la session suivante.

**Compte provisoire du nouvel ordinateur**

- Si le nouvel ordinateur ouvre seul la session d'un compte qui ne vient pas
  de l'ancien (compte « tmp » créé pour l'installation), cette ouverture
  automatique est coupée pendant la migration : au redémarrage, l'écran de
  connexion laisse choisir le compte migré. GDM (Ubuntu, Zorin, Debian) et
  LightDM (Mint). La ligne figure dans « Réglages » sur l'écran de choix ;
  « Annuler la migration » remet la configuration d'origine.
- Écran de fin : section « Compte provisoire » avec le nombre de fichiers
  personnels du compte. Bouton « Supprimer au prochain démarrage » (après
  confirmation) : le compte et son dossier sont supprimés au démarrage
  suivant, avant l'écran de connexion. Changement d'avis possible jusque-là
  (« Garder ce compte »). Refusé si aucun autre compte n'est administrateur,
  ou si une session du compte est ouverte. Nouvelle commande
  `sudo bernard remove-account IDENTIFIANT`.

**Chaque ordinateur garde ce qui tient à son matériel**

- La disposition du clavier n'est plus reprise quand les deux ordinateurs
  n'ont pas le même clavier (clavier français → clavier belge, par exemple) :
  celle choisie à l'installation reste. Une case, dans « Options avancées »,
  permet de reprendre celle de l'ancien.
- Les profils de couleur des écrans ne sont plus repris.
- Pilotes graphiques (NVIDIA…), micrologiciels, noyaux, amorçage, outils de
  machines virtuelles : jamais installés depuis l'ancien ordinateur, jamais
  retirés du nouveau. L'écran « Options avancées » indique les cartes
  graphiques des deux ordinateurs.

**Retrait des applications que vous aviez supprimées**

- (★) L'ancien ordinateur envoie la liste complète de ses paquets et, d'après
  le journal de dpkg, ceux qu'il a retirés et quand.
- Le nouvel ordinateur propose de retirer les applications du menu qu'il a
  reçues d'office à l'installation mais que l'ancien n'a pas. Une ligne sur
  l'écran de choix l'annonce (« 3 applications … seront retirées d'ici,
  25 Mo libérés ») ; le détail, case par case, est dans « Options avancées ».
- Garde-fous : même distribution des deux côtés ; seulement des applications
  du menu, jamais des bibliothèques ; jamais un pilote, un élément du système
  ou du bureau, ni un paquet protégé ; simulation apt avant de proposer, et
  aucun retrait qui emporterait un paquet présent sur l'ancien ordinateur.
  Non cochés d'office : les retraits qui emportent d'autres paquets, et ceux
  entre deux versions différentes du système (sauf retrait attesté par le
  journal de l'ancien).
- La place libérée compte dans le calcul de l'espace disponible.
- Retrait simple (`apt remove`, jamais `purge`), fait avant les
  installations. « Annuler la migration » réinstalle ce qui a été retiré.

## 0.2.1 — 27 septembre 2026

- Lien de téléchargement fixe, toujours vers la dernière version :
  `https://github.com/bernard-linux/bernard/releases/latest/download/bernard_amd64.deb`
  (la publication ajoute une copie du paquet sous ce nom).
- Le README et le guide d'essai indiquent où télécharger le paquet.
- Aucun changement du programme par rapport à la 0.2.0.

## 0.2.0 — 27 septembre 2026

**Interface graphique des deux côtés.** Le même programme s'ouvre sur l'ancien
et le nouvel ordinateur ; plus aucun terminal n'est nécessaire.

- Premier écran : « Ceci est le nouvel ordinateur » ou « Ceci est l'ancien
  ordinateur ». Côté ancien : attente du nouvel ordinateur (sans limite, sans
  mise en veille), saisie du code dans six cases, suivi de l'envoi (étape en
  cours sur le nouvel ordinateur, volume, débit, temps restant, liaison
  utilisée), écran de fin ; ou paquet chiffré sur un disque externe, avec
  choix du disque et vérification de l'espace libre.
- Le nouvel ordinateur annonce son étape à l'ancien (choix en cours,
  installation des applications, copie, réglages).
- Arrêt et reprise depuis l'interface : l'arrêt est immédiat, même au milieu
  d'un gros fichier ; le nouvel ordinateur affiche alors un nouveau code pour
  reprendre.
- La fenêtre dédiée fait partie du paquet `bernard` : un seul fichier à
  installer, pour toutes les versions prises en charge. L'ancien paquet
  `bernard-window` est remplacé automatiquement.
- La mise en veille est bloquée pendant toute la migration, des deux côtés.

**Beaucoup plus rapide sur les petits fichiers** (dossiers de configuration,
jeux, projets) : environ 6 fois plus de fichiers par seconde en essai, et
davantage sur un disque dur classique.

- Les fichiers sont demandés d'avance à l'ancien ordinateur (plus d'aller-
  retour réseau par fichier), et ses réponses sont regroupées.
- Les petits fichiers sont validés par lots : deux passages sur le disque par
  lot de 256 fichiers au lieu de cinq par fichier. Aucun fichier n'est rangé
  sous son nom définitif avant d'être sur le disque et vérifié ; une coupure
  fait seulement recevoir à nouveau le lot en cours.
- Moins d'allocations mémoire par fichier.

**Bascule automatique vers une liaison plus rapide.** Un câble réseau branché
pendant un transfert en Wi-Fi est repéré en quelques secondes ; l'ancien
ordinateur bascule dessus sans rien interrompre. Les balises partent
désormais de chaque interface avec sa propre adresse, pour que le câble et le
Wi-Fi soient distingués même sur la même box.

**Corrections**

- L'avancement côté ancien ordinateur progresse pendant les gros fichiers (il
  restait figé jusqu'à la fin de chaque fichier).
- `--root` (disque d'un ancien PC monté ailleurs) : les applications et
  imprimantes sont lues dans ce système, et non plus sur la machine qui
  exécute l'agent.
- Les disques proposés pour le paquet sont de vrais disques inscriptibles.

## 0.1.1 — 26 septembre 2026

- `bernard-agent connect` attend le nouvel ordinateur **sans limite de durée**
  au lieu d'abandonner après 4 secondes. On peut lancer l'agent sur l'ancien
  ordinateur, puis installer Linux sur le nouveau et y ouvrir Bernard des
  heures plus tard. L'écoute est passive : aucun trafic réseau. Un message
  « toujours en attente » s'affiche chaque minute ; `--timeout 2h` fixe une
  limite, Ctrl+C abandonne.
- L'inventaire de l'ancien ordinateur est fait **au moment où le nouveau est
  trouvé**, et non plus au lancement : les fichiers créés ou modifiés pendant
  l'attente sont bien envoyés.
- La mise en veille de l'ancien ordinateur est **bloquée** pendant l'attente
  et le transfert (`systemd-inhibit`, relâché automatiquement à la fin de
  l'agent, même en cas d'arrêt brutal). En administrateur, la fermeture du
  capot d'un portable ne le met pas non plus en veille. Même protection pour
  `bernard-agent pack`.

## 0.1.0 — 26 septembre 2026

Première version publique : migration Linux → Linux (jalon V1.0).
