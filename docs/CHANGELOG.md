# Historique des versions

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
