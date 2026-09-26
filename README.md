# Bernard

**L'assistant de migration vers Linux.** Comme le bernard-l'hermite qui change
de coquille, Bernard vous installe dans votre nouvel ordinateur Linux avec vos
comptes, documents, réglages et applications, depuis un ancien ordinateur
Linux, Windows ou Mac.

> Projet en cours de développement (jalon V1.0 : Linux → Linux, étapes 1 à 8 réalisées, essais sur matériel réel en cours).
> Ne l'utilisez pas encore sur des données sans sauvegarde.

## Principes

- **Aucune perte de données.** L'ancienne machine n'est jamais modifiée.
  Chaque fichier est vérifié par empreinte BLAKE3 après écriture. Rien n'est
  écrasé sur la nouvelle machine.
- **Reconstruire, pas cloner.** Les applications sont réinstallées proprement
  depuis les dépôts ou Flathub ; seuls les données et réglages sont copiés.
  La migration fonctionne vers un disque plus petit.
- **Transparence.** Un plan est affiché avant toute action, un rapport chiffré
  à la fin. Tout ce qui n'a pas pu être migré y est nommé.

## Distributions cibles (V1)

Ubuntu, Zorin OS et Debian avec GNOME ; Linux Mint avec Cinnamon.

## Utiliser l'assistant graphique

Sur le **nouvel** ordinateur : menu des applications → Bernard (ou `bernard gui`).
Le mot de passe administrateur est demandé, puis l'assistant s'ouvre dans sa
propre fenêtre. Sur l'**ancien** ordinateur : `sudo bernard-agent connect`,
puis le code affiché.

Installation à partir des paquets fournis dans `dist/` :

```sh
sudo apt install ./dist/bernard_*.deb            # moteur et agent (toutes versions)
sudo apt install ./dist/bernard-window_*.deb     # fenêtre dédiée (Ubuntu 24.04 et dérivées)
```

Le paquet `bernard-window` fourni est construit pour Ubuntu 24.04 (Zorin 18,
Mint 22). Pour Zorin 17 ou Mint 21, construisez-le sur place
(`packaging/deb/build.sh 1.0.0`) ou passez-vous-en : l'assistant s'ouvre
alors dans le navigateur.

Pour la fenêtre dédiée, compilez-la sur la machine cible (sinon l'assistant
s'ouvre dans le navigateur) :

```sh
sudo apt install gcc pkg-config libwebkit2gtk-4.1-dev
make window && sudo install -m755 bin/bernard-window /usr/bin/
```

Guide des essais entre deux ordinateurs : [docs/ESSAIS.md](docs/ESSAIS.md).

## Essayer (développeurs)

Prérequis : Go 1.22 ou plus récent. `make build` produit `bin/bernard` et
`bin/bernard-agent` ; `make window` produit `bin/bernard-window` ;
`sudo make install` installe le tout, avec l'entrée de menu et la politique
polkit.

**Migration réelle par le réseau** (Wi-Fi, câble RJ45, Thunderbolt/USB4) :

```sh
# Nouvel ordinateur : attend l'ancien, affiche le code, montre le plan,
# crée les comptes, installe les applications, copie dans /home
sudo ./bin/bernard receive --system

# Ancien ordinateur : trouve le nouveau et demande le code
sudo ./bin/bernard-agent connect
```

Sans `sudo`, l'agent ne lit que les fichiers de l'utilisateur qui le lance et
les mots de passe devront être saisis à nouveau.

On peut changer de liaison en cours de route (débrancher le câble, passer en
Wi-Fi) : la session se rétablit seule, sans nouveau code, et la copie reprend
au dernier point vérifié.

**Mode essai** (données seulement, sans toucher au système) :

```sh
./bin/bernard receive --dest ~/essai-migration
./bin/bernard-agent connect
```

**Par disque externe** :

```sh
# Ancien ordinateur : paquet chiffré sur le disque
./bin/bernard-agent pack --dest /media/disque/migration

# Nouvel ordinateur
./bin/bernard unpack --pack /media/disque/migration --dest ~/essai-migration
```

Une migration interrompue plus longtemps (ordinateur éteint) reprend en
relançant les deux commandes. `sudo bernard undo --journal <journal>` supprime
uniquement ce que Bernard a créé et qui n'a pas été modifié depuis, puis
retire les applications et les comptes qu'il a ajoutés. Le chemin du journal
est affiché en fin de migration (`/var/lib/bernard/…`).

Pare-feu : la cible utilise le port UDP 51515 (découverte) et TCP 51516
(appairage et transfert).

## Structure du dépôt

| Dossier | Contenu |
| --- | --- |
| `cmd/bernard` | Moteur, sur la machine cible |
| `cmd/bernard-agent` | Agent, sur la machine source |
| `internal/inventory` | Format de l'inventaire (`migration-inventory/1`) |
| `internal/plan` | Règles de migration et format du plan (`migration-plan/1`) |
| `internal/collect/linux` | Inventaire d'une source Linux (lecture seule) |
| `internal/transfer` | Copie vérifiée, sans écrasement |
| `internal/pkgmgr` | Adaptateurs apt, Flatpak, Snap |
| `internal/discovery` | Balises UDP, choix de la liaison la plus rapide |
| `internal/pake` | Appairage SPAKE2 par code à 6 chiffres |
| `internal/session` | TLS 1.3 éphémère lié à l'appairage |
| `internal/wire`, `internal/remote` | Protocole agent ↔ moteur |
| `internal/source` | Parcours sûr des fichiers de la source |
| `internal/engine` | Réception journalisée, reprise, annulation |
| `internal/journal` | Journal en ajout seul, synchronisé sur disque |
| `internal/pack` | Paquet chiffré pour disque externe |
| `internal/link` | Reconnexion automatique sur une autre liaison |
| `internal/system` | Seules opérations système : comptes, apt, Flatpak |
| `internal/apply` | Exécution du plan et annulation système |
| `internal/migrate` | Déroulé commun à la ligne de commande et à l'interface |
| `internal/ui` | Assistant : API locale protégée et interface web embarquée |
| `cmd/bernard-window` | Fenêtre dédiée (WebKitGTK), module séparé |
| `internal/settings` | Réglages : bureau (GNOME ↔ Cinnamon), Wi-Fi, tâches planifiées, imprimantes |
| `packaging/` | Polkit, entrée de menu, icône, paquets Debian |
| `tests/e2e` | Migration réelle complète en administrateur, puis annulation |
| `tests/vm` | Banc d'essai en machines virtuelles |
| `internal/sysexec` | Exécution de commandes, remplaçable en test |
| `mappings/` | Base de correspondances (à venir) |
| `docs/` | Feuille de route et documentation |

## Licence

GPL-3.0-or-later. Voir [LICENSE](LICENSE).
