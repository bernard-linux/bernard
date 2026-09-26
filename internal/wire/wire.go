// Package wire définit le découpage en trames du protocole entre l'agent et
// le moteur : 1 octet de type, 4 octets de longueur (gros-boutiste), puis la
// charge utile. Les messages de contrôle sont en JSON, les données en brut.
package wire

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Types de trames.
const (
	FrameJSON byte = 'J'
	FrameData byte = 'D'
)

// MaxPayload borne la taille d'une trame (protection mémoire).
const MaxPayload = 4 << 20

// ChunkSize est la taille des trames de données envoyées.
const ChunkSize = 1 << 20

// Protocol est la version du protocole, échangée au début de la session.
const Protocol = 1

// Write envoie une trame.
func Write(w io.Writer, typ byte, payload []byte) error {
	if len(payload) > MaxPayload {
		return fmt.Errorf("trame trop grande : %d octets", len(payload))
	}
	var hdr [5]byte
	hdr[0] = typ
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// Read lit une trame.
func Read(r io.Reader) (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[1:])
	if n > MaxPayload {
		return 0, nil, fmt.Errorf("trame trop grande annoncée : %d octets", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, nil, err
	}
	return hdr[0], buf, nil
}

// WriteJSON envoie un message de contrôle.
func WriteJSON(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return Write(w, FrameJSON, b)
}

// ReadJSON lit un message de contrôle ; une trame d'un autre type est une
// erreur de protocole.
func ReadJSON(r io.Reader, v any) error {
	typ, b, err := Read(r)
	if err != nil {
		return err
	}
	if typ != FrameJSON {
		return fmt.Errorf("protocole : message attendu, trame %q reçue", typ)
	}
	return json.Unmarshal(b, v)
}

// Msg est l'enveloppe commune des messages de contrôle.
type Msg struct {
	Type    string          `json:"type"`
	Error   string          `json:"error,omitempty"`
	Dataset string          `json:"dataset,omitempty"`
	Rel     string          `json:"rel,omitempty"`
	Offset  int64           `json:"offset,omitempty"`
	Hash    string          `json:"hash,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
}

// Types de messages.
const (
	MsgInventory = "inventory" // demande / réponse d'inventaire
	MsgList      = "list"      // demande de liste d'un jeu de données
	MsgEntry     = "entry"     // un élément de la liste
	MsgGet       = "get"       // demande d'un fichier à partir d'un décalage
	MsgFile      = "file"      // en-tête de fichier (Body = Entry)
	MsgDone      = "done"      // fin de fichier (Hash) ou fin de liste
	MsgSecrets   = "secrets"   // hachages des mots de passe (Body = login → hachage)
	MsgExtras    = "extras"    // réglages lus en administrateur (Body = settings.Extras)
	MsgError     = "error"
	MsgBye       = "bye"
)
