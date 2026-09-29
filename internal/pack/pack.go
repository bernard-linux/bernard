// Package pack écrit et lit un paquet de migration sur disque externe.
//
// Format bernard-pack/1, dans un dossier :
//
//	bernard-pack.json   en-tête en clair : dérivation de clé, sel, état
//	manifest.bin        manifeste chiffré : inventaire, liste des éléments,
//	                    empreintes, droits, dates, liens
//	data-000001.bin …   blocs chiffrés ; chaque fichier fait au plus
//	                    ObjectLimit octets (1 Go par défaut, compatible FAT32)
//
// Chiffrement AES-256-GCM, clé dérivée de la phrase de passe (PBKDF2-SHA256).
// Chaque bloc est authentifié avec sa position (fichier, rang, dernier bloc) :
// un bloc modifié, déplacé ou manquant est détecté. Les noms de fichiers
// eux-mêmes sont dans le manifeste chiffré.
package pack

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/zeebo/blake3"

	"github.com/bernard-linux/bernard/internal/i18n"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/source"
)

// Format identifie la version du paquet.
const Format = "bernard-pack/1"

const (
	headerName         = "bernard-pack.json"
	manifestName       = "manifest.bin"
	chunkSize          = 1 << 20
	DefaultObjectLimit = 1 << 30
	// DefaultIterations suit la recommandation OWASP pour PBKDF2-SHA256.
	DefaultIterations = 600_000
	maxIterations     = 10_000_000
	checkText         = "bernard-pack-ok"
)

// Erreurs.
var (
	ErrBadPassphrase = i18n.NewError("phrase de passe incorrecte")
	ErrIncomplete    = i18n.NewError("paquet incomplet : son écriture a été interrompue, il faut le recréer")
	ErrCorrupt       = i18n.NewError("paquet altéré : un bloc chiffré ne correspond pas")
)

// msgChanged : fichier modifié pendant sa lecture (relu). Gardé en français
// (clé) dans FileError.Msg et dans le paquet ; traduit à l'affichage.
var msgChanged = i18n.N("modifié pendant la lecture")

type header struct {
	Format     string `json:"format"`
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       []byte `json:"salt"`
	Check      []byte `json:"check"`
	Complete   bool   `json:"complete"`
	Objects    int    `json:"objects"`
}

type entry struct {
	source.Entry
	Hash   string `json:"hash,omitempty"`
	Index  uint64 `json:"index,omitempty"` // numéro du fichier, lié aux blocs
	Obj    int    `json:"obj,omitempty"`   // fichier data du premier bloc
	Off    int64  `json:"off,omitempty"`   // position du premier bloc
	Chunks uint64 `json:"chunks,omitempty"`
	Error  string `json:"error,omitempty"`
}

type manifest struct {
	Inventory *inventory.Inventory `json:"inventory"`
	Entries   map[string][]entry   `json:"entries"` // par jeu de données
	Secrets   map[string]string    `json:"secrets,omitempty"`
	Extras    json.RawMessage      `json:"extras,omitempty"`
}

// pbkdf2 dérive la clé (RFC 8018, un seul bloc de 32 octets).
func pbkdf2(pass, salt []byte, iter int) []byte {
	prf := hmac.New(sha256.New, pass)
	prf.Write(salt)
	prf.Write([]byte{0, 0, 0, 1})
	u := prf.Sum(nil)
	out := append([]byte(nil), u...)
	for i := 1; i < iter; i++ {
		prf.Reset()
		prf.Write(u)
		u = prf.Sum(u[:0])
		for j := range out {
			out[j] ^= u[j]
		}
	}
	return out
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func seal(aead cipher.AEAD, plain, aad []byte) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plain, aad), nil
}

func open(aead cipher.AEAD, sealed, aad []byte) ([]byte, error) {
	ns := aead.NonceSize()
	if len(sealed) < ns {
		return nil, ErrCorrupt
	}
	plain, err := aead.Open(nil, sealed[:ns], sealed[ns:], aad)
	if err != nil {
		return nil, ErrCorrupt
	}
	return plain, nil
}

func chunkAAD(index, chunk uint64, final bool) []byte {
	b := []byte(Format)
	b = binary.BigEndian.AppendUint64(b, index)
	b = binary.BigEndian.AppendUint64(b, chunk)
	if final {
		return append(b, 1)
	}
	return append(b, 0)
}

func objName(n int) string { return fmt.Sprintf("data-%06d.bin", n) }

// ---------------------------------------------------------------- écriture

// WriteOptions paramètre l'écriture.
type WriteOptions struct {
	ObjectLimit int64 // 0 = DefaultObjectLimit
	Iterations  int   // 0 = DefaultIterations
	// Secrets et Extras (mots de passe, réglages, Wi-Fi) sont stockés dans le
	// manifeste chiffré, s'ils sont fournis.
	Secrets map[string]string
	Extras  any
	OnFile  func(rel string, size int64)
	// Prepare arrête le service d'un jeu de données pendant son écriture
	// (voir remote.Server.Prepare).
	Prepare func(ds inventory.DataSet) (func(), error)
}

// WriteReport résume l'écriture.
type WriteReport struct {
	Files  int64
	Bytes  int64
	Errors []string
}

type objWriter struct {
	dir   string
	limit int64
	n     int
	f     *os.File
	size  int64
}

func (w *objWriter) record(sealed []byte) (int, int64, error) {
	recLen := int64(4 + len(sealed))
	if w.f == nil || (w.size > 0 && w.size+recLen > w.limit) {
		if err := w.rotate(); err != nil {
			return 0, 0, err
		}
	}
	off := w.size
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(sealed)))
	if _, err := w.f.Write(hdr[:]); err != nil {
		return 0, 0, err
	}
	if _, err := w.f.Write(sealed); err != nil {
		return 0, 0, err
	}
	w.size += recLen
	return w.n, off, nil
}

func (w *objWriter) rotate() error {
	if err := w.close(); err != nil {
		return err
	}
	w.n++
	f, err := os.OpenFile(filepath.Join(w.dir, objName(w.n)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w.f, w.size = f, 0
	return nil
}

func (w *objWriter) close() error {
	if w.f == nil {
		return nil
	}
	if err := w.f.Sync(); err != nil {
		return err
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// Write crée le paquet dans dir, qui ne doit pas exister ou être vide.
// La source est lue sans être modifiée.
func Write(ctx context.Context, inv *inventory.Inventory, dir, passphrase string, opt WriteOptions) (*WriteReport, error) {
	if len(passphrase) < 8 {
		return nil, errors.New(i18n.T("la phrase de passe doit faire au moins 8 caractères"))
	}
	if opt.ObjectLimit <= 0 {
		opt.ObjectLimit = DefaultObjectLimit
	}
	if opt.Iterations <= 0 {
		opt.Iterations = DefaultIterations
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if items, _ := os.ReadDir(dir); len(items) > 0 {
		return nil, i18n.Errorf("%s n'est pas vide : choisissez un dossier vide", dir)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	aead, err := newAEAD(pbkdf2([]byte(passphrase), salt, opt.Iterations))
	if err != nil {
		return nil, err
	}
	check, err := seal(aead, []byte(checkText), []byte("check"))
	if err != nil {
		return nil, err
	}
	hdr := header{Format: Format, KDF: "pbkdf2-sha256", Iterations: opt.Iterations, Salt: salt, Check: check}
	if err := writeHeader(dir, hdr); err != nil {
		return nil, err
	}

	ow := &objWriter{dir: dir, limit: opt.ObjectLimit}
	rep := &WriteReport{}
	man := manifest{Inventory: inv, Entries: map[string][]entry{}, Secrets: opt.Secrets}
	if opt.Extras != nil {
		if b, err := json.Marshal(opt.Extras); err == nil {
			man.Extras = b
		}
	}
	var index uint64
	buf := make([]byte, chunkSize)

	for _, ds := range inv.DataSets {
		var list []entry
		release := func() {}
		if ds.Service != "" && opt.Prepare != nil {
			rel, err := opt.Prepare(ds)
			if err != nil {
				rep.Errors = append(rep.Errors, ds.Dest+" : "+err.Error())
				man.Entries[ds.ID] = nil
				continue
			}
			release = rel
		}
		walkErr := source.WalkDataSet(ds.Path, ds.Excluded, ds.Include, func(e source.Entry) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if e.Kind != source.KindFile || e.Same != "" {
				list = append(list, entry{Entry: e}) // lien dur : pas de contenu
				return nil
			}
			index++
			en, err := writeFile(ow, aead, ds.Path, e, index, buf)
			if err != nil {
				var fe *source.FileError
				if !errors.As(err, &fe) {
					return err // écriture sur le disque externe impossible
				}
				en = entry{Entry: e, Error: fe.Msg}
				rep.Errors = append(rep.Errors, e.Rel+" : "+fe.Msg)
			} else {
				rep.Files++
				rep.Bytes += en.Size
				if opt.OnFile != nil {
					opt.OnFile(e.Rel, en.Size)
				}
			}
			list = append(list, en)
			return nil
		}, func(rel string, err error) {
			list = append(list, entry{Entry: source.Entry{Rel: rel, Kind: source.KindUnreadable}, Error: err.Error()})
			rep.Errors = append(rep.Errors, rel+" : "+err.Error())
		})
		release()
		if walkErr != nil {
			ow.close()
			return rep, walkErr
		}
		man.Entries[ds.ID] = list
	}
	if err := ow.close(); err != nil {
		return rep, err
	}

	mb, err := json.Marshal(man)
	if err != nil {
		return rep, err
	}
	sealed, err := seal(aead, mb, []byte("manifest"))
	if err != nil {
		return rep, err
	}
	if err := writeSynced(filepath.Join(dir, manifestName), sealed); err != nil {
		return rep, err
	}
	hdr.Complete, hdr.Objects = true, ow.n
	return rep, writeHeader(dir, hdr)
}

// writeFile chiffre un fichier bloc par bloc. S'il change pendant la lecture,
// il est relu (3 essais) ; les blocs abandonnés restent inutilisés.
func writeFile(ow *objWriter, aead cipher.AEAD, root string, e source.Entry, index uint64, buf []byte) (entry, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		en, err := writeFileOnce(ow, aead, root, e, index, buf)
		if err == nil {
			return en, nil
		}
		lastErr = err
		var fe *source.FileError
		if !errors.As(err, &fe) || fe.Msg != msgChanged {
			return en, err
		}
	}
	return entry{}, lastErr
}

func writeFileOnce(ow *objWriter, aead cipher.AEAD, root string, e source.Entry, index uint64, buf []byte) (entry, error) {
	f, info, err := source.OpenRegular(root, e.Rel)
	if err != nil {
		return entry{}, &source.FileError{Rel: e.Rel, Msg: err.Error()}
	}
	defer f.Close()
	en := entry{Entry: e, Index: index}
	en.Size, en.Mode, en.MTime = info.Size(), info.Mode(), info.ModTime()
	h := blake3.New()
	var chunk uint64
	var total int64
	for {
		n, rerr := io.ReadFull(f, buf)
		if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
			return en, &source.FileError{Rel: e.Rel, Msg: rerr.Error()}
		}
		total += int64(n)
		final := total >= info.Size() || rerr != nil
		if final && rerr == nil {
			// Taille atteinte : vérifier qu'il ne reste rien (fichier grossi).
			var probe [1]byte
			if m, _ := f.Read(probe[:]); m > 0 {
				return en, &source.FileError{Rel: e.Rel, Msg: msgChanged}
			}
		}
		h.Write(buf[:n])
		sealed, err := seal(aead, buf[:n], chunkAAD(index, chunk, final))
		if err != nil {
			return en, err
		}
		obj, off, err := ow.record(sealed)
		if err != nil {
			return en, err
		}
		if chunk == 0 {
			en.Obj, en.Off = obj, off
		}
		chunk++
		if final {
			break
		}
	}
	after, err := f.Stat()
	if err != nil || total != info.Size() || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return en, &source.FileError{Rel: e.Rel, Msg: msgChanged}
	}
	en.Chunks = chunk
	en.Hash = "blake3:" + hex.EncodeToString(h.Sum(nil))
	return en, nil
}

func writeSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// writeHeader remplace l'en-tête de façon atomique.
func writeHeader(dir string, h header) error {
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, headerName+".tmp")
	if err := writeSynced(tmp, b); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, headerName)); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// ---------------------------------------------------------------- lecture

// Pack est un paquet ouvert en lecture ; il implémente source.Source.
type Pack struct {
	dir  string
	aead cipher.AEAD
	man  manifest
	idx  map[string]map[string]*entry
}

// Open ouvre un paquet complet et vérifie la phrase de passe.
func Open(dir, passphrase string) (*Pack, error) {
	b, err := os.ReadFile(filepath.Join(dir, headerName))
	if err != nil {
		return nil, i18n.Errorf("pas de paquet Bernard dans %s : %w", dir, err)
	}
	var h header
	if err := json.Unmarshal(b, &h); err != nil {
		return nil, err
	}
	if h.Format != Format {
		return nil, i18n.Errorf("format de paquet non pris en charge : %q", h.Format)
	}
	if !h.Complete {
		return nil, ErrIncomplete
	}
	if h.Iterations <= 0 || h.Iterations > maxIterations {
		return nil, errors.New(i18n.T("en-tête de paquet invalide"))
	}
	aead, err := newAEAD(pbkdf2([]byte(passphrase), h.Salt, h.Iterations))
	if err != nil {
		return nil, err
	}
	if p, err := open(aead, h.Check, []byte("check")); err != nil || string(p) != checkText {
		return nil, ErrBadPassphrase
	}
	sealed, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return nil, ErrIncomplete
	}
	mb, err := open(aead, sealed, []byte("manifest"))
	if err != nil {
		return nil, err
	}
	p := &Pack{dir: dir, aead: aead, idx: map[string]map[string]*entry{}}
	if err := json.Unmarshal(mb, &p.man); err != nil {
		return nil, err
	}
	for ds, list := range p.man.Entries {
		m := map[string]*entry{}
		for i := range list {
			m[list[i].Rel] = &list[i]
		}
		p.idx[ds] = m
	}
	return p, nil
}

func (p *Pack) Inventory(context.Context) (*inventory.Inventory, error) {
	return p.man.Inventory, p.man.Inventory.Validate()
}

func (p *Pack) List(_ context.Context, ds string, fn func(source.Entry) error) error {
	list, ok := p.man.Entries[ds]
	if !ok {
		return &source.FileError{Msg: i18n.Tf("jeu de données absent du paquet : %s", ds)}
	}
	for _, e := range list {
		se := e.Entry
		if e.Error != "" {
			se = source.Entry{Rel: e.Rel, Kind: source.KindUnreadable, Link: i18n.T(e.Error)}
		}
		if err := fn(se); err != nil {
			return err
		}
	}
	return nil
}

func (p *Pack) Get(_ context.Context, ds, rel string, offset int64) (source.FileStream, error) {
	e, ok := p.idx[ds][rel]
	if !ok || e.Kind != source.KindFile || e.Error != "" {
		return nil, &source.FileError{Rel: rel, Msg: i18n.T("absent du paquet")}
	}
	f, err := os.Open(filepath.Join(p.dir, objName(e.Obj)))
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(e.Off, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	if offset < 0 || offset > e.Size {
		offset = 0
	}
	return &packStream{p: p, e: e, f: f, obj: e.Obj, skip: offset}, nil
}

func (p *Pack) Close() error { return nil }

// Secrets renvoie les hachages de mots de passe stockés dans le paquet.
func (p *Pack) Secrets(context.Context) (map[string]string, error) {
	if p.man.Secrets == nil {
		return nil, errors.New(i18n.T("aucun mot de passe dans le paquet"))
	}
	return p.man.Secrets, nil
}

// Extras renvoie les réglages stockés dans le paquet.
func (p *Pack) Extras(_ context.Context, out any) error {
	if len(p.man.Extras) == 0 {
		return errors.New(i18n.T("aucun réglage dans le paquet"))
	}
	return json.Unmarshal(p.man.Extras, out)
}

type packStream struct {
	p     *Pack
	e     *entry
	f     *os.File
	obj   int
	chunk uint64
	skip  int64
	buf   []byte
	done  bool
	err   error
}

func (s *packStream) Info() source.Entry { return s.e.Entry }

// next lit et déchiffre le bloc suivant, en passant au fichier data suivant
// si nécessaire.
func (s *packStream) next() error {
	var hdr [4]byte
	for {
		_, err := io.ReadFull(s.f, hdr[:])
		if err == io.EOF {
			s.f.Close()
			s.obj++
			if s.f, err = os.Open(filepath.Join(s.p.dir, objName(s.obj))); err != nil {
				return i18n.Errorf("%w : fichier %s manquant", ErrCorrupt, objName(s.obj))
			}
			continue
		}
		if err != nil {
			return ErrCorrupt
		}
		break
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > chunkSize+64 {
		return ErrCorrupt
	}
	sealed := make([]byte, n)
	if _, err := io.ReadFull(s.f, sealed); err != nil {
		return ErrCorrupt
	}
	final := s.chunk == s.e.Chunks-1
	plain, err := open(s.p.aead, sealed, chunkAAD(s.e.Index, s.chunk, final))
	if err != nil {
		// Bloc altéré : ce fichier est perdu, les autres restent lisibles.
		return &source.FileError{Rel: s.e.Rel, Msg: ErrCorrupt.Error()}
	}
	s.chunk++
	s.done = final
	s.buf = plain
	return nil
}

func (s *packStream) Read(b []byte) (int, error) {
	for len(s.buf) == 0 || s.skip > 0 {
		if s.skip > 0 && len(s.buf) > 0 {
			k := min(int64(len(s.buf)), s.skip)
			s.buf, s.skip = s.buf[k:], s.skip-k
			continue
		}
		if s.done {
			return 0, io.EOF
		}
		if s.err != nil {
			return 0, s.err
		}
		if s.err = s.next(); s.err != nil {
			return 0, s.err
		}
	}
	n := copy(b, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}

func (s *packStream) Finish() (string, error) {
	if _, err := io.Copy(io.Discard, s); err != nil {
		return "", err
	}
	return s.e.Hash, nil
}

func (s *packStream) Close() error {
	if s.f != nil {
		return s.f.Close()
	}
	return nil
}
