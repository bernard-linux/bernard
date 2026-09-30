package linux

import (
	"io/fs"
	"path/filepath"

	"github.com/bernard-linux/bernard/internal/source"
)

// DefaultExcludes sont les chemins, relatifs au dossier personnel, exclus par
// défaut : caches et corbeilles, régénérables ou jetables, ainsi que les
// caches et verrous des navigateurs (voir browserExcludes). L'utilisateur
// pourra les réintégrer dans l'interface.
var DefaultExcludes = append([]string{
	".cache",      // dont l'index des polices (fontconfig), propre à la machine
	".fontconfig", // ancien emplacement de l'index des polices
	".nv",         // cache OpenGL des pilotes NVIDIA, propre à la carte
	".local/share/Trash",
	".thumbnails",
	".var/app/*/cache",
	"snap/*/*/.cache",
}, browserExcludes()...)

// Dossiers de profil des navigateurs de la famille Chromium (Brave, Chrome,
// Chromium, Edge, Vivaldi, Opera), installés en paquet, Flatpak ou Snap.
var chromiumRoots = []string{
	".config/BraveSoftware/Brave-Browser",
	".config/BraveSoftware/Brave-Browser-Beta",
	".config/google-chrome",
	".config/google-chrome-beta",
	".config/chromium",
	".config/microsoft-edge",
	".config/vivaldi",
	".config/opera",
	".var/app/com.brave.Browser/config/BraveSoftware/Brave-Browser",
	".var/app/com.google.Chrome/config/google-chrome",
	".var/app/org.chromium.Chromium/config/chromium",
	".var/app/com.microsoft.Edge/config/microsoft-edge",
	".var/app/com.vivaldi.Vivaldi/config/vivaldi",
	"snap/chromium/common/chromium",
	"snap/brave/*/.config/BraveSoftware/Brave-Browser",
}

// Dossiers contenant les profils Firefox et Thunderbird (un sous-dossier
// par profil).
var mozillaRoots = []string{
	".mozilla/firefox",
	".var/app/org.mozilla.firefox/.mozilla/firefox",
	"snap/firefox/common/.mozilla/firefox",
	".thunderbird",
	".var/app/org.mozilla.Thunderbird/.thunderbird",
}

// browserExcludes écarte ce qui, dans un profil de navigateur, est lié à
// l'ancienne machine plutôt qu'à l'utilisateur :
//
//   - les verrous (SingletonLock de Chromium, lock et .parentlock de
//     Firefox) désignent l'ancien ordinateur par son nom : copiés, ils font
//     croire au navigateur qu'il tourne déjà ailleurs, et il refuse de
//     démarrer ;
//   - les caches graphiques (GPUCache, ShaderCache…) sont compilés pour la
//     carte graphique de l'ancien ordinateur : sur une autre carte (Intel
//     vers NVIDIA, par exemple), ils peuvent faire planter le démarrage ;
//   - les autres caches se reconstruisent seuls.
//
// Favoris, historique, mots de passe, extensions, cookies et réglages sont
// conservés.
func browserExcludes() []string {
	var out []string
	for _, r := range chromiumRoots {
		for _, p := range []string{
			"Singleton*", "GrShaderCache", "GraphiteDawnCache", "ShaderCache",
			"*/GPUCache", "*/Cache", "*/Code Cache", "*/DawnCache", "*/DawnGraphiteCache",
			"*/DawnWebGPUCache", "*/Service Worker/CacheStorage", "*/Service Worker/ScriptCache",
		} {
			out = append(out, r+"/"+p)
		}
	}
	for _, r := range mozillaRoots {
		for _, p := range []string{"*/lock", "*/.parentlock", "*/startupCache", "*/shader-cache", "*/cache2"} {
			out = append(out, r+"/"+p)
		}
	}
	return out
}

// excluded délègue au motif partagé avec le transfert.
func excluded(rel string, patterns []string) bool { return source.Excluded(rel, patterns) }

// measureStats résume un parcours de dossier.
type measureStats struct {
	Files      int64
	Bytes      int64
	Unreadable int64 // entrées illisibles (droits), signalées dans le rapport
}

// measure parcourt root sans suivre les liens symboliques et compte les
// fichiers réguliers et liens. Les fichiers spéciaux (sockets, FIFO,
// périphériques) sont ignorés : ils ne se migrent pas.
func measure(root string, patterns []string) (measureStats, error) {
	var st measureStats
	seen := linkSeen{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			st.Unreadable++
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if excluded(filepath.ToSlash(rel), patterns) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		switch {
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				st.Unreadable++
				return nil
			}
			st.Files++
			if !seen.again(info) {
				st.Bytes += info.Size()
			}
		case d.Type()&fs.ModeSymlink != 0:
			st.Files++
		}
		return nil
	})
	return st, err
}
