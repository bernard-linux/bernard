package i18n

// Réglages, système, services, inventaire et collecte.
func init() {
	add(map[string]string{
		"non appliqué": "not applied",
		// settings
		"identifiant refusé : %q":                  "username refused: %q",
		"lecture des réglages actuels de %s : %w":  "could not read the current settings of %s: %w",
		"%w : pas une connexion Wi-Fi ni VPN":      "%w : not a Wi-Fi or VPN connection",
		"connexion réseau incomplète":              "incomplete network connection",
		"%w : « %s » existe déjà":                  "%w : “%s” already exists",
		"fichier refusé : %s":                      "file refused: %s",
		"%w : %s a déjà des tâches planifiées ici": "%w : %s already has scheduled tasks on this computer",
		"nom d'imprimante refusé : %q":             "printer name refused: %q",
		"%w : %s n'est pas une imprimante réseau, à installer depuis les réglages d'impression": "%w : %s is not a network printer; add it from the printer settings",
		"%w : %s existe déjà": "%w : %s already exists",

		// system
		"paramètre refusé":                   "value refused",
		"%w : identifiant %q":                "%w : username %q",
		"le compte %s existe déjà":           "the account %s already exists",
		"%w : aucun mot de passe pour %s":    "%w : no password for %s",
		"%w : hachage de mot de passe":       "%w : password hash",
		"%w : mot de passe":                  "%w : password",
		"mot de passe de %s non défini : %w": "could not set the password for %s: %w",
		"non installé":                       "not installed",
		"%w : identifiant Flatpak %q":        "%w : Flatpak ID %q",
		"installation de Flatpak : %w":       "installing Flatpak: %w",
		"aucun autre compte administrateur : ce compte ne peut pas être supprimé": "there is no other administrator account, so this account can’t be deleted",
		"%s n'est pas un compte utilisateur de cet ordinateur":                    "%s is not a user account on this computer",
		"une session de %s est ouverte : suppression abandonnée":                  "%s is still logged in, so the account was not deleted",

		// services
		"machines virtuelles allumées (%s) : éteignez-les, puis relancez la migration pour les copier": "virtual machines are running (%s): shut them down, then run the migration again to copy them",
		"arrêt du service %s impossible : %w": "could not stop the service %s: %w",

		// inventory
		"format d'inventaire non pris en charge : %q (attendu %q)": "unsupported inventory format: %q (expected %q)",
		"utilisateur incomplet : %+v":                              "incomplete user: %+v",
		"jeu de données %s : destination refusée %q":               "data set %s: destination refused %q",
		"jeu de données %s rattaché à un utilisateur inconnu %q":   "data set %s belongs to an unknown user %q",
		"inventaire illisible : %w":                                "unreadable inventory: %w",

		// collect : avertissements
		"lecture des comptes impossible : %w":                                                  "could not read the user accounts: %w",
		"inventaire %s incomplet : %v":                                                         "%s list incomplete: %v",
		"réseaux Wi-Fi non inventoriés : %v":                                                   "Wi-Fi networks not listed: %v",
		"imprimantes non inventoriées : %v":                                                    "printers not listed: %v",
		"dossier personnel de %s introuvable (%s)":                                             "home folder of %s not found (%s)",
		"mesure de %s incomplète : %v":                                                         "could not fully measure %s: %v",
		"%d éléments illisibles dans %s (droits insuffisants ?)":                               "%d items could not be read in %s (missing permissions?)",
		"préférences du bureau et imprimantes non lues : système monté depuis un autre disque": "desktop settings and printers not read: the system was opened from another disk",
		"préférences du bureau de %s non lues":                                                 "desktop settings of %s not read",

		// collect : données hors des dossiers personnels
		"Bases MySQL / MariaDB":                       "MySQL / MariaDB databases",
		"Bases PostgreSQL":                            "PostgreSQL databases",
		"Bases MongoDB":                               "MongoDB databases",
		"Données Redis":                               "Redis data",
		"Docker : images, conteneurs et volumes":      "Docker: images, containers and volumes",
		"Podman : images et conteneurs":               "Podman: images and containers",
		"LXD : conteneurs":                            "LXD: containers",
		"Machines virtuelles (libvirt)":               "Virtual machines (libvirt)",
		"Serveur FileMaker : bases et réglages":       "FileMaker Server: databases and settings",
		"Sites web (/var/www)":                        "Websites (/var/www)",
		"Réglages système modifiés ou ajoutés (/etc)": "Changed or added system settings (/etc)",
		"Données du service « %s »":                   "Data of the “%s” service",
		"Sauvegardes Timeshift (/timeshift)":          "Timeshift backups (/timeshift)",
		"Dossier /%s":                                 "Folder /%s",
		"Disque de sauvegarde":                        "Backup disk",
		"Bibliothèque de jeux":                        "Games library",
		"Dossier personnel sur un autre disque":       "Home folder on another disk",
		"Autre disque":                                "Other disk",
		"Bibliothèque Steam (%s)":                     "Steam library (%s)",
		"Dossier /%s (sans compte)":                   "Folder /%s (no account)",
		"Logiciel installé à la main : /opt/%s":       "Manually installed software: /opt/%s",
		"Données de service : /srv/%s":                "Service data: /srv/%s",
		"Programmes et fichiers ajoutés (/usr/local)": "Added programs and files (/usr/local)",
		"Dossier de l'administrateur (/root)":         "Administrator’s folder (/root)",
	})
}
