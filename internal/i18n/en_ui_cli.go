package i18n

// Catalogue anglais : moteur de l'interface (internal/ui) et lignes de
// commande (cmd/bernard, cmd/bernard-agent).
func init() {
	add(map[string]string{
		// internal/ui : contrôleur
		"une migration est déjà en cours":                                          "a migration is already in progress",
		"Bernard : migration en cours":                                             "Bernard: migration in progress",
		"le port %d est occupé : une autre instance de Bernard est-elle ouverte ?": "port %d is already in use: is Bernard already open in another window?",
		"Connexion refusée : %s":                                                   "Connection refused: %s",
		"possible seulement à la fin de la migration":                              "only possible once the migration has finished",
		"ce compte fait partie de la migration : il ne peut pas être supprimé ici": "this account is part of the migration: it can't be removed here",
		"aucun compte migré n'est administrateur : gardez ce compte, sinon plus personne ne pourrait gérer l'ordinateur": "none of the transferred accounts is an administrator: keep this account, otherwise nobody could manage the computer",
		"aucun plan en attente de validation":                             "no plan is waiting for confirmation",
		"la sélection ne tient pas sur ce disque : décochez des dossiers": "your selection doesn't fit on this disk: untick some folders",
		"choisissez un mot de passe pour le compte %s":                    "choose a password for the account %s",
		"choix déjà transmis":                                             "your choices have already been sent",
		"aucune migration à annuler":                                      "there is no migration to undo",
		"redémarrage possible seulement à la fin de la migration":         "you can only restart once the migration has finished",

		// internal/ui : ancien ordinateur
		"rôle inconnu : %q": "unknown role: %q",
		"écoute du réseau impossible : Bernard est-il déjà ouvert sur cet ordinateur (ou bernard-agent dans un terminal) ?": "can't listen on the network: is Bernard already open on this computer (or bernard-agent in a terminal)?",
		"le code comporte 6 chiffres":                                                   "the code has 6 digits",
		"aucun nouvel ordinateur en attente":                                            "no new computer is waiting",
		"ce nouvel ordinateur n'est plus visible : Bernard y est-il toujours ouvert ?":  "this new computer can no longer be seen: is Bernard still open on it?",
		"Connexion impossible : %s":                                                     "Can't connect: %s",
		"Ce code ne correspond pas. Vérifiez le code affiché sur le nouvel ordinateur.": "This code doesn't match. Check the code shown on the new computer.",
		"Le nouvel ordinateur affiche maintenant un nouveau code : saisissez-le.":       "The new computer is now showing a new code: enter it.",
		"inventaire de cet ordinateur impossible : %w":                                  "couldn't take stock of this computer: %w",
		"réseau": "network",
		"la phrase de passe doit faire au moins 8 caractères":   "the passphrase must be at least 8 characters long",
		"ce disque n'est plus branché":                          "this disk is no longer connected",
		"aucune préparation en cours":                           "nothing is being prepared",
		"disque externe":                                        "external disk",
		"pas assez de place sur %s : %s nécessaires, %s libres": "not enough space on %s: %s needed, %s free",
		"écriture du paquet impossible : %w":                    "couldn't write the package: %w",
		"%d o":                                                  "%d B",
		"%.1f %co":                                              "%.1f %cB",

		// internal/ui : serveur local
		"hôte refusé":             "host refused",
		"jeton refusé":            "token refused",
		"POST attendu":            "POST expected",
		"flux non pris en charge": "streaming not supported",
		"mode inconnu : %q":       "unknown mode: %q",

		// cmd/bernard
		`bernard — moteur de Bernard, l'assistant de migration vers Linux

Usage :
  bernard gui [--browser]
      Ouvre l'assistant graphique (demande le mot de passe administrateur).
      --browser : dans le navigateur au lieu de la fenêtre dédiée.
  sudo bernard receive --system [--yes] [--port 51516]
      Migration réelle : attend l'ancien ordinateur, affiche le code,
      montre le plan, crée les comptes, installe les applications et copie
      les données dans /home. Survit aux changements de liaison ; relancer
      la commande reprend une migration interrompue.
  bernard receive --dest DOSSIER [--port 51516]
      Mode essai : copie seulement les données dans DOSSIER/<compte>.
  bernard unpack --pack DOSSIER_DU_PAQUET --dest DOSSIER
      Lit un paquet écrit sur disque externe par bernard-agent pack.
  [sudo] bernard undo --journal FICHIER
      Supprime uniquement ce que Bernard a créé et qui n'a pas été modifié ;
      en administrateur, retire aussi les comptes et applications ajoutés.
  bernard plan -i inventaire.json [-o plan.json]
      Calcule ce qui serait fait sur CETTE machine. Ne modifie rien.
  bernard copy SOURCE DESTINATION
      Copie vérifiée d'un dossier local (outil de test).
  sudo bernard remove-account IDENTIFIANT
      Supprime un compte provisoire et son dossier personnel (utilisé au
      démarrage quand la suppression a été programmée en fin de migration).
  bernard version
`: `bernard — the engine of Bernard, the assistant for moving to Linux

Usage:
  bernard gui [--browser]
      Opens the assistant (asks for the administrator password).
      --browser: in the web browser instead of the Bernard window.
  sudo bernard receive --system [--yes] [--port 51516]
      Real migration: waits for the old computer, shows the code,
      shows the plan, creates the accounts, installs the apps and copies
      the data into /home. Survives changes of connection; running the
      command again resumes an interrupted migration.
  bernard receive --dest FOLDER [--port 51516]
      Trial mode: only copies the data into FOLDER/<account>.
  bernard unpack --pack PACKAGE_FOLDER --dest FOLDER
      Reads a package written to an external disk by bernard-agent pack.
  [sudo] bernard undo --journal FILE
      Removes only what Bernard created and that hasn't been changed since;
      as administrator, also removes the accounts and apps it added.
  bernard plan -i inventaire.json [-o plan.json]
      Works out what would be done on THIS computer. Changes nothing.
  bernard copy SOURCE DESTINATION
      Verified copy of a local folder (testing tool).
  sudo bernard remove-account LOGIN
      Removes a temporary account and its home folder (used at start-up
      when the removal was scheduled at the end of the migration).
  bernard version
`,
		"\r  %s : %d / %d éléments (%d %%), %.1f Mo reçus    ":              "\r  %s: %d / %d items (%d %%), %.1f MB received    ",
		"%s : %d fichiers copiés et vérifiés (%.1f Mo), %d déjà présents\n": "%s: %d files copied and checked (%.1f MB), %d already there\n",
		"  Renommé (un fichier du même nom existait) :":                     "  Renamed (a file with the same name already existed):",
		"  Non migrable (fichier spécial) :":                                "  Can't be transferred (special file):",
		"  ERREUR %s : %s\n":                                                "  ERROR %s: %s\n",
		"Migration interrompue :":                                           "Migration interrupted:",
		"Relancez la même commande pour reprendre ; rien de ce qui est déjà vérifié ne sera recopié.": "Run the same command again to resume; nothing already checked will be copied again.",
		"Journal :": "Log:",
		"Terminé avec des éléments non copiés (voir ci-dessus).":                                   "Finished, but some items weren't copied (see above).",
		"Migration terminée : tout a été copié et vérifié.":                                        "Migration finished: everything has been copied and checked.",
		"mode essai : dossier de destination":                                                      "trial mode: destination folder",
		"migration réelle (comptes, applications, /home) ; nécessite sudo":                         "real migration (accounts, apps, /home); needs sudo",
		"ne pas demander de confirmation du plan":                                                  "don't ask to confirm the plan",
		"port TCP d'appairage":                                                                     "TCP port for pairing",
		"Choisissez --system (migration réelle) ou --dest DOSSIER (essai).":                        "Choose --system (real migration) or --dest FOLDER (trial).",
		"La migration réelle crée des comptes et installe des applications : lancez-la avec sudo.": "The real migration creates accounts and installs apps: run it with sudo.",
		"Écoute impossible :":                                                                      "Can't listen:",
		"\nSur l'ancien ordinateur, lancez : sudo bernard-agent connect\nCode d'appairage : %s\n":  "\nOn the old computer, run: sudo bernard-agent connect\nPairing code: %s\n",
		"  (si la recherche échoue : --target <adresse de %s>:%d)\n":                               "  (if the search fails: --target <address of %s>:%d)\n",
		"Connexion refusée :":                                                                      "Connection refused:",
		"Arrêt :":                                                                                  "Stopped:",
		"Appairé avec %s.\n":                                                                       "Paired with %s.\n",
		"Relancez les deux commandes pour reprendre : rien de ce qui est vérifié ne sera refait.":  "Run both commands again to resume: nothing already checked will be done again.",
		"Migration terminée.":                                                                      "Migration finished.",
		"espace disque insuffisant":                                                                "not enough disk space",
		"Lancer la migration ? [o/N] ":                                                             "Start the migration? [y/N] ",
		"migration annulée avant toute modification":                                               "migration cancelled before anything was changed",
		"Nouveau mot de passe pour %s : ":                                                          "New password for %s: ",
		"Confirmez : ":                                                                             "Confirm: ",
		"Les deux saisies diffèrent ou sont vides.":                                                "The two entries are different or empty.",
		"  Non installé : %s (%s)\n":                                                               "  Not installed: %s (%s)\n",
		"  Repris :":                                                                               "  Transferred:",
		"  Non repris : %s (%s)\n":                                                                 "  Not transferred: %s (%s)\n",
		"  ÉCHEC : %s (%s)\n":                                                                      "  FAILED: %s (%s)\n",
		"Journal (pour annuler : sudo bernard undo --journal) :":                                   "Log (to undo: sudo bernard undo --journal):",
		"\n=== Plan de migration ===":                                                              "\n=== Migration plan ===",
		"mot de passe repris":                                                                      "password transferred",
		"nouveau mot de passe demandé":                                                             "new password required",
		"  Créer le compte %s (%s)\n":                                                              "  Create the account %s (%s)\n",
		"  Utiliser le compte existant %s\n":                                                       "  Use the existing account %s\n",
		"  Copier %s → /home/%s (%.1f Go, %d fichiers)\n":                                          "  Copy %s → /home/%s (%.1f GB, %d files)\n",
		"  Retirer %s (absente de l'ancien ordinateur)\n":                                          "  Remove %s (not on the old computer)\n",
		"  Reprendre la disposition du clavier de l'ancien ordinateur pour %s\n":                   "  Use the old computer's keyboard layout for %s\n",
		"  Ajouter le dépôt de logiciels %s\n":                                                     "  Add the software source %s\n",
		"  Copier %s (%.1f Go)\n":                                                                  "  Copy %s (%.1f GB)\n",
		"  Installer %d applications depuis les dépôts et %d depuis Flathub\n":                     "  Install %d apps from the software sources and %d from Flathub\n",
		"  %d éléments sans équivalent automatique (listés dans le plan)\n":                        "  %d items with no automatic equivalent (listed in the plan)\n",
		"  Attention :":                                                                            "  Warning:",
		"  Espace : %.1f Go à copier, %.1f Go libres\n":                                            "  Space: %.1f GB to copy, %.1f GB free\n",
		"dossier du paquet sur le disque externe":                                                  "package folder on the external disk",
		"dossier de destination":                                                                   "destination folder",
		"Phrase de passe du paquet : ":                                                             "Package passphrase: ",
		"Paquet illisible :":                                                                       "Can't read the package:",
		"journal de la migration à annuler":                                                        "log of the migration to undo",
		"Journal illisible :":                                                                      "Can't read the log:",
		"Cette migration a créé des comptes ou installé des applications : lancez l'annulation avec sudo.": "This migration created accounts or installed apps: run the undo with sudo.",
		"Annulation impossible :": "Can't undo:",
		"Supprimés : %d fichiers et liens créés par Bernard, %d dossiers vides, %d fichiers temporaires.\n": "Removed: %d files and links created by Bernard, %d empty folders, %d temporary files.\n",
		"Conservé (modifié depuis la copie) :": "Kept (changed since it was copied):",
		"Application retirée :":                "App removed:",
		"Application réinstallée :":            "App reinstalled:",
		"Fichier d'origine remis en place :":   "Original file put back:",
		"Compte supprimé : %s (son dossier personnel est conservé s'il contient encore des fichiers)\n": "Account removed: %s (its home folder is kept if it still contains files)\n",
		"Non annulé :":                         "Not undone:",
		"inventaire produit par bernard-agent": "inventory made by bernard-agent",
		"fichier de plan à écrire":             "plan file to write",
		"Inventaire refusé :":                  "Inventory rejected:",
		"Plan impossible :":                    "Can't make the plan:",
		"Écriture impossible :":                "Can't write:",
		"comptes à créer":                      "accounts to create",
		"comptes existants réutilisés":         "existing accounts reused",
		"installation de Flatpak":              "Flatpak installation",
		"applications à installer":             "apps to install",
		"dossiers à copier":                    "folders to copy",
		"réseaux Wi-Fi à importer":             "Wi-Fi networks to import",
		"connexions VPN à importer":            "VPN connections to import",
		"éléments déjà présents ou inutiles":   "items already there or not needed",
		"actions manuelles proposées":          "suggested manual steps",
		"réglages de comptes":                  "account settings",
		"imprimantes":                          "printers",
		"applications absentes de l'ancien ordinateur (retrait proposé)": "apps not on the old computer (removal suggested)",
		"options de clavier (non cochées)":                               "keyboard options (not ticked)",
		"dépôts de logiciels à ajouter":                                  "software sources to add",
		"données hors des dossiers personnels":                           "data outside the home folders",
		"ouverture de session automatique à couper":                      "automatic login to turn off",
		"Cible : %s %s — %.1f Go libres\n":                               "Target: %s %s — %.1f GB free\n",
		"Volume à transférer : %.1f Go (%d fichiers)\n":                  "Amount to transfer: %.1f GB (%d files)\n",
		"Attention :": "Warning:",
		"BLOQUÉ : espace disque insuffisant sur la cible.": "BLOCKED: not enough disk space on the target.",
		"Plan écrit dans %s — rien n'a été modifié.":       "Plan written to %s — nothing has been changed.",
		"Copie impossible :": "Can't copy:",
		"s'arrêter à la fermeture de l'entrée standard":                                                              "stop when standard input is closed",
		"langue de l'interface (fr, en), transmise par « bernard gui »":                                              "interface language (fr, en), passed on by “bernard gui”",
		"Le moteur doit être lancé en administrateur : utilisez « bernard gui ».":                                    "The engine must be run as administrator: use “bernard gui”.",
		"usage : sudo bernard remove-account IDENTIFIANT":                                                            "usage: sudo bernard remove-account LOGIN",
		"Compte %s non supprimé : %v":                                                                                "Account %s not removed: %v",
		"Compte %s supprimé, avec son dossier personnel.":                                                            "Account %s removed, along with its home folder.",
		"Attention : lancez plutôt « bernard gui » sans sudo ; la fenêtre ne devrait pas tourner en administrateur.": "Warning: run “bernard gui” without sudo instead; the window shouldn't run as administrator.",
		"Impossible de lancer le moteur :":                                                                           "Can't start the engine:",
		"Le moteur n'a pas démarré (mot de passe administrateur refusé ?)":                                           "The engine didn't start (administrator password refused?)",
		"Fenêtre dédiée impossible à ouvrir :":                                                                       "Can't open the Bernard window:",
		"La fenêtre dédiée ne s'affiche pas : ouverture de Bernard dans le navigateur.":                              "The Bernard window isn't showing: opening Bernard in the web browser.",
		"Fenêtre dédiée absente : ouverture dans le navigateur.":                                                     "Bernard window not installed: opening in the web browser.",
		"Fermez Bernard depuis la fenêtre, ou ici avec Ctrl+C.":                                                      "Close Bernard from its window, or here with Ctrl+C.",
		"La fenêtre dédiée de Bernard ne s'affiche pas sur cet ordinateur : Bernard s'ouvre dans le navigateur.\nSupprimez ce fichier pour réessayer la fenêtre.\n": "The Bernard window doesn't show on this computer: Bernard opens in the web browser.\nDelete this file to try the window again.\n",

		// cmd/bernard-agent
		`bernard-agent — agent source de Bernard, l'assistant de migration vers Linux

Usage :
  bernard-agent connect [--code 123456] [--target hôte:port] [--timeout 2h]
      Attend le nouvel ordinateur sur le réseau (Wi-Fi, câble, Thunderbolt),
      aussi longtemps qu'il le faut, puis lui envoie les données après saisie
      du code affiché sur celui-ci. Peut être lancé avant même d'installer
      Linux sur le nouvel ordinateur ; la mise en veille est bloquée.
  bernard-agent pack --dest /media/disque/migration
      Écrit un paquet chiffré sur un disque externe.
  bernard-agent inventory [-o inventaire.json] [--no-data]
  bernard-agent version

L'agent ne modifie jamais cette machine.
`: `bernard-agent — the sending side of Bernard, the assistant for moving to Linux

Usage:
  bernard-agent connect [--code 123456] [--target host:port] [--timeout 2h]
      Waits for the new computer on the network (Wi-Fi, cable, Thunderbolt)
      for as long as it takes, then sends it the data once you enter the
      code shown on it. Can be started even before Linux is installed on
      the new computer; sleep is blocked meanwhile.
  bernard-agent pack --dest /media/disk/migration
      Writes an encrypted package to an external disk.
  bernard-agent inventory [-o inventaire.json] [--no-data]
  bernard-agent version

The agent never changes anything on this computer.
`,
		"Inventaire en cours (lecture seule)…":                 "Taking stock (read only)…",
		"fichier de sortie":                                    "output file",
		"ne pas mesurer les dossiers personnels (plus rapide)": "don't measure the home folders (faster)",
		"racine du système à migrer (disque monté ailleurs)":   "root of the system to transfer (disk mounted elsewhere)",
		"Échec :":                     "Failed:",
		"Inventaire écrit dans %s":    "Inventory written to %s",
		"bureau non détecté":          "desktop not detected",
		"Système      : %s %s (%s)\n": "System       : %s %s (%s)\n",
		"Comptes      : %d\n":         "Accounts     : %d\n",
		"Applications : %d\n":         "Apps         : %d\n",
		"Données      : %s\n":         "Data         : %s\n",
		"Attention    :":              "Warning      :",
		"En attente du nouvel ordinateur (Wi-Fi, câble réseau, Thunderbolt)…":                            "Waiting for the new computer (Wi-Fi, network cable, Thunderbolt)…",
		"Ouvrez Bernard sur le nouvel ordinateur, maintenant ou plus tard : cet ordinateur l'attendra":   "Open Bernard on the new computer, now or later: this computer will wait for it",
		"aussi longtemps qu'il le faut, sans charger le réseau. Ctrl+C pour abandonner.":                 "for as long as it takes, without loading the network. Ctrl+C to give up.",
		"  toujours en attente (%s)…\n":                                                                  "  still waiting (%s)…\n",
		"  Si Bernard est déjà ouvert sur le nouvel ordinateur : les deux sont-ils sur le même réseau ?": "  If Bernard is already open on the new computer: are both on the same network?",
		"  Sur un Wi-Fi invité ou d'entreprise, reliez-les par un câble ou utilisez --target.":           "  On a guest or company Wi-Fi, connect them with a cable or use --target.",
		"délai dépassé : aucun ordinateur trouvé":                                                        "time's up: no computer found",
		"attente abandonnée":                                 "waiting cancelled",
		"Trouvé : %s, par %s\n":                              "Found: %s, via %s\n",
		"Numéro de l'ordinateur : ":                          "Computer number: ",
		"choix invalide":                                     "invalid choice",
		"code à 6 chiffres affiché sur le nouvel ordinateur": "6-digit code shown on the new computer",
		"adresse hôte:port du nouvel ordinateur (sinon recherche automatique)":                                 "host:port address of the new computer (otherwise found automatically)",
		"abandonner si le nouvel ordinateur n'apparaît pas dans ce délai (ex. 2h ; 0 = attendre indéfiniment)": "give up if the new computer doesn't appear within this time (e.g. 2h; 0 = wait forever)",
		"Remarque : lancé sans sudo, l'agent ne lit que vos propres fichiers et les mots de passe":             "Note: without sudo, the agent only reads your own files and passwords",
		"devront être saisis à nouveau. Pour tout migrer : sudo bernard-agent connect":                         "will have to be entered again. To transfer everything: sudo bernard-agent connect",
		"Migration Bernard : attente du nouvel ordinateur et envoi des données":                                "Bernard migration: waiting for the new computer and sending data",
		"Remarque : impossible de bloquer la mise en veille ; désactivez-la le temps de la migration.":         "Note: can't block sleep; turn it off for the duration of the migration.",
		"Écoute du réseau impossible (Bernard est-il déjà ouvert sur cet ordinateur ?) :":                      "Can't listen on the network (is Bernard already open on this computer?):",
		"Échec de l'inventaire :":                  "Couldn't take stock:",
		"Code affiché sur le nouvel ordinateur : ": "Code shown on the new computer: ",
		"Appairage impossible :":                   "Pairing failed:",
		"Connecté à %s. Transfert en cours — laissez cette fenêtre ouverte.\n":                            "Connected to %s. Transfer in progress — leave this window open.\n",
		"Vous pouvez brancher un câble réseau à tout moment : le transfert basculera dessus de lui-même.": "You can plug in a network cable at any time: the transfer will switch to it on its own.",
		"  %d fichiers envoyés (%s)\n":                                                                         "  %d files sent (%s)\n",
		"Le nouvel ordinateur installe les comptes et les applications…":                                       "The new computer is installing the accounts and apps…",
		"Copie des fichiers : %s à envoyer.\n":                                                                 "Copying files: %s to send.\n",
		"Le nouvel ordinateur applique les réglages…":                                                          "The new computer is applying the settings…",
		"Transfert interrompu après %d fichiers : %v\n":                                                        "Transfer interrupted after %d files: %v\n",
		"Relancez la commande (un nouveau code sera demandé) : rien de ce qui est vérifié ne sera renvoyé.":    "Run the command again (you'll be asked for a new code): nothing already checked will be sent again.",
		"Terminé : %d fichiers envoyés (%s). Rien n'a été modifié sur cet ordinateur.\n":                       "Done: %d files sent (%s). Nothing was changed on this computer.\n",
		"dossier vide sur le disque externe":                                                                   "empty folder on the external disk",
		"Phrase de passe pour chiffrer le paquet (8 caractères minimum) : ":                                    "Passphrase to encrypt the package (at least 8 characters): ",
		"Confirmez la phrase de passe : ":                                                                      "Confirm the passphrase: ",
		"Les deux saisies diffèrent.":                                                                          "The two entries are different.",
		"Migration Bernard : écriture du paquet sur le disque externe":                                         "Bernard migration: writing the package to the external disk",
		"Remarque : sans sudo, ni les mots de passe, ni les réglages du bureau, ni le Wi-Fi ne seront inclus.": "Note: without sudo, passwords, desktop settings and Wi-Fi won't be included.",
		"  %d fichiers écrits\n":                                                                               "  %d files written\n",
		"Écriture du paquet impossible :":                                                                      "Couldn't write the package:",
		"Videz le dossier de destination avant de recommencer.":                                                "Empty the destination folder before trying again.",
		"Paquet écrit : %d fichiers (%s) dans %s\n":                                                            "Package written: %d files (%s) in %s\n",
		"Non inclus :": "Not included:",
		"Conservez la phrase de passe : sans elle, le paquet est illisible.": "Keep the passphrase safe: without it, the package can't be read.",
		"%d h %02d": "%d h %02d min",
	})
}
