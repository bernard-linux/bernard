// Package engine exécute, sur la cible, la copie des jeux de données depuis
// une source (agent réseau ou paquet sur disque externe).
//
// Chaque étape est journalisée et synchronisée avant la suivante. Après une
// coupure, relancer CopyDataSet avec le même journal reprend exactement où
// l'on s'était arrêté : fichiers terminés sautés, fichier en cours repris au
// dernier point synchronisé.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/journal"
	"github.com/bernard-linux/bernard/internal/source"
	"github.com/bernard-linux/bernard/internal/transfer"
)

// DefaultSyncEvery est l'intervalle entre deux points de reprise.
const DefaultSyncEvery = 8 << 20

// Progress est transmis après chaque élément traité.
type Progress struct {
	Dataset string
	Rel     string
	Files   int64 // éléments traités dans ce jeu de données
	Total   int64 // éléments listés
	Bytes   int64 // octets reçus pendant cette session
}

// Receiver copie depuis une source en journalisant.
type Receiver struct {
	Src        source.Source
	Journal    *journal.Journal
	State      *journal.State // état relu au démarrage (reprise)
	SyncEvery  int64
	OnProgress func(Progress)

	bytes    int64
	curIndex int
	curTotal int
	lastTick time.Time
}

// fatal indique une erreur qui doit interrompre la session (reprise plus
// tard), par opposition à une erreur limitée à un fichier.
func fatal(err error) bool {
	var fe *source.FileError
	if errors.As(err, &fe) {
		return false
	}
	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		return false // problème local sur un fichier précis
	}
	return !errors.Is(err, transfer.ErrVerifyFailed) && !errors.Is(err, transfer.ErrTooManyNames)
}

// CopyDataSet copie un jeu de données dans dstRoot. Une erreur renvoyée
// signifie que la session s'est interrompue ; le rapport indique ce qui a été
// fait jusque-là.
func (r *Receiver) CopyDataSet(ctx context.Context, ds inventory.DataSet, dstRoot string) (*transfer.TreeReport, error) {
	return r.CopyDataSetAs(ctx, ds, dstRoot, nil)
}

// CopyDataSetAs copie comme CopyDataSet, en attribuant chaque élément créé à
// owner (mode administrateur, copie dans /home/<compte>). Chaque écriture est
// précédée d'une vérification des dossiers parents (pas de lien piégé).
func (r *Receiver) CopyDataSetAs(ctx context.Context, ds inventory.DataSet, dstRoot string, owner *transfer.Owner) (*transfer.TreeReport, error) {
	if r.SyncEvery <= 0 {
		r.SyncEvery = DefaultSyncEvery
	}
	rep := &transfer.TreeReport{}
	if created, err := transfer.EnsureDir(dstRoot, 0o755); err != nil {
		return rep, err
	} else if created {
		if err := owner.Apply(dstRoot); err != nil {
			return rep, err
		}
		if err := r.Journal.Append(journal.Record{T: journal.RecDir, Dst: dstRoot}); err != nil {
			return rep, err
		}
	}

	var entries []source.Entry
	err := r.Src.List(ctx, ds.ID, func(e source.Entry) error {
		if e.Kind == source.KindUnreadable {
			rep.Errors = append(rep.Errors, transfer.FileError{Path: e.Rel, Err: e.Link})
			return nil
		}
		entries = append(entries, e)
		return nil
	})
	if err != nil {
		return rep, err
	}

	fail := func(rel string, err error) error {
		if fatal(err) {
			return err
		}
		rep.Errors = append(rep.Errors, transfer.FileError{Path: rel, Err: err.Error()})
		r.Journal.Append(journal.Record{T: journal.RecError, Key: ds.ID + "/" + rel, Error: err.Error()})
		return nil
	}

	for i, e := range entries {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		r.curIndex, r.curTotal = i, len(entries)
		key := ds.ID + "/" + e.Rel
		dst := filepath.Join(dstRoot, filepath.FromSlash(e.Rel))
		if owner != nil {
			if err := transfer.SafeParents(dstRoot, dst); err != nil {
				if err := fail(e.Rel, err); err != nil {
					return rep, err
				}
				continue
			}
		}

		if done, ok := r.State.Done[key]; ok && sameSource(done, e) {
			if _, err := os.Lstat(done.Dst); err == nil {
				rep.AlreadyPresent++
				r.progress(ds.ID, e.Rel, i, len(entries))
				continue
			}
		}

		switch e.Kind {
		case source.KindDir:
			created, err := transfer.EnsureDir(dst, e.Mode.Perm())
			if err == nil && created {
				err = owner.Apply(dst)
			}
			if err != nil {
				if err := fail(e.Rel, err); err != nil {
					return rep, err
				}
				continue
			}
			if created {
				if err := r.Journal.Append(journal.Record{T: journal.RecDir, Dst: dst}); err != nil {
					return rep, err
				}
			}
		case source.KindSymlink:
			res, err := transfer.PlaceSymlink(e.Link, dst)
			if err == nil && res.Status != transfer.StatusAlreadyPresent {
				err = owner.Apply(res.Dst)
			}
			if err != nil {
				if err := fail(e.Rel, err); err != nil {
					return rep, err
				}
				continue
			}
			if err := r.done(key, res, e); err != nil {
				return rep, err
			}
			tally(rep, res)
		case source.KindFile:
			res, err := r.receiveFile(ctx, ds.ID, e, dst, key, owner)
			if err != nil {
				if err := fail(e.Rel, err); err != nil {
					return rep, err
				}
				continue
			}
			tally(rep, res)
		default:
			rep.Skipped = append(rep.Skipped, e.Rel)
		}
		r.progress(ds.ID, e.Rel, i, len(entries))
	}
	return rep, nil
}

func (r *Receiver) progress(ds, rel string, i, total int) {
	if r.OnProgress != nil {
		r.OnProgress(Progress{Dataset: ds, Rel: rel, Files: int64(i + 1), Total: int64(total), Bytes: r.bytes})
	}
}

func tally(rep *transfer.TreeReport, res transfer.FileResult) {
	switch res.Status {
	case transfer.StatusAlreadyPresent:
		rep.AlreadyPresent++
	case transfer.StatusRenamed:
		rep.Renamed = append(rep.Renamed, res)
		fallthrough
	default:
		rep.Files++
		rep.Bytes += res.Bytes
	}
}

func (r *Receiver) done(key string, res transfer.FileResult, e source.Entry) error {
	return r.Journal.Append(journal.Record{
		T: journal.RecDone, Key: key, Dst: res.Dst, Hash: res.Hash, Status: string(res.Status),
		Size: res.Bytes, SrcMTime: e.MTime,
	})
}

// sameSource indique que l'élément source n'a pas changé depuis sa copie.
// S'il a changé (reprise après modification sur l'ancienne machine), il est
// recopié ; l'ancienne copie n'est pas écrasée.
func sameSource(done journal.Record, e source.Entry) bool {
	if e.Kind == source.KindSymlink {
		return done.Hash == "symlink:"+e.Link
	}
	return done.Size == e.Size && done.SrcMTime.Equal(e.MTime)
}

// receiveFile reçoit un fichier, en reprenant au dernier point synchronisé.
// Si la vérification finale échoue, le fichier est recommencé une fois
// depuis le début.
func (r *Receiver) receiveFile(ctx context.Context, ds string, e source.Entry, dst, key string, owner *transfer.Owner) (transfer.FileResult, error) {
	part := transfer.PartPath(filepath.Dir(dst), key)
	var offset int64
	if p, ok := r.State.Progress[key]; ok && p.Part == part {
		offset = p.Offset
	}
	for attempt := 0; ; attempt++ {
		res, err := r.receiveOnce(ctx, ds, e.Rel, dst, key, part, offset)
		if err == nil && res.Status != transfer.StatusAlreadyPresent {
			err = owner.Apply(res.Dst)
		}
		if err == nil {
			return res, r.done(key, res, e)
		}
		if errors.Is(err, transfer.ErrVerifyFailed) && attempt == 0 {
			os.Remove(part)
			offset = 0
			continue
		}
		if !fatal(err) {
			os.Remove(part) // erreur définitive sur ce fichier : pas de reste
		}
		return res, err
	}
}

func (r *Receiver) receiveOnce(ctx context.Context, ds, rel, dst, key, part string, offset int64) (transfer.FileResult, error) {
	f, off, err := transfer.OpenPart(part, offset)
	if err != nil {
		return transfer.FileResult{}, err
	}
	closed := false
	defer func() {
		if !closed {
			f.Close()
		}
	}()
	if err := r.Journal.Append(journal.Record{T: journal.RecProgress, Key: key, Part: part, Offset: off}); err != nil {
		return transfer.FileResult{}, err
	}
	st, err := r.Src.Get(ctx, ds, rel, off)
	if err != nil {
		return transfer.FileResult{}, err
	}
	defer st.Close()

	buf := make([]byte, 1<<20)
	written, sinceSync := off, int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return transfer.FileResult{}, err
		}
		n, rerr := st.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				st.Finish() // garder le protocole synchronisé
				return transfer.FileResult{}, werr
			}
			written += int64(n)
			sinceSync += int64(n)
			r.bytes += int64(n)
			// Avancement pendant un gros fichier, au plus 4 fois par seconde.
			if r.OnProgress != nil && time.Since(r.lastTick) > 250*time.Millisecond {
				r.lastTick = time.Now()
				r.OnProgress(Progress{Dataset: ds, Rel: rel, Files: int64(r.curIndex), Total: int64(r.curTotal), Bytes: r.bytes})
			}
			if sinceSync >= r.SyncEvery {
				if err := f.Sync(); err != nil {
					st.Finish()
					return transfer.FileResult{}, err
				}
				if err := r.Journal.Append(journal.Record{T: journal.RecProgress, Key: key, Part: part, Offset: written}); err != nil {
					return transfer.FileResult{}, err
				}
				sinceSync = 0
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return transfer.FileResult{}, rerr
		}
	}
	hash, err := st.Finish()
	if err != nil {
		return transfer.FileResult{}, err
	}
	if err := f.Sync(); err != nil {
		return transfer.FileResult{}, err
	}
	closed = true
	if err := f.Close(); err != nil {
		return transfer.FileResult{}, err
	}
	info := st.Info()
	res, err := transfer.CommitPart(part, dst, hash, info.Size, info.Mode, info.MTime)
	res.Src = rel
	return res, err
}

// UndoReport résume une annulation.
type UndoReport struct {
	Removed     int      `json:"removed"`
	Kept        []string `json:"kept,omitempty"` // modifiés depuis la copie : conservés
	DirsRemoved int      `json:"dirsRemoved"`
	Parts       int      `json:"parts"`
}

// Undo supprime uniquement ce que Bernard a créé et qui n'a pas été modifié
// depuis. Un fichier déjà présent avant la migration n'est jamais touché : il
// n'apparaît pas dans le journal comme créé.
func Undo(journalPath string) (*UndoReport, error) {
	st, err := journal.Load(journalPath)
	if err != nil {
		return nil, err
	}
	rep := &UndoReport{}
	for _, d := range st.Done {
		if d.Status != string(transfer.StatusCopied) && d.Status != string(transfer.StatusRenamed) {
			continue
		}
		if !unchanged(d) {
			if _, err := os.Lstat(d.Dst); err == nil {
				rep.Kept = append(rep.Kept, d.Dst)
			}
			continue
		}
		if err := os.Remove(d.Dst); err == nil {
			rep.Removed++
		}
	}
	for _, p := range st.Progress {
		if os.Remove(p.Part) == nil {
			rep.Parts++
		}
	}
	for i := len(st.Dirs) - 1; i >= 0; i-- {
		if os.Remove(st.Dirs[i]) == nil { // échoue si non vide : conservé
			rep.DirsRemoved++
		}
	}
	return rep, nil
}

func unchanged(d journal.Record) bool {
	if len(d.Hash) > 8 && d.Hash[:8] == "symlink:" {
		target, err := os.Readlink(d.Dst)
		return err == nil && "symlink:"+target == d.Hash
	}
	h, _, err := transfer.HashFile(d.Dst)
	return err == nil && h == d.Hash
}

// Begin ouvre (ou reprend) un journal pour un inventaire donné.
func Begin(journalPath, inventoryDigest string) (*journal.Journal, *journal.State, error) {
	st, err := journal.Load(journalPath)
	if err != nil {
		return nil, nil, err
	}
	if st.Inventory != "" && st.Inventory != inventoryDigest {
		return nil, nil, fmt.Errorf("ce journal appartient à une autre migration (inventaire différent)")
	}
	j, err := journal.Open(journalPath)
	if err != nil {
		return nil, nil, err
	}
	if err := j.Append(journal.Record{T: journal.RecBegin, Inventory: inventoryDigest}); err != nil {
		j.Close()
		return nil, nil, err
	}
	return j, st, nil
}
