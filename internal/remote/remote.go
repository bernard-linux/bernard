// Package remote implémente le protocole entre l'agent et le moteur, une fois
// la session appairée : l'agent (Server) répond, le moteur (Client) demande.
//
// Le moteur mène le jeu : il demande l'inventaire, la liste d'un jeu de
// données, puis chaque fichier à partir d'un décalage (reprise à l'octet près).
// L'agent envoie les données puis l'empreinte BLAKE3 du fichier entier.
package remote

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/zeebo/blake3"

	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/source"
	"github.com/bernard-linux/bernard/internal/wire"
)

// Server répond aux demandes du moteur. Il ne sert que les fichiers situés
// sous les jeux de données de son propre inventaire.
type Server struct {
	Inv *inventory.Inventory
	// OnFile est appelé après chaque fichier envoyé (progression côté source).
	OnFile func(rel string, bytes int64)
	// OnBytes est appelé à chaque bloc envoyé, pour une progression fluide
	// pendant les gros fichiers.
	OnBytes func(rel string, n int64)
	// Secrets fournit les hachages de mots de passe, si l'agent peut les lire.
	Secrets func() (map[string]string, error)
	// Extras fournit les réglages lus en administrateur (Wi-Fi, bureau…).
	Extras func() (any, error)
	// OnStatus reçoit l'état annoncé par le nouvel ordinateur (étape en
	// cours, volume prévu), pour l'afficher côté source.
	OnStatus func(Status)
}

// Status est l'état que le moteur annonce à l'agent.
type Status struct {
	Phase   string `json:"phase"`             // analysing, system, copy, settings, done
	Planned int64  `json:"planned,omitempty"` // octets à copier
	Files   int64  `json:"files,omitempty"`   // fichiers à copier
}

func (s *Server) dataset(id string) (inventory.DataSet, error) {
	for _, d := range s.Inv.DataSets {
		if d.ID == id {
			return d, nil
		}
	}
	return inventory.DataSet{}, fmt.Errorf("jeu de données inconnu : %q", id)
}

// Serve traite les demandes jusqu'au message de fin ou à la coupure.
//
// Les réponses sont mises en tampon et envoyées d'un bloc tant que d'autres
// demandes attendent déjà (le moteur en envoie plusieurs d'avance) : des
// centaines de petits fichiers partent en quelques paquets réseau.
func (s *Server) Serve(ctx context.Context, conn io.ReadWriter) error {
	br := bufio.NewReaderSize(conn, 64<<10)
	bw := bufio.NewWriterSize(conn, 256<<10)
	defer bw.Flush()
	rw := struct {
		io.Reader
		io.Writer
	}{br, bw}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if br.Buffered() == 0 {
			if err := bw.Flush(); err != nil {
				return err
			}
		}
		var m wire.Msg
		if err := wire.ReadJSON(rw, &m); err != nil {
			return err
		}
		var err error
		switch m.Type {
		case wire.MsgInventory:
			var body []byte
			if body, err = json.Marshal(s.Inv); err == nil {
				err = wire.WriteJSON(rw, wire.Msg{Type: wire.MsgInventory, Body: body})
			}
		case wire.MsgSecrets:
			err = s.secrets(rw)
		case wire.MsgExtras:
			err = s.extras(rw)
		case wire.MsgList:
			err = s.list(rw, m.Dataset)
		case wire.MsgGet:
			err = s.get(rw, m.Dataset, m.Rel, m.Offset)
		case wire.MsgStatus:
			var st Status
			if json.Unmarshal(m.Body, &st) == nil && s.OnStatus != nil {
				s.OnStatus(st)
			}
			err = wire.WriteJSON(rw, wire.Msg{Type: wire.MsgDone})
		case wire.MsgBye:
			if s.OnStatus != nil {
				s.OnStatus(Status{Phase: "done"})
			}
			return nil
		default:
			err = wire.WriteJSON(rw, wire.Msg{Type: wire.MsgError, Error: "demande inconnue : " + m.Type})
		}
		if err != nil {
			return err
		}
	}
}

func (s *Server) secrets(w io.Writer) error {
	if s.Secrets == nil {
		return wire.WriteJSON(w, wire.Msg{Type: wire.MsgError, Error: "mots de passe non disponibles"})
	}
	m, err := s.Secrets()
	if err != nil {
		return wire.WriteJSON(w, wire.Msg{Type: wire.MsgError, Error: "mots de passe non lisibles (agent lancé sans droits administrateur ?)"})
	}
	b, _ := json.Marshal(m)
	return wire.WriteJSON(w, wire.Msg{Type: wire.MsgSecrets, Body: b})
}

func (s *Server) extras(w io.Writer) error {
	if s.Extras == nil {
		return wire.WriteJSON(w, wire.Msg{Type: wire.MsgError, Error: "réglages non disponibles (agent lancé sans droits administrateur ?)"})
	}
	v, err := s.Extras()
	if err != nil {
		return wire.WriteJSON(w, wire.Msg{Type: wire.MsgError, Error: err.Error()})
	}
	b, _ := json.Marshal(v)
	return wire.WriteJSON(w, wire.Msg{Type: wire.MsgExtras, Body: b})
}

func (s *Server) list(w io.Writer, id string) error {
	ds, err := s.dataset(id)
	if err != nil {
		return wire.WriteJSON(w, wire.Msg{Type: wire.MsgError, Error: err.Error()})
	}
	var sendErr error
	walkErr := source.WalkDataSet(ds.Path, ds.Excluded, ds.Include, func(e source.Entry) error {
		b, _ := json.Marshal(e)
		sendErr = wire.WriteJSON(w, wire.Msg{Type: wire.MsgEntry, Body: b})
		return sendErr
	}, func(rel string, err error) {
		if sendErr == nil {
			sendErr = wire.WriteJSON(w, wire.Msg{Type: wire.MsgEntry, Rel: rel, Error: err.Error()})
		}
	})
	if sendErr != nil {
		return sendErr
	}
	if walkErr != nil {
		return wire.WriteJSON(w, wire.Msg{Type: wire.MsgError, Error: walkErr.Error()})
	}
	return wire.WriteJSON(w, wire.Msg{Type: wire.MsgDone})
}

func (s *Server) get(w io.Writer, id, rel string, offset int64) error {
	refuse := func(err error) error {
		return wire.WriteJSON(w, wire.Msg{Type: wire.MsgError, Rel: rel, Error: err.Error()})
	}
	ds, err := s.dataset(id)
	if err != nil {
		return refuse(err)
	}
	if source.Excluded(rel, ds.Excluded) || !source.NewIncluder(ds.Include).File(rel) {
		return refuse(source.ErrOutsideRoot)
	}
	f, info, err := source.OpenRegular(ds.Path, rel)
	if err != nil {
		return refuse(err)
	}
	defer f.Close()
	if offset < 0 || offset > info.Size() {
		offset = 0
	}
	entry := source.Entry{Rel: rel, Kind: source.KindFile, Size: info.Size(), Mode: info.Mode(), MTime: info.ModTime()}
	b, _ := json.Marshal(entry)
	if err := wire.WriteJSON(w, wire.Msg{Type: wire.MsgFile, Offset: offset, Body: b}); err != nil {
		return err
	}
	// L'empreinte porte sur le fichier entier : le début déjà reçu est relu
	// localement, sans être renvoyé.
	h := blake3.New()
	if _, err := io.CopyN(h, f, offset); err != nil {
		return wire.WriteJSON(w, wire.Msg{Type: wire.MsgError, Rel: rel, Error: err.Error()})
	}
	bp := chunkPool.Get().(*[]byte)
	defer chunkPool.Put(bp)
	buf := *bp
	var sent int64
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			if err := wire.Write(w, wire.FrameData, buf[:n]); err != nil {
				return err
			}
			sent += int64(n)
			if s.OnBytes != nil {
				s.OnBytes(rel, int64(n))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return wire.WriteJSON(w, wire.Msg{Type: wire.MsgError, Rel: rel, Error: rerr.Error()})
		}
	}
	after, err := f.Stat()
	if err != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || offset+sent != info.Size() {
		return wire.WriteJSON(w, wire.Msg{Type: wire.MsgError, Rel: rel, Error: "fichier modifié pendant l'envoi"})
	}
	if s.OnFile != nil {
		s.OnFile(rel, sent)
	}
	return wire.WriteJSON(w, wire.Msg{Type: wire.MsgDone, Hash: "blake3:" + hex.EncodeToString(h.Sum(nil))})
}

var chunkPool = sync.Pool{New: func() any { b := make([]byte, wire.ChunkSize); return &b }}

// Client est la vue « source » d'un agent distant, pour le moteur.
//
// Les demandes de fichiers peuvent être envoyées d'avance (Prefetch) : l'agent
// y répond dans l'ordre, sans attendre un aller-retour réseau par fichier.
// Get consomme les réponses dans le même ordre.
type Client struct {
	RW io.ReadWriteCloser

	once    sync.Once
	br      *bufio.Reader
	bw      *bufio.Writer
	pending []getReq // demandes envoyées d'avance, réponses non lues
	cur     *stream  // fichier en cours de lecture
}

type getReq struct {
	dataset, rel string
	offset       int64
}

func (c *Client) init() {
	c.once.Do(func() {
		c.br = bufio.NewReaderSize(c.RW, 256<<10)
		c.bw = bufio.NewWriterSize(c.RW, 16<<10)
	})
}

func (c *Client) send(m wire.Msg) error {
	c.init()
	return wire.WriteJSON(c.bw, m)
}

// recv envoie les demandes en attente puis lit un message de contrôle.
func (c *Client) recv(m *wire.Msg) error {
	if err := c.bw.Flush(); err != nil {
		return err
	}
	return wire.ReadJSON(c.br, m)
}

// settle lit jusqu'au bout le fichier en cours et les réponses envoyées
// d'avance qui n'ont pas été consommées, pour que le protocole reste aligné.
func (c *Client) settle(keep int) error {
	c.init()
	if c.cur != nil {
		if _, err := c.cur.Finish(); err != nil && !isFileErr(err) {
			return err
		}
		c.cur = nil
	}
	for len(c.pending) > keep {
		c.pending = c.pending[1:]
		if err := c.skipResponse(); err != nil {
			return err
		}
	}
	return nil
}

// skipResponse lit et jette la réponse à une demande de fichier.
func (c *Client) skipResponse() error {
	var m wire.Msg
	if err := c.recv(&m); err != nil {
		return err
	}
	switch m.Type {
	case wire.MsgError:
		return nil
	case wire.MsgFile:
		st := &stream{r: c.br}
		if _, err := st.Finish(); err != nil && !isFileErr(err) {
			return err
		}
		return nil
	}
	return fmt.Errorf("protocole : message inattendu %q", m.Type)
}

func isFileErr(err error) bool {
	_, ok := err.(*source.FileError)
	return ok
}

// call envoie une demande simple et lit sa réponse.
func (c *Client) call(req wire.Msg, m *wire.Msg) error {
	if err := c.settle(0); err != nil {
		return err
	}
	if err := c.send(req); err != nil {
		return err
	}
	return c.recv(m)
}

func (c *Client) Inventory(ctx context.Context) (*inventory.Inventory, error) {
	var m wire.Msg
	if err := c.call(wire.Msg{Type: wire.MsgInventory}, &m); err != nil {
		return nil, err
	}
	if m.Type != wire.MsgInventory {
		return nil, &source.FileError{Msg: m.Error}
	}
	var inv inventory.Inventory
	if err := json.Unmarshal(m.Body, &inv); err != nil {
		return nil, err
	}
	return &inv, inv.Validate()
}

// List transmet chaque élément à fn. Un élément illisible côté source arrive
// avec Kind = KindUnreadable et le message d'erreur dans Link.
func (c *Client) List(ctx context.Context, dataset string, fn func(source.Entry) error) error {
	if err := c.settle(0); err != nil {
		return err
	}
	if err := c.send(wire.Msg{Type: wire.MsgList, Dataset: dataset}); err != nil {
		return err
	}
	var fnErr error
	for {
		var m wire.Msg
		if err := c.recv(&m); err != nil {
			return err
		}
		switch m.Type {
		case wire.MsgDone:
			return fnErr
		case wire.MsgError:
			return &source.FileError{Msg: m.Error}
		case wire.MsgEntry:
			if fnErr != nil {
				continue // on vide la liste pour garder le protocole synchronisé
			}
			if m.Error != "" {
				fnErr = fn(source.Entry{Rel: m.Rel, Kind: source.KindUnreadable, Link: m.Error})
				continue
			}
			var e source.Entry
			if err := json.Unmarshal(m.Body, &e); err != nil {
				return err
			}
			fnErr = fn(e)
		default:
			return fmt.Errorf("protocole : message inattendu %q", m.Type)
		}
	}
}

// Secrets demande les hachages des mots de passe. Une erreur signifie
// simplement qu'il faudra saisir de nouveaux mots de passe.
func (c *Client) Secrets(ctx context.Context) (map[string]string, error) {
	var m wire.Msg
	if err := c.call(wire.Msg{Type: wire.MsgSecrets}, &m); err != nil {
		return nil, err
	}
	if m.Type != wire.MsgSecrets {
		return nil, &source.FileError{Msg: m.Error}
	}
	out := map[string]string{}
	return out, json.Unmarshal(m.Body, &out)
}

// Extras demande les réglages lus en administrateur ; out reçoit le JSON.
func (c *Client) Extras(ctx context.Context, out any) error {
	var m wire.Msg
	if err := c.call(wire.Msg{Type: wire.MsgExtras}, &m); err != nil {
		return err
	}
	if m.Type != wire.MsgExtras {
		return &source.FileError{Msg: m.Error}
	}
	return json.Unmarshal(m.Body, out)
}

// SendStatus annonce l'étape en cours à l'agent, qui l'affiche. Un agent
// plus ancien ne connaît pas ce message : son refus est ignoré.
func (c *Client) SendStatus(ctx context.Context, st Status) error {
	b, _ := json.Marshal(st)
	var m wire.Msg
	return c.call(wire.Msg{Type: wire.MsgStatus, Body: b}, &m)
}

// Prefetch demande un fichier d'avance ; Get le lira plus tard.
func (c *Client) Prefetch(dataset, rel string, offset int64) error {
	if err := c.send(wire.Msg{Type: wire.MsgGet, Dataset: dataset, Rel: rel, Offset: offset}); err != nil {
		return err
	}
	c.pending = append(c.pending, getReq{dataset, rel, offset})
	return nil
}

// Flush envoie les demandes faites d'avance.
func (c *Client) Flush() error {
	c.init()
	return c.bw.Flush()
}

func (c *Client) Get(ctx context.Context, dataset, rel string, offset int64) (source.FileStream, error) {
	want := getReq{dataset, rel, offset}
	// Réponses demandées d'avance mais plus utiles (fichier sauté) : lues et
	// jetées jusqu'à celle-ci. Si elle n'a pas été demandée, tout est vidé.
	idx := -1
	for i, p := range c.pending {
		if p == want {
			idx = i
			break
		}
	}
	keep := 0
	if idx >= 0 {
		keep = len(c.pending) - idx
	}
	if err := c.settle(keep); err != nil {
		return nil, err
	}
	if idx >= 0 {
		c.pending = c.pending[1:]
	} else if err := c.send(wire.Msg{Type: wire.MsgGet, Dataset: dataset, Rel: rel, Offset: offset}); err != nil {
		return nil, err
	}
	var m wire.Msg
	if err := c.recv(&m); err != nil {
		return nil, err
	}
	if m.Type == wire.MsgError {
		return nil, &source.FileError{Rel: rel, Msg: m.Error}
	}
	if m.Type != wire.MsgFile {
		return nil, fmt.Errorf("protocole : message inattendu %q", m.Type)
	}
	var e source.Entry
	if err := json.Unmarshal(m.Body, &e); err != nil {
		return nil, err
	}
	if m.Offset != offset {
		return nil, fmt.Errorf("protocole : reprise à %d demandée, %d accordée", offset, m.Offset)
	}
	c.cur = &stream{r: c.br, info: e}
	return c.cur, nil
}

func (c *Client) Close() error {
	if c.settle(0) == nil {
		c.send(wire.Msg{Type: wire.MsgBye})
		c.bw.Flush()
	}
	return c.RW.Close()
}

type stream struct {
	r    io.Reader
	info source.Entry
	buf  []byte
	end  *wire.Msg
	err  error
}

func (s *stream) Info() source.Entry { return s.info }

func (s *stream) Read(p []byte) (int, error) {
	for len(s.buf) == 0 {
		if s.end != nil || s.err != nil {
			return 0, io.EOF
		}
		typ, b, err := wire.Read(s.r)
		if err != nil {
			s.err = err
			return 0, err
		}
		if typ == wire.FrameData {
			s.buf = b
			continue
		}
		var m wire.Msg
		if err := json.Unmarshal(b, &m); err != nil {
			s.err = err
			return 0, err
		}
		s.end = &m
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}

// Finish lit jusqu'au message de fin et renvoie l'empreinte annoncée.
func (s *stream) Finish() (string, error) {
	if _, err := io.Copy(io.Discard, s); err != nil {
		return "", err
	}
	if s.err != nil {
		return "", s.err
	}
	if s.end == nil {
		return "", io.ErrUnexpectedEOF
	}
	if s.end.Type == wire.MsgError {
		return "", &source.FileError{Rel: s.info.Rel, Msg: s.end.Error}
	}
	if s.end.Type != wire.MsgDone || s.end.Hash == "" {
		return "", fmt.Errorf("protocole : fin de fichier invalide")
	}
	return s.end.Hash, nil
}

// Close ne coupe pas la session : un flux doit être lu jusqu'au bout (Finish)
// pour que le protocole reste synchronisé.
func (s *stream) Close() error { return nil }
