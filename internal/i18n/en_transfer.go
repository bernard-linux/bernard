package i18n

// Catalogue anglais : copie, journal, paquet, réseau, appairage.
func init() {
	add(map[string]string{
		// Généraux
		"%s : %s": "%s: %s",
		"%s : %w": "%s: %w",

		// transfer
		"pas un fichier ordinaire": "not a regular file",
		"vérification échouée : le contenu écrit diffère de la source": "check failed: the copied content differs from the original",
		"fichier source modifié pendant la copie":                      "the original file changed while it was being copied",
		"trop de conflits de noms":                                     "too many files with the same name",
		"opération non prise en charge":                                "operation not supported",
		"attribut %s de %s : %w":                                       "attribute %s of %s: %w",
		"%s est hors de %s":                                            "%s is outside %s",
		"%s n'est pas un dossier ordinaire : écriture refusée":         "%s is not a regular folder: writing refused",
		"%s existe déjà et n'est pas un dossier":                       "%s already exists and is not a folder",
		"%s existe déjà":                                               "%s already exists",
		"%s n'est pas un dossier":                                      "%s is not a folder",

		// engine, journal
		"ce journal appartient à une autre migration (inventaire différent)": "this log belongs to another migration (different inventory)",
		"journal corrompu : %w":                                 "log damaged: %w",
		"journal : l'inventaire a changé en cours de migration": "log: the inventory changed during the migration",

		// source
		"chemin refusé : hors des dossiers à migrer": "path refused: outside the folders to migrate",
		"%s : pas un fichier ordinaire":              "%s: not a regular file",
		"%s : fichier remplacé pendant l'ouverture":  "%s: file replaced while it was being opened",
		"source : %s":      "source: %s",
		"source : %s : %s": "source: %s: %s",

		// pack (disque externe)
		"phrase de passe incorrecte": "incorrect passphrase",
		"paquet incomplet : son écriture a été interrompue, il faut le recréer": "incomplete package: writing it was interrupted, it needs to be created again",
		"paquet altéré : un bloc chiffré ne correspond pas":                     "damaged package: an encrypted block does not match",
		"modifié pendant la lecture":                                            "changed while being read",
		"la phrase de passe doit faire au moins 8 caractères":                   "the passphrase must be at least 8 characters long",
		"%s n'est pas vide : choisissez un dossier vide":                        "%s is not empty: choose an empty folder",
		"pas de paquet Bernard dans %s : %w":                                    "no Bernard package in %s: %w",
		"format de paquet non pris en charge : %q":                              "unsupported package format: %q",
		"en-tête de paquet invalide":                                            "invalid package header",
		"jeu de données absent du paquet : %s":                                  "data set missing from the package: %s",
		"absent du paquet":                                                      "missing from the package",
		"aucun mot de passe dans le paquet":                                     "no passwords in the package",
		"aucun réglage dans le paquet":                                          "no settings in the package",
		"%w : fichier %s manquant":                                              "%w: file %s missing",

		// remote, wire
		"jeu de données inconnu : %q":   "unknown data set: %q",
		"demande inconnue : %s":         "unknown request: %s",
		"mots de passe non disponibles": "passwords not available",
		"mots de passe non lisibles (agent lancé sans droits administrateur ?)": "passwords could not be read (was Bernard started without administrator rights?)",
		"réglages non disponibles (agent lancé sans droits administrateur ?)":   "settings not available (was Bernard started without administrator rights?)",
		"fichier modifié pendant l'envoi":                                       "file changed while it was being sent",
		"protocole : message inattendu %q":                                      "protocol: unexpected message %q",
		"protocole : reprise à %d demandée, %d accordée":                        "protocol: resume at %d requested, %d granted",
		"protocole : fin de fichier invalide":                                   "protocol: invalid end of file",
		"trame trop grande : %d octets":                                         "frame too large: %d bytes",
		"trame trop grande annoncée : %d octets":                                "announced frame too large: %d bytes",
		"protocole : message attendu, trame %q reçue":                           "protocol: message expected, frame %q received",

		// link, directlink
		"Liaison perdue. Recherche d'une autre liaison (câble, Wi-Fi)…":                            "Connection lost. Looking for another connection (cable, Wi-Fi)…",
		"liaison non rétablie : relancez la commande pour reprendre":                               "connection not restored: run the command again to resume",
		"Reconnecté via %s. Le transfert reprend.":                                                 "Reconnected via %s. The transfer is resuming.",
		"Liaison plus rapide détectée (%s) : bascule en cours…":                                    "Faster connection found (%s): switching over…",
		"Liaison perdue. En attente de l'ancien ordinateur ; le transfert reprendra seul…":         "Connection lost. Waiting for the old computer; the transfer will resume by itself…",
		"Reconnecté. Reprise du transfert.":                                                        "Reconnected. Resuming the transfer.",
		"l'ancien ordinateur ne s'est pas reconnecté : relancez les deux commandes pour reprendre": "the old computer did not reconnect: run the command again on both computers to resume",
		"Câble direct sur %s : adresse automatique impossible (%s)":                                "Direct cable on %s: could not set an address automatically (%s)",
		"Câble direct détecté sur %s : adresse automatique en lien local (169.254.x.x)":            "Direct cable found on %s: automatic link-local address (169.254.x.x)",

		// pake, session
		"code d'appairage incorrect": "incorrect pairing code",
		"rôle inconnu":               "unknown role",
		"point invalide reçu":        "invalid point received",
		"code d'appairage expiré : un nouveau code est nécessaire":        "pairing code expired: a new code is needed",
		"trop d'essais incorrects : un nouveau code est nécessaire":       "too many incorrect attempts: a new code is needed",
		"aucune session à reprendre : un nouvel appairage est nécessaire": "no session to resume: the computers need to be paired again",
		"version de protocole incompatible (%d)":                          "incompatible protocol version (%d)",

		// agent
		"câble Thunderbolt / USB4": "Thunderbolt / USB4 cable",
		"câble réseau (RJ45)":      "network cable (RJ45)",
		"Wi-Fi":                    "Wi-Fi",
		"réseau":                   "network",

		// sysexec
		"commande absente": "command not found",
	})
}
