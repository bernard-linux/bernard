# Dépôt APT de Bernard

Avec le dépôt APT, Bernard se met à jour comme le reste du système (gestionnaire
de mises à jour). Il est servi par GitHub Pages depuis la branche `gh-pages`,
mise à jour à chaque version publiée (`git tag vX.Y.Z && git push origin vX.Y.Z`).

Installer le paquet `.deb` une seule fois suffit : il ajoute le dépôt tout seul
(`/etc/apt/sources.list.d/bernard.sources`, retiré avec le paquet).

## Mise en place (une seule fois)

### 1. Créer la clé de signature du dépôt

Sur un ordinateur Linux (gpg y est déjà), dans un terminal :

```sh
export GNUPGHOME=$(mktemp -d)
gpg --batch --passphrase '' --quick-gen-key "Bernard (dépôt APT) <bernard-linux@users.noreply.github.com>" ed25519 sign never
gpg --armor --export-secret-keys > ~/bernard-cle-privee.asc
gpg --export > ~/bernard-archive-keyring.gpg
rm -rf "$GNUPGHOME"
```

Sur un Mac, installer d'abord gpg (`brew install gnupg`), puis mêmes commandes.

**Gardez `bernard-cle-privee.asc` en lieu sûr** (gestionnaire de mots de passe,
clé USB rangée) : qui la détient peut publier des mises à jour de Bernard.
Ne la mettez jamais dans le dépôt.

### 2. Donner la clé privée à GitHub

Dépôt `bernard-linux/bernard` → **Settings → Secrets and variables → Actions →
New repository secret** :

- Name : `APT_GPG_KEY`
- Secret : tout le contenu de `bernard-cle-privee.asc` (de `-----BEGIN` à `-----END…-----`).

### 3. Mettre la clé publique dans les sources

```sh
cp ~/bernard-archive-keyring.gpg packaging/apt/bernard-archive-keyring.gpg
git add packaging/apt/bernard-archive-keyring.gpg
git commit -m "Dépôt APT : clé publique" && git push
```

Le paquet `.deb` l'embarque alors, avec la déclaration du dépôt.

### 4. Activer GitHub Pages

Après la première version publiée avec la clé (la branche `gh-pages` est alors
créée) : **Settings → Pages → Build and deployment → Source : Deploy from a
branch → Branch : `gh-pages` / `(root)` → Save**.

Le dépôt est ensuite à l'adresse <https://bernard-linux.github.io/bernard/>
(page d'accueil avec les instructions).

## Vérifier

Sur un ordinateur où Bernard est installé depuis le `.deb` :

```sh
sudo apt update && apt policy bernard
```

La ligne `https://bernard-linux.github.io/bernard stable/main` doit apparaître.
