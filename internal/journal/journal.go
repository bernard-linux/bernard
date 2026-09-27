// Package journal consigne la migration sur la cible, ligne par ligne (JSON),
// avec synchronisation sur disque après chaque enregistrement.
//
// Il permet de reprendre après une coupure (courant, réseau, plantage) et
// d'annuler proprement : on sait exactement ce que Bernard a créé.
// Une dernière ligne tronquée par une coupure est ignorée à la relecture.
package journal

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// Types d'enregistrements.
const (
	RecBegin    = "begin"    // début ou reprise de session
	RecProgress = "progress" // octets reçus et synchronisés d'un fichier
	RecDone     = "done"     // fichier ou lien vérifié et placé
	RecDir      = "dir"      // dossier créé par Bernard
	RecError    = "error"    // échec sur un élément
	RecFinish   = "finish"   // migration terminée
	RecSys      = "sys"      // modification système faite par Bernard (Op, Name)
)

// Opérations système journalisées, pour l'annulation.
const (
	SysUserCreated  = "userCreated"
	SysAptAdded     = "aptAdded"
	SysFlathubAdded = "flathubAdded"
	SysFlatpakAdded = "flatpakAdded"
	SysWifiAdded    = "wifiAdded"    // Name = connexion, Dst = fichier créé
	SysPrinterAdded = "printerAdded" // Name = imprimante
	SysCrontabSet   = "crontabSet"   // Name = identifiant
	SysDconfApplied = "dconfApplied" // Name = identifiant, Dst = sauvegarde
	// SysPreferSource : fichier de profil de l'ancien ordinateur mis à la
	// place de celui du nouveau. Name = nom de conflit d'origine, Dst = nom
	// final, Key = emplacement où la version du nouveau a été mise de côté.
	SysPreferSource = "preferSource"
	// SysAptRemoved : paquet retiré de la cible à la demande (application
	// que l'utilisateur avait supprimée de l'ancien ordinateur).
	SysAptRemoved = "aptRemoved"
	// SysFlatpakRemoved : idem pour une application Flatpak.
	SysFlatpakRemoved = "flatpakRemoved"
	// SysAutoLoginOff : ouverture de session automatique coupée. Name =
	// fichier de configuration, Dst = sauvegarde.
	SysAutoLoginOff = "autoLoginOff"
)

// Record est une ligne du journal.
type Record struct {
	T         string    `json:"t"`
	Time      time.Time `json:"time"`
	Inventory string    `json:"inventory,omitempty"`
	Key       string    `json:"key,omitempty"` // "<dataset>/<chemin relatif>"
	Part      string    `json:"part,omitempty"`
	Offset    int64     `json:"offset,omitempty"`
	Dst       string    `json:"dst,omitempty"`
	Hash      string    `json:"hash,omitempty"`
	Status    string    `json:"status,omitempty"`
	Size      int64     `json:"size,omitempty"`
	SrcMTime  time.Time `json:"srcMtime,omitempty"` // date du fichier source copié
	Error     string    `json:"error,omitempty"`
	Op        string    `json:"op,omitempty"`
	Name      string    `json:"name,omitempty"`
}

// Journal est ouvert en ajout seul.
type Journal struct {
	mu   sync.Mutex
	f    *os.File
	Path string
}

// Open ouvre (ou crée) le journal.
func Open(path string) (*Journal, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &Journal{f: f, Path: path}, nil
}

// Append écrit un enregistrement et le synchronise sur disque avant de
// rendre la main : ce qui est journalisé survit à une coupure.
func (j *Journal) Append(r Record) error {
	if r.Time.IsZero() {
		r.Time = time.Now().UTC()
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, err := j.f.Write(append(b, '\n')); err != nil {
		return err
	}
	return j.f.Sync()
}

// Write écrit un enregistrement sans attendre le disque. Il ne survit à une
// coupure qu'après le prochain Sync (ou Append). Sert aux enregistrements
// groupés : un seul passage sur le disque pour des centaines de petits
// fichiers, au lieu d'un par fichier.
func (j *Journal) Write(r Record) error {
	if r.Time.IsZero() {
		r.Time = time.Now().UTC()
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	_, err = j.f.Write(append(b, '\n'))
	return err
}

// Sync rend durables les enregistrements écrits par Write.
func (j *Journal) Sync() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.f.Sync()
}

// Close ferme le journal.
func (j *Journal) Close() error { return j.f.Close() }

// Progress est l'état d'un fichier en cours de réception.
type Progress struct {
	Part   string
	Offset int64
}

// State est l'état reconstitué à partir du journal.
type State struct {
	Inventory string
	Done      map[string]Record   // par clé
	Progress  map[string]Progress // fichiers commencés, non terminés
	Dirs      []string            // dossiers créés par Bernard, dans l'ordre
	Sys       []Record            // modifications système, dans l'ordre
	Finished  bool
}

// Load relit un journal. Un journal absent donne un état vide.
func Load(path string) (*State, error) {
	st := &State{Done: map[string]Record{}, Progress: map[string]Progress{}}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var pendingErr error
	for sc.Scan() {
		if pendingErr != nil {
			// Une ligne illisible suivie d'autres lignes : journal corrompu.
			return nil, pendingErr
		}
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			pendingErr = fmt.Errorf("journal corrompu : %w", err)
			continue
		}
		switch r.T {
		case RecBegin:
			if st.Inventory != "" && r.Inventory != st.Inventory {
				return nil, errors.New("journal : l'inventaire a changé en cours de migration")
			}
			st.Inventory = r.Inventory
		case RecProgress:
			st.Progress[r.Key] = Progress{Part: r.Part, Offset: r.Offset}
		case RecDone:
			st.Done[r.Key] = r
			delete(st.Progress, r.Key)
		case RecDir:
			st.Dirs = append(st.Dirs, r.Dst)
		case RecSys:
			st.Sys = append(st.Sys, r)
		case RecFinish:
			st.Finished = true
		}
	}
	// pendingErr non nul ici = dernière ligne tronquée par une coupure : ignorée.
	return st, sc.Err()
}
