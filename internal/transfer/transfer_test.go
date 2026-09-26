package transfer

import (
	"crypto/rand"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
)

// Noms volontairement pénibles : accents, emoji, espaces, tiret initial,
// retour à la ligne, collisions de casse, fichiers cachés.
var trickyNames = []string{
	"rapport été 2026.odt",
	"😀 photo.jpg",
	"fin avec espace ",
	"-commence-par-un-tiret",
	"ligne\nretour.txt",
	"Rapport.pdf",
	"rapport.pdf",
	".bashrc",
	"sans-extension",
	"très/profond/dossier/imbriqué/fichier.txt",
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
}

func randomBytes(t *testing.T, n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// snapshot relève empreinte, taille, droits et date de chaque élément, pour
// prouver que la source n'a pas bougé.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		info, _ := os.Lstat(p)
		sig := info.Mode().String() + "|" + info.ModTime().String()
		if info.Mode().IsRegular() {
			h, _, err := HashFile(p)
			if err != nil {
				t.Fatal(err)
			}
			sig += "|" + h
		}
		out[p] = sig
		return nil
	})
	return out
}

func buildSource(t *testing.T) string {
	src := t.TempDir()
	for i, name := range trickyNames {
		writeFile(t, filepath.Join(src, name), []byte(strings.Repeat("données ", i+1)))
	}
	writeFile(t, filepath.Join(src, "vide.txt"), nil)
	writeFile(t, filepath.Join(src, "gros.bin"), randomBytes(t, 5<<20))
	writeFile(t, filepath.Join(src, ".cache/jetable.bin"), randomBytes(t, 1024))
	if err := os.Symlink("rapport été 2026.odt", filepath.Join(src, "lien-vers-rapport")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/cible/inexistante", filepath.Join(src, "lien-casse")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(src, "tube"), 0o600); err != nil {
		t.Fatal(err)
	}
	return src
}

func TestCopyTreeCopiesEverythingAndLeavesSourceUntouched(t *testing.T) {
	src := buildSource(t)
	dst := filepath.Join(t.TempDir(), "cible")
	before := snapshot(t, src)

	rep, err := CopyTree(src, dst, TreeOptions{
		Exclude: func(rel string) bool { return rel == ".cache" || strings.HasPrefix(rel, ".cache/") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatalf("erreurs inattendues : %+v", rep.Errors)
	}

	// 1. La source n'a pas changé d'un octet, ni de droits, ni de date.
	after := snapshot(t, src)
	for p, sig := range before {
		if after[p] != sig {
			t.Errorf("la source a été modifiée : %s", p)
		}
	}

	// 2. Chaque fichier ordinaire est identique, droits et date compris.
	for _, name := range append(trickyNames, "vide.txt", "gros.bin") {
		s, d := filepath.Join(src, name), filepath.Join(dst, name)
		hs, _, _ := HashFile(s)
		hd, _, err := HashFile(d)
		if err != nil || hs != hd {
			t.Errorf("%q absent ou différent sur la cible", name)
			continue
		}
		si, _ := os.Stat(s)
		di, _ := os.Stat(d)
		if si.Mode() != di.Mode() || !si.ModTime().Equal(di.ModTime()) {
			t.Errorf("%q : droits ou date non conservés", name)
		}
	}

	// 3. Les liens sont recréés tels quels, même cassés.
	for _, l := range []string{"lien-vers-rapport", "lien-casse"} {
		want, _ := os.Readlink(filepath.Join(src, l))
		got, err := os.Readlink(filepath.Join(dst, l))
		if err != nil || got != want {
			t.Errorf("lien %s mal recréé : %q", l, got)
		}
	}

	// 4. Le tube est signalé, l'exclusion aussi, rien ne disparaît en silence.
	if len(rep.Skipped) != 1 || rep.Skipped[0] != "tube" {
		t.Errorf("le tube devait être signalé comme ignoré : %v", rep.Skipped)
	}
	if len(rep.Excluded) != 1 || rep.Excluded[0] != ".cache" {
		t.Errorf("exclusion non signalée : %v", rep.Excluded)
	}
	if _, err := os.Lstat(filepath.Join(dst, ".cache")); err == nil {
		t.Error("le dossier exclu a été copié")
	}

	// 5. Aucun fichier temporaire ne traîne.
	filepath.WalkDir(dst, func(p string, d fs.DirEntry, _ error) error {
		if strings.HasSuffix(p, ".part") {
			t.Errorf("fichier temporaire oublié : %s", p)
		}
		return nil
	})

	wantFiles := int64(len(trickyNames) + 2 + 2) // + vide, gros, + 2 liens
	if rep.Files != wantFiles {
		t.Errorf("fichiers comptés : %d, attendu %d", rep.Files, wantFiles)
	}
}

func TestExistingFileIsNeverOverwritten(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(src, "notes.txt"), []byte("version de l'ancien PC"))
	writeFile(t, filepath.Join(dst, "notes.txt"), []byte("version déjà sur le nouveau PC"))

	res, err := CopyFile(filepath.Join(src, "notes.txt"), filepath.Join(dst, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusRenamed || filepath.Base(res.Dst) != "notes (bernard 1).txt" {
		t.Fatalf("attendu un nom de conflit, obtenu %+v", res)
	}
	kept, _ := os.ReadFile(filepath.Join(dst, "notes.txt"))
	if string(kept) != "version déjà sur le nouveau PC" {
		t.Fatal("le fichier existant a été écrasé")
	}
	copied, _ := os.ReadFile(res.Dst)
	if string(copied) != "version de l'ancien PC" {
		t.Fatal("contenu copié incorrect")
	}
}

func TestRerunIsIdempotent(t *testing.T) {
	src := buildSource(t)
	dst := filepath.Join(t.TempDir(), "cible")
	if _, err := CopyTree(src, dst, TreeOptions{}); err != nil {
		t.Fatal(err)
	}
	// Une seconde passe (reprise après coupure) ne doit rien dupliquer.
	rep, err := CopyTree(src, dst, TreeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Files != 0 || len(rep.Renamed) != 0 {
		t.Errorf("la reprise a dupliqué des fichiers : %d copiés, %d renommés", rep.Files, len(rep.Renamed))
	}
	var names []string
	filepath.WalkDir(dst, func(p string, d fs.DirEntry, _ error) error {
		if strings.Contains(p, "(bernard") {
			names = append(names, p)
		}
		return nil
	})
	sort.Strings(names)
	if len(names) > 0 {
		t.Errorf("doublons créés : %v", names)
	}
}

func TestConflictName(t *testing.T) {
	cases := map[string]string{
		"/a/notes.txt":    "/a/notes (bernard 2).txt",
		"/a/.bashrc":      "/a/.bashrc (bernard 2)",
		"/a/archive":      "/a/archive (bernard 2)",
		"/a/photo.tar.gz": "/a/photo.tar (bernard 2).gz",
	}
	for in, want := range cases {
		if got := conflictName(in, 2); got != want {
			t.Errorf("conflictName(%q) = %q, attendu %q", in, got, want)
		}
	}
}

func TestDestinationFileBlockingADirectoryIsReported(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(src, "Photos/vacances.jpg"), []byte("jpg"))
	writeFile(t, filepath.Join(dst, "Photos"), []byte("un fichier, pas un dossier"))
	rep, err := CopyTree(src, dst, TreeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() {
		t.Fatal("le conflit dossier/fichier aurait dû être signalé")
	}
	kept, _ := os.ReadFile(filepath.Join(dst, "Photos"))
	if string(kept) != "un fichier, pas un dossier" {
		t.Fatal("le fichier existant a été modifié")
	}
}
