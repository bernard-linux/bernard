# Historique des versions

## 0.6.1 — 29 septembre 2026

**Autres disques de l'ancien ordinateur** (disque de jeux, dossier personnel
placé sur un second disque, disque de données, disque de sauvegarde) :

- **disque déplacé dans le nouveau PC** : Bernard le reconnaît (identifiant
  du système de fichiers) et propose de le **rattacher tel quel**, sans rien
  copier : il est monté à chaque démarrage (dans `/mnt/<nom>` s'il était
  monté automatiquement dans `/media`). `/etc/fstab` est sauvegardé avant, et
  « Annuler la migration » le remet. Disques FAT, exFAT et NTFS : attribués
  au premier compte, pour qu'il puisse y écrire ;
- **disque resté dans l'ancien PC** : son contenu est copié sur le disque de
  données le plus libre du nouveau PC (jamais sur une clé USB), ou à défaut
  dans `~/Disques/<nom>` du premier compte ;
- **bibliothèques Steam** : après copie ou rattachement ailleurs, Steam est
  mis à jour avec leur nouvel emplacement ;
- les disques de sauvegarde ne sont jamais cochés d'office ;
- la place d'une copie vers un autre disque n'est plus comptée sur celle du
  disque système.

## 0.6.0 — 29 septembre 2026

**Bases de données, Docker, machines virtuelles, serveurs d'application.**
Détectés depuis la 0.4, ils sont maintenant copiés, à l'identique et de façon
cohérente :

- **service arrêté pendant sa copie, des deux côtés**, puis relancé tel
  qu'il était : MySQL/MariaDB, PostgreSQL, MongoDB, Redis, Docker (et son
  socket), Podman, serveur FileMaker. On ne copie jamais une base en train
  d'écrire. Sur l'ancien ordinateur, les données ne sont pas touchées : le
  service est seulement mis en pause (ancien ordinateur en direct comme
  paquet sur disque externe) ;
- **machines virtuelles** (libvirt) : leurs disques sont copiés tels quels,
  fichiers creux compris, et leurs définitions (dans `/etc/libvirt`)
  suivent ; libvirt est relancé pour les reprendre. Une machine allumée
  empêche la copie de ses disques : Bernard le dit (« éteignez-les ») et
  continue le reste ;
- **bases de données** : copiées directement quand les deux ordinateurs ont
  la même base (même distribution, même nom de code), donc la même version
  du serveur ; sinon elles restent signalées (export et import prévus plus
  tard).

**Attributs étendus conservés** hors des dossiers personnels : droits
détaillés (ACL), capacités des programmes (`setcap`), attributs des couches
Docker. Les fichiers « effacés » des couches overlay (Docker) sont recréés.
Sans cela, une image Docker copiée serait incohérente.

## 0.5.1 — 29 septembre 2026

**Dépôts de logiciels ajoutés à la main** (Brave, Chrome, VS Code, Docker,
PPA…). Jusqu'ici, les logiciels installés depuis ces dépôts étaient classés
« introuvables » sur le nouvel ordinateur.

- L'ancien ordinateur envoie ses dépôts (`/etc/apt/sources.list.d`) avec
  leurs clés de signature, et, pour chaque logiciel, le dépôt d'où vient la
  version installée.
- Le nouvel ordinateur ajoute seulement les dépôts qu'il ne connaît pas
  (les dépôts officiels de Zorin, Ubuntu ou Mint déjà présents ne sont pas
  touchés), puis installe les logiciels qui en viennent. L'écran de choix
  les montre (« Dépôt de logiciels … ») ; chaque logiciel concerné porte le
  nom de son dépôt.
- Le nom de code de la version est adapté (`jammy` → `noble`) pour les PPA.
- Un dépôt qui ne répond pas depuis le nouvel ordinateur (version du système
  non prise en charge, dépôt fermé) est retiré aussitôt, avec sa clé, et
  signalé au bilan : aucune source cassée n'est laissée derrière.
- Aucune clé ni aucun fichier existant n'est remplacé ; « Annuler la
  migration » retire ce que Bernard a ajouté.

## 0.5.0 — 29 septembre 2026

**Copie des données hors des dossiers personnels.** Les éléments trouvés par
l'examen du disque (0.4) sont maintenant copiés, cochés par défaut quand
Bernard le conseille :

- réglages système modifiés ou ajoutés (`/etc`), logiciels installés à la
  main (`/opt`, `/usr/local`), données de service (`/srv`), sites web
  (`/var/www`), dossiers ajoutés à la racine (`/data`…), dossiers de `/home`
  sans compte, dossier de l'administrateur et données d'autres services (à
  cocher : Bernard ne sait pas s'ils sont utiles) ;
- **au même emplacement, avec leurs propriétaires d'origine** (retrouvés par
  leur nom : `www-data`, `mysql`… ; root si le compte n'existe pas ici), leurs
  droits, y compris setuid et setgid, et leurs dates ;
- seuls les fichiers qui n'appartiennent à aucun paquet, ou les fichiers de
  configuration modifiés, sont copiés : le reste est réinstallé ;
- un fichier déjà présent sur le nouvel ordinateur est **mis de côté**
  (`/var/lib/bernard/<migration>/avant-migration/`), et « Annuler la
  migration » le remet en place ;
- la cible refuse d'écrire ailleurs que dans ces emplacements (jamais dans
  `/usr`, `/bin`, `/boot`…), et refuse aussi les fichiers de `/etc` propres à
  la machine, même si l'ancien ordinateur les envoyait ;
- services ajoutés ou modifiés dans `/etc/systemd` pris en compte
  (`systemctl daemon-reload`).

**Fichiers creux conservés**, pour toute la migration (dossiers personnels
compris) : un disque virtuel de 100 Go qui n'en occupe que 20 garde ses
20 Go sur le nouvel ordinateur. Le contenu est vérifié comme avant.

**Cible plus petite** : si tout ne tient pas mais que les dossiers
personnels tiennent, les données hors dossiers personnels sont décochées,
des plus grosses aux plus petites, avec un avertissement ; vous ajustez sur
l'écran de choix. L'espace nécessaire les compte désormais.

Encore détectés seulement (copie dans la 0.6, avec arrêt du service) : bases
de données, conteneurs, machines virtuelles, serveur FileMaker ; et les
autres disques (choix de l'emplacement sur le nouvel ordinateur).

`sudo bernard undo` utilise désormais exactement la même annulation que
l'interface.

## 0.4.0 — 29 septembre 2026

**Examen de tout le disque de l'ancien ordinateur.** Bernard repère désormais
ce qui n'appartient ni au système ni aux dossiers personnels, et le montre sur
l'écran de choix, avec la taille de chaque élément et un conseil :

- réglages système modifiés ou ajoutés (`/etc`, liste des fichiers) ;
- logiciels installés à la main (`/opt`, `/usr/local`), données de service
  (`/srv`) ;
- sites web (`/var/www`), bases MySQL/MariaDB, PostgreSQL, MongoDB, Redis,
  Docker, Podman, machines virtuelles libvirt, serveur FileMaker, autres
  services ;
- dossiers ajoutés à la racine (`/data`…), dossiers de `/home` sans compte,
  dossier de l'administrateur ;
- autres disques : sauvegarde (copie déconseillée), bibliothèque de jeux,
  dossier personnel déplacé, autre disque ; bibliothèques Steam sur un
  second disque ;
- place réellement occupée à côté de la taille apparente (fichiers creux des
  disques virtuels).

Ce qui appartient à un paquet et n'a pas été modifié est réinstallé, jamais
copié. Ce qui tient à la machine (identité, disques, amorçage, pilotes) est
écarté. **Ces éléments ne sont pas encore copiés** : ils figurent au bilan,
sous « Resté sur l'ancien ordinateur ». La copie arrive avec la 0.5.
Installez la 0.4 sur les deux ordinateurs.

Spécification : `docs/DONNEES-HORS-DOSSIERS.md`.

## 0.3.3 — 29 septembre 2026

- **Clavier** : sur un compte créé par Bernard, la disposition de l'ancien
  ordinateur revenait quand même (clavier français sur un PC à clavier
  belge). Cause : la base des réglages du bureau (`~/.config/dconf/user`)
  est copiée avec le dossier personnel, et elle contient la disposition de
  l'ancien clavier. Bernard vide désormais ces réglages puis impose
  explicitement la disposition du nouvel ordinateur (les options, comme la
  touche compose, sont gardées). La case « Reprendre la disposition de
  l'ancien ordinateur » des options avancées reste disponible.
- Même traitement pour les profils de couleur des écrans, propres au
  matériel.

## 0.3.2 — 27 septembre 2026

- **Câble réseau direct entre les deux ordinateurs** (sans box) : Linux
  n'y trouvait pas d'adresse (« l'activation de la connexion réseau a
  échoué ») et le transfert restait en Wi-Fi. Bernard repère désormais un
  câble branché resté sans adresse pendant 12 secondes et y active une
  connexion provisoire en « lien local » : chaque ordinateur prend seul une
  adresse en 169.254.x.x, et le transfert bascule sur le câble. Même chose
  pour un câble Thunderbolt. La connexion provisoire (`bernard-cable-…`) est
  retirée à la fin, y compris après un arrêt brutal (au lancement suivant).
  Un câble vers une box, qui reçoit son adresse normalement, n'est pas
  touché.

## 0.3.1 — 27 septembre 2026

- Fenêtre vide au lancement sur certains ordinateurs (cartes graphiques
  anciennes, par exemple Intel HD 3000) : le rendu accéléré de WebKitGTK est
  désormais coupé pour la fenêtre de Bernard, qui n'en a pas besoin.
- Si la fenêtre dédiée n'a pas chargé l'assistant au bout de 12 secondes,
  Bernard la ferme et ouvre l'assistant dans le navigateur, sans perdre la
  session, et s'en souvient : les fois suivantes, il ouvre directement le
  navigateur sur cet ordinateur (fichier `~/.config/bernard/navigateur`, à
  supprimer pour réessayer la fenêtre). Cas rencontré : un HP ProBook sous
  Zorin OS 18.1 dont le moteur d'affichage web du système ne fonctionne pas
  (l'aide de Zorin, Yelp, y est vide aussi). Les messages d'erreur de la
  fenêtre s'affichent désormais dans le terminal.
- `bernard gui --browser` ouvre directement l'assistant dans le navigateur.
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
