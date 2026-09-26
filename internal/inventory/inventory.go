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
	"time"
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
