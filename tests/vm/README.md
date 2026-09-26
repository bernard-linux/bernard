# Banc d'essai en machines virtuelles

L'intégration continue rejoue déjà une migration réelle complète dans des
conteneurs Ubuntu 22.04 et 24.04, Debian 12, Mint 21 et 22
(`.github/workflows/ci.yml`, tâche `e2e`). Ce que les conteneurs ne peuvent
pas montrer, et que ce banc vérifie :

- deux machines distinctes sur un vrai réseau (découverte par balises) ;
- polkit, la fenêtre dédiée et une vraie session graphique ;
- NetworkManager, CUPS et dconf dans une session réelle ;
- la reconnexion en coupant l'interface réseau d'une machine virtuelle.

## Mise en place (GNOME Boxes ou virt-manager)

1. Créer deux machines à partir des ISO officielles (Zorin, Ubuntu, Mint,
   Debian GNOME), 4 Go de mémoire et 30 Go de disque chacune.
2. Les placer sur le même réseau virtuel (réseau « default » de libvirt).
3. Sur la **source** : créer un ou deux comptes, y déposer des documents,
   changer le fond d'écran, la disposition du clavier et le dock, enregistrer
   un réseau Wi-Fi fictif (`nmcli connection add type wifi ssid Test …`),
   ajouter une tâche planifiée et une imprimante IPP fictive
   (`lpadmin -p Test -E -v ipp://192.0.2.1/ipp/print -m everywhere`).
4. Faire un instantané des deux machines.
5. Installer les paquets `bernard` (et `bernard-window` sur la cible) :
   `sudo apt install ./bernard_*.deb`.

## Scénarios

| # | Source → cible | À vérifier |
| --- | --- | --- |
| 1 | Zorin 17 → Zorin 17 | parcours complet dans la fenêtre, reconnexion avec le compte migré |
| 2 | Ubuntu 24.04 → Mint 22 | traduction GNOME → Cinnamon : fond, clavier, favoris |
| 3 | Mint 22 → Ubuntu 24.04 | traduction inverse ; Snap non utilisé côté Mint |
| 4 | Debian 12 → Zorin 17 | paquets renommés ou absents signalés dans le plan |
| 5 | n'importe laquelle | couper la carte réseau de la cible pendant la copie (`virsh domif-setlink … down` puis `up`) : reprise sans nouveau code |
| 6 | n'importe laquelle | paquet sur disque virtuel FAT32 avec un fichier de plus de 4 Go |
| 7 | après 1 à 6 | « Annuler la migration » puis comparaison avec l'instantané |

Revenir à l'instantané entre deux scénarios. Noter pour chacun : versions,
débit affiché, messages obscurs, contenu de `/var/lib/bernard/*/rapport.json`.
