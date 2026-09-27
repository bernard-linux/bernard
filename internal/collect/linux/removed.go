package linux

import (
	"bufio"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// allPackages lit la liste de tous les paquets installés dans la base de
// dpkg (fonctionne aussi pour un système monté ailleurs).
func allPackages(root string) []string {
	var out []string
	eachStanza(filepath.Join(root, "var/lib/dpkg/status"), func(f map[string]string) {
		if name := f["Package"]; name != "" && strings.HasSuffix(f["Status"], " installed") {
			out = append(out, name)
		}
	})
	sort.Strings(out)
	return out
}

// removedPackages lit les journaux de dpkg (dpkg.log, dpkg.log.1,
// dpkg.log.2.gz…) et renvoie les paquets retirés et toujours absents, avec
// la date du dernier retrait. installed permet d'écarter un paquet retiré
// puis réinstallé.
func removedPackages(root string, installed []string) map[string]string {
	have := map[string]bool{}
	for _, p := range installed {
		have[p] = true
	}
	files, _ := filepath.Glob(filepath.Join(root, "var/log/dpkg.log*"))
	out := map[string]string{}
	for _, f := range files {
		readDpkgLog(f, func(date, action, pkg string) {
			if action != "remove" && action != "purge" {
				return
			}
			pkg, _, _ = strings.Cut(pkg, ":") // « rhythmbox:amd64 »
			if !have[pkg] && date > out[pkg] {
				out[pkg] = date
			}
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// readDpkgLog appelle fn pour chaque ligne « AAAA-MM-JJ hh:mm:ss action paquet … ».
func readDpkgLog(path string, fn func(date, action, pkg string)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return
		}
		defer gz.Close()
		r = gz
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) >= 4 {
			fn(fl[0], fl[2], fl[3])
		}
	}
}
