// Package engine exécute, sur la cible, la copie des jeux de données depuis
// une source (agent réseau ou paquet sur disque externe).
//
// Chaque étape est journalisée. Après une coupure, relancer CopyDataSet avec
// le même journal reprend exactement où l'on s'était arrêté : fichiers
// terminés sautés, gros fichier en cours repris au dernier point synchronisé.
//
// Petits fichiers (moins de 1 Mo) : ils sont demandés d'avance à l'agent,
// écrits sans attendre le disque, puis validés par lots — un seul passage
// sur le disque (syncfs) avant de les ranger, un autre après, puis le
// journal. Aucun fichier n'est rangé sous son nom final avant d'être sur le
// disque et vérifié ; une coupure fait seulement recevoir à nouveau le lot en
// cours.
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

// Réglages des lots de petits fichiers et des demandes d'avance.
const (
	SmallFile     = 1 << 20         // taille sous laquelle un fichier va dans un lot
	batchFiles    = 256             // fichiers par lot au plus
	batchBytes    = 32 << 20        // octets par lot au plus
	batchAge      = 2 * time.Second // âge maximal d'un lot
	aheadFiles    = 64              // demandes d'avance au plus
	aheadBytes    = 16 << 20        // octets demandés d'avance au plus
	aheadReqBytes = 32 << 10        // taille cumulée des demandes (voir prefetch)
)

// Prefetcher est une source qui accepte des demandes d'avance (agent réseau).
type Prefetcher interface {
	Prefetch(dataset, rel string, offset int64) error
	Flush() error
}

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

	// OwnerFor, s'il est fixé, donne le propriétaire de chaque élément
	// (données hors dossiers personnels : mysql, www-data, root…) à la place
	// du propriétaire unique passé à CopyDataSetAs.
	OwnerFor func(source.Entry) *transfer.Owner
	// Replace, s'il est fixé, est appelé avant d'écrire un fichier ou un lien
	// là où un élément existe déjà : il le met de côté (pour l'annulation),
	// au lieu que la copie reçoive un nom de conflit. Sert pour /etc, /opt…,
	// où le fichier doit prendre la place de celui de la cible.
	Replace func(dst string) error

	bytes    int64
	curIndex int
	curTotal int
	lastTick time.Time

	// Lot de petits fichiers reçus, pas encore rangés.
	batch      []pending
	batchSize  int64
	batchSince time.Time

	// Demandes d'avance : prochain élément à examiner, et éléments demandés
	// dont la réponse n'est pas encore lue.
	aheadNext  int
	ahead      map[int]int64 // index → octets
	aheadBytes int64
	aheadReq   int
	// linked : premiers noms des fichiers à plusieurs noms, rangés dans le
	// jeu de données en cours (clé → résultat), pour recréer les liens durs.
	linked map[string]transfer.FileResult
}

// pending est un petit fichier reçu, en attente de validation par lot.
type pending struct {
	key, part, dst, hash string
	e                    source.Entry
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
	// La cible peut restreindre encore la liste (ex. fichiers de /etc
	// propres à la machine, refusés même si l'ancien ordinateur les envoie).
	only := source.NewIncluder(ds.Include)
	err := r.Src.List(ctx, ds.ID, func(e source.Entry) error {
		if e.Kind == source.KindDir && !only.Dir(e.Rel) || e.Kind != source.KindDir && e.Kind != source.KindUnreadable && !only.File(e.Rel) {
			return nil
		}
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
		r.Journal.Write(journal.Record{T: journal.RecError, Key: ds.ID + "/" + rel, Error: err.Error()})
		return nil
	}
	r.batch, r.batchSize = nil, 0
	r.linked = map[string]transfer.FileResult{}
	r.aheadNext, r.ahead, r.aheadBytes, r.aheadReq = 0, map[int]int64{}, 0, 0
	pf, _ := r.Src.(Prefetcher)

	for i, e := range entries {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		if pf != nil {
			if err := r.prefetch(pf, ds, entries, i, dstRoot); err != nil {
				return rep, err
			}
		}
		r.curIndex, r.curTotal = i, len(entries)
		key := ds.ID + "/" + e.Rel
		dst := filepath.Join(dstRoot, filepath.FromSlash(e.Rel))
		err := r.handle(ctx, ds, e, i, len(entries), key, dst, dstRoot, owner, rep, fail)
		if n, ok := r.ahead[i]; ok { // réponse lue ou à jeter : plus en attente
			delete(r.ahead, i)
			r.aheadBytes -= n
			r.aheadReq -= len(e.Rel) + 100
		}
		if err != nil {
			return rep, err
		}
		if len(r.batch) > 0 && (len(r.batch) >= batchFiles || r.batchSize >= batchBytes || time.Since(r.batchSince) > batchAge) {
			if err := r.flush(ctx, ds.ID, dstRoot, owner, rep, fail, len(entries)); err != nil {
				return rep, err
			}
		}
	}
	if err := r.flush(ctx, ds.ID, dstRoot, owner, rep, fail, len(entries)); err != nil {
		return rep, err
	}
	return rep, r.Journal.Sync()
}

// isDone indique qu'un élément a déjà été copié lors d'une session
// précédente (reprise), et n'a pas changé depuis.
func (r *Receiver) isDone(key string, e source.Entry) bool {
	if done, ok := r.State.Done[key]; ok && sameSource(done, e) {
		if _, err := os.Lstat(done.Dst); err == nil {
			return true
		}
	}
	return false
}

// small indique qu'un fichier passe par les lots : petit, et pas de reprise
// en cours à un décalage (sinon, chemin classique).
func (r *Receiver) small(key string, e source.Entry) bool {
	if e.Kind != source.KindFile || e.Size >= SmallFile || e.Same != "" {
		return false
	}
	p, ok := r.State.Progress[key]
	return !ok || p.Offset == 0
}

// prefetch demande d'avance les petits fichiers qui suivent i, dans la
// limite de la fenêtre. Il ne dépasse jamais un gros fichier : les réponses
// arrivent ainsi dans l'ordre où le moteur les lit. La taille cumulée des
// demandes reste petite pour ne jamais remplir le tampon réseau de l'agent
// (qui pourrait sinon bloquer les deux côtés).
func (r *Receiver) prefetch(pf Prefetcher, ds inventory.DataSet, entries []source.Entry, i int, dstRoot string) error {
	if r.aheadNext < i {
		r.aheadNext = i
	}
	sent := false
	for r.aheadNext < len(entries) && len(r.ahead) < aheadFiles && r.aheadBytes < aheadBytes && r.aheadReq < aheadReqBytes {
		e := entries[r.aheadNext]
		key := ds.ID + "/" + e.Rel
		if e.Kind == source.KindFile && e.Same != "" {
			r.aheadNext++ // lien dur : rien à demander
			continue
		}
		if e.Kind == source.KindFile && !r.small(key, e) && !r.isDone(key, e) {
			break // gros fichier : pas de demande au-delà
		}
		if e.Kind == source.KindFile && !r.isDone(key, e) {
			if err := pf.Prefetch(ds.ID, e.Rel, 0); err != nil {
				return err
			}
			r.ahead[r.aheadNext] = e.Size
			r.aheadBytes += e.Size
			r.aheadReq += len(e.Rel) + 100
			sent = true
		}
		r.aheadNext++
	}
	if sent {
		return pf.Flush()
	}
	return nil
}

func (r *Receiver) ownerOf(e source.Entry, def *transfer.Owner) *transfer.Owner {
	if r.OwnerFor != nil {
		return r.OwnerFor(e)
	}
	return def
}

// makeRoom met de côté un élément existant à la place de dst (mode Replace).
func (r *Receiver) makeRoom(dst string) error {
	if r.Replace == nil {
		return nil
	}
	fi, err := os.Lstat(dst)
	if err != nil || fi.IsDir() {
		return nil
	}
	return r.Replace(dst)
}

// handle traite un élément de la liste.
func (r *Receiver) handle(ctx context.Context, ds inventory.DataSet, e source.Entry, i, total int, key, dst, dstRoot string, owner *transfer.Owner, rep *transfer.TreeReport, fail func(string, error) error) error {
	{
		if owner != nil || r.OwnerFor != nil {
			if err := transfer.SafeParents(dstRoot, dst); err != nil {
				return fail(e.Rel, err)
			}
		}
		if r.isDone(key, e) {
			rep.AlreadyPresent++
			r.progress(ds.ID, e.Rel, i, total)
			return nil
		}

		switch e.Kind {
		case source.KindDir:
			// Journal écrit sans attendre : il devient durable au prochain lot.
			// Perdu dans une coupure, le dossier serait seulement conservé
			// lors d'une annulation.
			created, err := transfer.EnsureDir(dst, e.Mode.Perm())
			if err == nil && created {
				err = r.ownerOf(e, owner).Apply(dst)
			}
			if err != nil {
				return fail(e.Rel, err)
			}
			if created {
				if err := r.Journal.Write(journal.Record{T: journal.RecDir, Dst: dst}); err != nil {
					return err
				}
			}
		case source.KindSymlink:
			if l, err := os.Readlink(dst); err != nil || l != e.Link {
				if err := r.makeRoom(dst); err != nil {
					return fail(e.Rel, err)
				}
			}
			res, err := transfer.PlaceSymlink(e.Link, dst)
			if err == nil && res.Status != transfer.StatusAlreadyPresent {
				err = r.ownerOf(e, owner).Apply(res.Dst)
			}
			if err != nil {
				return fail(e.Rel, err)
			}
			if err := r.doneLater(key, res, e); err != nil {
				return err
			}
			tally(rep, res)
		case source.KindFile:
			if e.Same != "" {
				ok, err := r.hardLink(ctx, ds.ID, e, key, dst, dstRoot, owner, rep, fail, total)
				if err != nil {
					return err
				}
				if ok {
					r.progress(ds.ID, e.Rel, i, total)
					return nil
				}
				// Premier nom absent de la cible : contenu copié normalement.
			}
			if err := r.makeRoom(dst); err != nil {
				return fail(e.Rel, err)
			}
			if r.small(key, e) {
				if err := r.receiveSmall(ctx, ds.ID, e, dst, key); err != nil {
					return fail(e.Rel, err)
				}
				return nil // compté et signalé au rangement du lot
			}
			// Gros fichier : le lot en cours est d'abord rangé.
			if err := r.flush(ctx, ds.ID, dstRoot, owner, rep, fail, total); err != nil {
				return err
			}
			res, err := r.receiveFile(ctx, ds.ID, e, dst, key, r.ownerOf(e, owner))
			if err != nil {
				return fail(e.Rel, err)
			}
			tally(rep, res)
		case source.KindWhiteout:
			if err := r.makeRoom(dst); err != nil {
				return fail(e.Rel, err)
			}
			res, err := transfer.PlaceWhiteout(dst)
			if err == nil && res.Status != transfer.StatusAlreadyPresent {
				err = r.ownerOf(e, owner).Apply(res.Dst)
			}
			if err != nil {
				return fail(e.Rel, err)
			}
			if err := r.doneLater(key, res, e); err != nil {
				return err
			}
			tally(rep, res)
		default:
			rep.Skipped = append(rep.Skipped, e.Rel)
		}
		r.progress(ds.ID, e.Rel, i, total)
	}
	return nil
}

// noteLinked retient où a été rangé le premier nom d'un fichier à
// plusieurs noms.
func (r *Receiver) noteLinked(key string, e source.Entry, res transfer.FileResult) {
	if e.Nlink > 1 && e.Same == "" && r.linked != nil {
		r.linked[key] = res
	}
}

// placedAs renvoie où et avec quelle empreinte un élément de ce jeu de
// données a été rangé (cette session ou une précédente).
func (r *Receiver) placedAs(key string) (transfer.FileResult, bool) {
	if res, ok := r.linked[key]; ok {
		return res, true
	}
	if d, ok := r.State.Done[key]; ok && d.Hash != "" {
		return transfer.FileResult{Dst: d.Dst, Hash: d.Hash}, true
	}
	return transfer.FileResult{}, false
}

// hardLink recrée un lien dur vers le premier nom du fichier. Renvoie faux
// si ce premier nom n'a pas été rangé (erreur, exclusion) : le fichier est
// alors copié comme les autres.
func (r *Receiver) hardLink(ctx context.Context, dsID string, e source.Entry, key, dst, dstRoot string, owner *transfer.Owner, rep *transfer.TreeReport, fail func(string, error) error, total int) (bool, error) {
	firstKey := dsID + "/" + e.Same
	first, ok := r.placedAs(firstKey)
	if !ok && len(r.batch) > 0 {
		// Premier nom peut-être dans le lot en cours : on le range d'abord.
		if err := r.flush(ctx, dsID, dstRoot, owner, rep, fail, total); err != nil {
			return false, err
		}
		first, ok = r.placedAs(firstKey)
	}
	if !ok {
		return false, nil
	}
	fi1, err := os.Lstat(first.Dst)
	if err != nil || !fi1.Mode().IsRegular() {
		return false, nil
	}
	res := transfer.FileResult{Src: e.Rel, Dst: dst, Hash: first.Hash, Bytes: e.Size, Status: transfer.StatusCopied}
	if fi2, err := os.Lstat(dst); err == nil {
		if os.SameFile(fi1, fi2) {
			res.Status = transfer.StatusAlreadyPresent
		} else if err := r.makeRoom(dst); err != nil {
			return true, fail(e.Rel, err)
		} else if _, err := os.Lstat(dst); err == nil {
			return false, nil // un autre fichier porte ce nom : copie classique (renommée)
		}
	}
	if res.Status == transfer.StatusCopied {
		if err := os.Link(first.Dst, dst); err != nil {
			return false, nil // autre disque, système de fichiers sans liens durs…
		}
	}
	if err := r.doneLater(key, res, e); err != nil {
		return true, err
	}
	if res.Status == transfer.StatusAlreadyPresent {
		rep.AlreadyPresent++
	} else {
		rep.Files++
		rep.Links++
	}
	return true, nil
}

// receiveSmall reçoit un petit fichier dans son fichier temporaire, sans
// attendre le disque ; il sera vérifié et rangé avec son lot.
func (r *Receiver) receiveSmall(ctx context.Context, ds string, e source.Entry, dst, key string) error {
	part := transfer.PartPath(filepath.Dir(dst), key)
	f, _, err := transfer.OpenPart(part, 0)
	if err != nil {
		return err
	}
	// Le fichier temporaire est consigné pour l'annulation ; le journal est
	// synchronisé avant les données du lot. Si une coupure survient avant,
	// la reprise réutilise ce même fichier temporaire (nom stable).
	if err := r.Journal.Write(journal.Record{T: journal.RecProgress, Key: key, Part: part}); err != nil {
		f.Close()
		return err
	}
	st, err := r.Src.Get(ctx, ds, e.Rel, 0)
	if err != nil {
		f.Close()
		os.Remove(part)
		return err
	}
	n, err := io.Copy(f, st)
	if err != nil {
		f.Close()
		os.Remove(part)
		return err
	}
	r.bytes += n
	hash, err := st.Finish()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(part)
		return err
	}
	info := st.Info()
	info.Rel, info.User, info.Group, info.Xattrs, info.Nlink = e.Rel, e.User, e.Group, e.Xattrs, e.Nlink
	if len(r.batch) == 0 {
		r.batchSince = time.Now()
	}
	r.batch = append(r.batch, pending{key: key, part: part, dst: dst, hash: hash, e: info})
	r.batchSize += n
	return nil
}

// flush range le lot : données sur le disque, vérification, nom final,
// puis journal.
func (r *Receiver) flush(ctx context.Context, dsID, dstRoot string, owner *transfer.Owner, rep *transfer.TreeReport, fail func(string, error) error, total int) error {
	if len(r.batch) == 0 {
		return nil
	}
	batch := r.batch
	r.batch, r.batchSize = nil, 0
	if err := r.Journal.Sync(); err != nil {
		return err
	}
	parts := make([]string, len(batch))
	for i, p := range batch {
		parts[i] = p.part
	}
	if err := transfer.SyncFS(dstRoot, parts); err != nil {
		return err
	}
	var placed []transfer.FileResult
	var ok []pending
	for _, p := range batch {
		res, err := transfer.CommitPartNoSync(p.part, p.dst, p.hash, p.e.Size, p.e.Mode, p.e.MTime)
		if err == nil && res.Status != transfer.StatusAlreadyPresent {
			err = r.ownerOf(p.e, owner).Apply(res.Dst)
		}
		if errors.Is(err, transfer.ErrVerifyFailed) {
			// Contenu écrit différent de la source : fichier redemandé une
			// fois, par le chemin classique (synchronisé, vérifié).
			os.Remove(p.part)
			res, err = r.receiveFile(ctx, dsID, p.e, p.dst, p.key, r.ownerOf(p.e, owner))
			if err == nil {
				tally(rep, res)
				continue
			}
		}
		if err != nil {
			os.Remove(p.part)
			if ferr := fail(p.e.Rel, err); ferr != nil {
				return ferr
			}
			continue
		}
		res.Src = p.e.Rel
		placed = append(placed, res)
		ok = append(ok, p)
	}
	var files []string
	for _, res := range placed {
		files = append(files, res.Dst)
	}
	if err := transfer.SyncFS(dstRoot, files); err != nil {
		return err
	}
	for i, res := range placed {
		if err := r.doneLater(ok[i].key, res, ok[i].e); err != nil {
			return err
		}
		r.noteLinked(ok[i].key, ok[i].e, res)
		tally(rep, res)
	}
	if err := r.Journal.Sync(); err != nil {
		return err
	}
	if len(ok) > 0 {
		r.progress(dsID, ok[len(ok)-1].e.Rel, r.curIndex, total)
	}
	return nil
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

func doneRecord(key string, res transfer.FileResult, e source.Entry) journal.Record {
	return journal.Record{
		T: journal.RecDone, Key: key, Dst: res.Dst, Hash: res.Hash, Status: string(res.Status),
		Size: res.Bytes, SrcMTime: e.MTime,
	}
}

// done consigne un élément terminé et attend le disque.
func (r *Receiver) done(key string, res transfer.FileResult, e source.Entry) error {
	return r.Journal.Append(doneRecord(key, res, e))
}

// doneLater consigne un élément terminé ; durable au prochain Sync.
func (r *Receiver) doneLater(key string, res transfer.FileResult, e source.Entry) error {
	return r.Journal.Write(doneRecord(key, res, e))
}

// sameSource indique que l'élément source n'a pas changé depuis sa copie.
// S'il a changé (reprise après modification sur l'ancienne machine), il est
// recopié ; l'ancienne copie n'est pas écrasée.
func sameSource(done journal.Record, e source.Entry) bool {
	if e.Kind == source.KindWhiteout {
		return done.Hash == transfer.WhiteoutHash
	}
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
			r.noteLinked(key, e, res)
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
			if werr := transfer.WriteSparse(f, buf[:n]); werr != nil {
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
	// Un trou final (fichier creux) n'a rien écrit : la taille est fixée ici.
	if err := f.Truncate(written); err != nil {
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
	if d.Hash == transfer.WhiteoutHash {
		fi, err := os.Lstat(d.Dst)
		return err == nil && fi.Mode()&os.ModeCharDevice != 0
	}
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
