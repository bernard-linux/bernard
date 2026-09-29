// Package inventory définit le format de l'inventaire produit par l'agent
// source : ce qui existe sur l'ancienne machine. Il ne décrit aucune action ;
// les actions sont le rôle du plan (package plan).
package inventory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bernard-linux/bernard/internal/aptrepo"
)

// Schema identifie la version du format. Le moteur refuse toute autre valeur.
const Schema = "migration-inventory/1"

// Inventory est la description complète de la machine source.
type Inventory struct {
	Schema    string    `json:"schema"`
	CreatedAt time.Time `json:"createdAt"`
	Agent     string    `json:"agent"` // version de l'agent qui l'a produit
	Source    Source    `json:"source"`
	Users     []User    `json:"users"`
	Apps      []App     `json:"apps"`
	DataSets  []DataSet `json:"dataSets"`
	Network   Network   `json:"network"`
	// Packages liste TOUS les paquets apt installés (pas seulement ceux
	// installés à la main) : la cible s'en sert pour repérer les
	// applications que l'utilisateur avait retirées. Vide avec un agent
	// antérieur à la 0.3 : la proposition de retrait est alors désactivée.
	Packages []string `json:"packages,omitempty"`
	// PackagesRemoved : paquets retirés d'après le journal de dpkg, avec la
	// date du retrait (AAAA-MM-JJ). Le journal ne remonte qu'à un an environ.
	PackagesRemoved map[string]string `json:"packagesRemoved,omitempty"`
	// AptSources : dépôts de logiciels ajoutés (sources.list.d) et leurs clés.
	AptSources []aptrepo.Source `json:"aptSources,omitempty"`
	// System : données hors des dossiers personnels (sites, bases, services,
	// réglages système modifiés, autres disques…). Détectées depuis la 0.4.
	System []SystemItem `json:"system,omitempty"`
	// Disks : disques et partitions montés sur la source.
	Disks []Disk `json:"disks,omitempty"`
	// Warnings liste ce que l'agent n'a pas pu inventorier, pour le rapport.
	Warnings []string `json:"warnings,omitempty"`
}

// Source décrit le système d'exploitation de la machine source.
type Source struct {
	OS       string `json:"os"`               // linux, windows, darwin
	Distro   string `json:"distro,omitempty"` // ubuntu, debian, zorin, linuxmint…
	Version  string `json:"version"`
	Desktop  string `json:"desktop,omitempty"` // gnome, cinnamon, kde…
	Hostname string `json:"hostname"`
	Snapshot string `json:"snapshot"` // vss, apfs, btrfs, lvm, none
	// Codename : nom de code Ubuntu (ou Debian) de la version (« noble »),
	// utilisé pour adapter les PPA à la version de la cible.
	Codename string `json:"codename,omitempty"`
	// Keyboard : disposition du clavier du système (« fr », « be »…).
	Keyboard string `json:"keyboard,omitempty"`
	// GPUs : fabricants des cartes graphiques (« intel », « nvidia »…).
	GPUs []string `json:"gpus,omitempty"`
}

// User est un compte humain de la machine source.
type User struct {
	ID       string   `json:"id"`
	Login    string   `json:"login"`
	FullName string   `json:"fullName,omitempty"`
	UID      int      `json:"uid,omitempty"`
	Home     string   `json:"home"`
	Shell    string   `json:"shell,omitempty"`
	Groups   []string `json:"groups,omitempty"`
}

// Origines possibles d'une application.
const (
	OriginApt     = "apt"
	OriginFlatpak = "flatpak"
	OriginSnap    = "snap"
	OriginWindows = "windows"
	OriginMac     = "macos"
)

// App est une application installée sur la source.
type App struct {
	ID       string `json:"id"`
	SourceID string `json:"sourceId"` // ex. "apt:gimp", "flatpak:org.gimp.GIMP"
	Name     string `json:"name"`
	Version  string `json:"version,omitempty"`
	Origin   string `json:"origin"`
	// Repo : adresse (normalisée) du dépôt d'où vient la version installée,
	// quand ce n'est pas celui de la distribution qui la fournit.
	Repo string `json:"repo,omitempty"`
	// LastUsed est vide quand la date est inconnue ; l'interface garde alors
	// l'application cochée par défaut.
	LastUsed *time.Time `json:"lastUsed,omitempty"`
}

// DataSet est un ensemble de fichiers à transférer, rattaché à un utilisateur.
type DataSet struct {
	ID        string   `json:"id"`
	User      string   `json:"user"` // User.ID
	Kind      string   `json:"kind"` // home, documents, …
	Path      string   `json:"path"`
	Files     int64    `json:"files"`
	SizeBytes int64    `json:"sizeBytes"`
	Excluded  []string `json:"excluded,omitempty"` // chemins relatifs exclus par défaut
	// Include limite la copie à ces chemins relatifs (fichiers de /etc
	// modifiés, fichiers de /opt n'appartenant à aucun paquet…). Vide : tout.
	Include []string `json:"include,omitempty"`
	// Dest : emplacement sur la cible, pour les données hors des dossiers
	// personnels (même chemin que sur la source).
	Dest string `json:"dest,omitempty"`
	// System : identifiant du SystemItem que ce jeu de données copie.
	System string `json:"system,omitempty"`
	// Service : unités systemd à arrêter pendant la copie, des deux côtés
	// (« mysql », « docker.socket docker ») ; « libvirtd » : machines
	// virtuelles à éteindre.
	Service string `json:"service,omitempty"`
}

// Genres de données hors des dossiers personnels.
const (
	SysWeb       = "web"       // sites web (/var/www…)
	SysDatabase  = "database"  // bases MySQL, MariaDB, PostgreSQL
	SysContainer = "container" // Docker, Podman, LXD
	SysVM        = "vm"        // disques de machines virtuelles
	SysAppServer = "appserver" // serveur d'application (FileMaker…)
	SysOpt       = "opt"       // logiciel installé à la main dans /opt
	SysSrv       = "srv"       // données de service dans /srv
	SysLocal     = "local"     // /usr/local
	SysEtc       = "etc"       // réglages système modifiés ou ajoutés
	SysRoot      = "root"      // dossier de l'administrateur (/root)
	SysService   = "service"   // données d'un autre service (/var/lib/…)
	SysCustom    = "custom"    // dossier ajouté à la racine (/data…)
	SysDisk      = "disk"      // données sur un autre disque ou partition
	SysHomeElse  = "homeelse"  // dossier personnel placé sur un autre disque
	SysBackup    = "backup"    // sauvegardes (Timeshift, Déjà Dup…)
	SysSteam     = "steam"     // bibliothèque Steam hors du dossier personnel
)

// Conseils associés à un élément.
const (
	AdviceCopy   = "copy"   // à copier
	AdviceSkip   = "skip"   // copie déconseillée (sauvegarde, données régénérables)
	AdviceAttach = "attach" // disque à rattacher tel quel s'il est déplacé
	AdviceReview = "review" // à examiner par l'utilisateur
)

// SystemItem est un ensemble de données hors des dossiers personnels.
type SystemItem struct {
	ID    string   `json:"id"`
	Kind  string   `json:"kind"`
	Label string   `json:"label"`
	Paths []string `json:"paths"` // chemins absolus sur la source
	Files int64    `json:"files"`
	Bytes int64    `json:"bytes"` // taille apparente
	// Used est la place réellement occupée : plus petite que Bytes pour les
	// fichiers creux (disques virtuels), qu'il faudra copier creux.
	Used int64 `json:"used"`
	// Service : service à arrêter pendant la copie (mysql, docker…).
	Service string `json:"service,omitempty"`
	Advice  string `json:"advice"`
	// Detail : liste des fichiers concernés quand elle est courte (/etc).
	Detail []string `json:"detail,omitempty"`
	// Pour un autre disque : identifiant du système de fichiers (reconnu si
	// le disque est déplacé dans le nouvel ordinateur) et son type.
	UUID   string `json:"uuid,omitempty"`
	FSType string `json:"fstype,omitempty"`
}

// Disk est un système de fichiers monté sur la source.
type Disk struct {
	Device string `json:"device"`
	Mount  string `json:"mount"`
	FSType string `json:"fstype"`
	Label  string `json:"label,omitempty"`
	UUID   string `json:"uuid,omitempty"`
	Size   int64  `json:"size"`
	Used   int64  `json:"used"`
	Role   string `json:"role"` // system, home, boot, data
}

// Network regroupe ce qui touche au réseau et aux périphériques.
type Network struct {
	Wifi     []string `json:"wifi,omitempty"`
	Printers []string `json:"printers,omitempty"`
}

// Validate vérifie la cohérence minimale d'un inventaire.
func (inv *Inventory) Validate() error {
	if inv.Schema != Schema {
		return fmt.Errorf("format d'inventaire non pris en charge : %q (attendu %q)", inv.Schema, Schema)
	}
	users := map[string]bool{}
	for _, u := range inv.Users {
		if u.ID == "" || u.Login == "" {
			return fmt.Errorf("utilisateur incomplet : %+v", u)
		}
		users[u.ID] = true
	}
	for _, d := range inv.DataSets {
		if d.Kind == "system" {
			if d.Dest == "" || !strings.HasPrefix(d.Dest, "/") || strings.Contains(d.Dest, "..") {
				return fmt.Errorf("jeu de données %s : destination refusée %q", d.ID, d.Dest)
			}
			continue
		}
		if !users[d.User] {
			return fmt.Errorf("jeu de données %s rattaché à un utilisateur inconnu %q", d.ID, d.User)
		}
	}
	return nil
}

// Digest renvoie l'empreinte de l'inventaire, reprise dans le plan pour
// garantir qu'un plan ne peut pas être appliqué à un autre inventaire.
func (inv *Inventory) Digest() (string, error) {
	b, err := json.Marshal(inv)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Identity renvoie l'empreinte de l'identité de la migration : machine
// source, comptes et jeux de données, sans les volumes ni les dates. Elle
// reste identique si l'agent est relancé (coupure côté source), ce qui permet
// de reprendre avec le même journal.
func (inv *Inventory) Identity() string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s\n", inv.Source.OS, inv.Source.Hostname, inv.Source.Distro)
	for _, u := range inv.Users {
		fmt.Fprintf(h, "u|%s|%s|%s\n", u.ID, u.Login, u.Home)
	}
	for _, d := range inv.DataSets {
		fmt.Fprintf(h, "d|%s|%s|%s\n", d.ID, d.User, d.Path)
	}
	return "id:" + hex.EncodeToString(h.Sum(nil))
}

// Save écrit l'inventaire en JSON indenté.
func (inv *Inventory) Save(path string) error {
	b, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

// Load lit et valide un inventaire.
func Load(path string) (*Inventory, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var inv Inventory
	if err := json.Unmarshal(b, &inv); err != nil {
		return nil, fmt.Errorf("inventaire illisible : %w", err)
	}
	if err := inv.Validate(); err != nil {
		return nil, err
	}
	return &inv, nil
}
