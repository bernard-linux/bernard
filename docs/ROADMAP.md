# Feuille de route

## Jalon V1.0 — Linux → Linux

| Étape | Contenu | État |
| --- | --- | --- |
| 1 | Structure du dépôt, format d'inventaire, collecteur Linux, copie vérifiée sans écrasement, tests anti-perte | Fait |
| 2 | Plan de migration : règles apt / Flatpak / Snap, comptes, espace disque ; adaptateurs de consultation | Fait (première version) |
| 3 | Transport : découverte par balise UDP (Wi-Fi, RJ45, Thunderbolt), appairage SPAKE2, TLS 1.3 lié, journal, reprise à l'octet, annulation, paquet chiffré pour disque externe | Fait |
| 4 | Exécution du plan en administrateur : comptes (UID, groupes utiles, mot de passe repris), apt et Flatpak, copie dans /home avec les bons propriétaires, annulation système ; bascule automatique de liaison en cours de transfert | Fait |
| 5 | Assistant graphique : fenêtre dédiée WebKitGTK, lancement via polkit, API locale protégée par jeton, écrans source, connexion, analyse, choix, migration, bilan, annulation | Fait |
| 6 | Journal de migration, reprise après coupure, annulation des changements système, rapport final (PDF) | À faire |
| 7 | Réglages : fichiers de configuration du dossier personnel (dont clés SSH/GPG), modèles du dossier neuf remplacés, préférences du bureau avec traduction GNOME ↔ Cinnamon et sauvegarde pour l'annulation, Wi-Fi avec mot de passe, tâches planifiées, imprimantes réseau sans pilote | Fait |
| 8 | Paquets `.deb`, test de bout en bout réel, matrice d'intégration continue (Ubuntu 22.04/24.04, Debian 12, Mint 21/22), test Flathub réel, banc d'essai en machines virtuelles | Fait, sauf instantanés btrfs/LVM de la source (reportés) |

| 9 | Données hors des dossiers personnels : détection (0.4), copie (0.5), serveurs et bases cohérents (0.6) — voir `DONNEES-HORS-DOSSIERS.md` | Fait (0.4 à 0.6.1) |
| 10 | Finitions : extensions GNOME et tableau de bord de Zorin, bilan lisible et PDF, date de dernière utilisation, VPN, liens durs (0.7) ; anglais | Fait (0.7 ; anglais en 0.8) |
| 11 | Validation V1.0 : banc en machines virtuelles (automatique, 0.9), essais VirtualBox avec le vrai bureau, 5 testeurs, dépôt APT du projet (0.9) | En cours |

### Points déjà identifiés

- `apt-mark showmanual` remonte de nombreux paquets du système de base. Le plan
  les classe « déjà présents », mais l'interface devra masquer ces lignes par
  défaut pour ne montrer que les vraies applications.
- Date de dernière utilisation (0.7) : date d'accès au programme, fiable
  seulement si le disque source enregistre ces dates (« relatime », par
  défaut) ; seuil 12 mois, date inconnue = application cochée.
- La table Snap → Flatpak est provisoire (`internal/plan/snapmap.go`).
- Étape 3, limites connues à lever :
  - la phrase de passe du paquet s'affiche pendant la saisie en ligne de
    commande (l'interface de l'étape 5 la masquera) ;
  - l'écriture d'un paquet interrompue doit être recommencée depuis le début
    (la lecture, elle, reprend) ;
  - un seul fichier transféré à la fois ; le parallélisme des petits fichiers
    viendra après la mesure des débits réels ;
  - la liste d'un jeu de données est gardée en mémoire (environ 150 Mo pour
    un million de fichiers) ;
  - les fichiers reçus appartiennent à l'utilisateur qui lance `bernard`
    jusqu'à l'étape 4 ;
  - la détection du type de liaison n'existe que sous Linux (agents Windows
    et macOS : V1.1 et V1.2).
- Étape 4, limites connues :
  - en ligne de commande, les nouveaux mots de passe s'affichent pendant la
    saisie (masqués dans l'interface de l'étape 5) ;
  - après annulation, le dossier personnel d'un compte supprimé garde les
    fichiers modèles (.bashrc…) créés par le système ;
  - non encore conservés : liens durs (copiés comme fichiers séparés),
    attributs étendus et ACL, fichiers creux (copiés pleins) ;
  - installation Flatpak testée avec un faux système uniquement (Flathub
    injoignable depuis l'environnement de test) : à valider sur un vrai poste.
- Étape 5, limites connues :
  - l'agent de l'ancien ordinateur reste en ligne de commande (une petite
    fenêtre viendra avec les agents Windows et macOS) ;
  - le rapport détaillé est un fichier JSON ; une version lisible (HTML/PDF)
    reste à faire ;
  - la date de dernière utilisation des applications n'est pas encore
    collectée : toutes les applications sont donc cochées par défaut ;
  - interface en français uniquement pour l'instant.
- Étapes 7 et 8, limites connues :
  - réglages du bureau et imprimantes non repris quand la source est un
    disque monté ailleurs (`--root`) : ils sont lus dans la session de
    chaque compte ;
  - bureaux autres que GNOME et Cinnamon : seuls fichiers, Wi-Fi, tâches
    planifiées et imprimantes suivent ;
  - connexions VPN non reprises (elles pointent souvent vers des
    certificats à réinstaller) ;
  - imprimantes USB ou à pilote propriétaire : signalées, à réinstaller ;
  - la source est lue sans instantané : un fichier modifié pendant la copie
    est relu (3 essais) puis signalé ; les instantanés btrfs/LVM restent à
    faire ;
  - les images Docker de Mint utilisées par l'intégration continue sont à
    confirmer au premier passage ;
  - banc d'essai en machines virtuelles documenté mais pas encore déroulé.

### Interface texte (plus tard)

Pour les machines sans écran graphique, une interface texte (TUI) sera un
second client de l'API de `internal/ui` : aucune logique de migration à
réécrire. D'ici là, `sudo bernard receive --system` fonctionne sans écran.

## Jalon V1.1 — Windows → Linux

Agent Windows (VSS, registre, winget), base de correspondances YAML d'une
cinquantaine d'applications, profils Wi-Fi, profils de navigateurs.

## Jalon V1.2 — macOS → Linux

Agent macOS (snapshot APFS, Homebrew, /Applications), correspondances.

## Au-delà

V2 : Fedora, Arch, openSUSE, KDE, traduction des réglages entre bureaux,
mode live CD. V3 : modèles réutilisables et déploiement multi-postes.
