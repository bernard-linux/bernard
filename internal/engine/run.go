package engine

import (
	"context"
	"os"
	"path/filepath"

	"github.com/bernard-linux/bernard/internal/journal"
	"github.com/bernard-linux/bernard/internal/source"
	"github.com/bernard-linux/bernard/internal/transfer"
)

// Summary est le bilan d'une session de copie.
type Summary struct {
	Reports  map[string]*transfer.TreeReport `json:"reports"` // par jeu de données
	Complete bool                            `json:"complete"`
}

// OK indique que tout a été copié sans erreur.
func (s *Summary) OK() bool {
	for _, r := range s.Reports {
		if !r.OK() {
			return false
		}
	}
	return s.Complete
}

// RunAll copie tous les jeux de données de la source dans destRoot/<login>.
// Mode de développement de l'étape 3 : la copie vers /home avec création des
// comptes arrive avec l'assistant privilégié (étape 4).
func RunAll(ctx context.Context, src source.Source, destRoot, journalPath string, onProgress func(Progress)) (*Summary, error) {
	inv, err := src.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o700); err != nil {
		return nil, err
	}
	j, st, err := Begin(journalPath, inv.Identity())
	if err != nil {
		return nil, err
	}
	defer j.Close()
	logins := map[string]string{}
	for _, u := range inv.Users {
		logins[u.ID] = u.Login
	}
	sum := &Summary{Reports: map[string]*transfer.TreeReport{}}
	r := &Receiver{Src: src, Journal: j, State: st, OnProgress: onProgress}
	for _, ds := range inv.DataSets {
		rep, err := r.CopyDataSet(ctx, ds, filepath.Join(destRoot, logins[ds.User]))
		sum.Reports[ds.ID] = rep
		if err != nil {
			return sum, err
		}
	}
	sum.Complete = true
	return sum, j.Append(journal.Record{T: journal.RecFinish})
}
