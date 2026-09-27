// Package linux inventorie une machine source Linux de la famille Debian.
//
// Règle absolue : ce package ne fait que LIRE. Il n'ouvre aucun fichier en
// écriture, ne lance aucune commande qui modifie le système et ne suit pas
// les liens symboliques lors des parcours.
package linux

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/bernard-linux/bernard/internal/hardware"
	"github.com/bernard-linux/bernard/internal/inventory"
	"github.com/bernard-linux/bernard/internal/settings"
)

// Plage des UID des comptes humains sur Debian et dérivés.
const (
	minHumanUID = 1000
	maxHumanUID = 59999
)

// parseOSRelease lit /etc/os-release (format clé=valeur).
func parseOSRelease(r io.Reader) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[k] = strings.Trim(v, `"'`)
	}
	return out
}

func readSource(root string) inventory.Source {
	src := inventory.Source{OS: "linux", Snapshot: "none"}
	if f, err := os.Open(filepath.Join(root, "etc/os-release")); err == nil {
		defer f.Close()
		kv := parseOSRelease(f)
		src.Distro = kv["ID"]
		src.Version = kv["VERSION_ID"]
	}
	src.Hostname, _ = os.Hostname()
	src.Desktop = settings.DetectDesktop(root)
	src.Keyboard = hardware.Keyboard(root)
	src.GPUs = hardware.GPUs(root)
	return src
}

func normalizeDesktop(v string) string { return settings.NormalizeDesktop(v) }

// parseGroups lit /etc/group et renvoie, pour chaque login, ses groupes
// secondaires triés.
func parseGroups(r io.Reader) map[string][]string {
	out := map[string][]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Split(sc.Text(), ":")
		if len(f) < 4 || f[3] == "" {
			continue
		}
		for _, member := range strings.Split(f[3], ",") {
			out[member] = append(out[member], f[0])
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// parsePasswd renvoie les comptes humains de /etc/passwd.
func parsePasswd(r io.Reader, groups map[string][]string) []inventory.User {
	var users []inventory.User
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Split(sc.Text(), ":")
		if len(f) < 7 {
			continue
		}
		uid, err := strconv.Atoi(f[2])
		if err != nil || uid < minHumanUID || uid > maxHumanUID {
			continue
		}
		shell := f[6]
		if strings.HasSuffix(shell, "nologin") || strings.HasSuffix(shell, "/false") {
			continue
		}
		fullName, _, _ := strings.Cut(f[4], ",")
		users = append(users, inventory.User{
			ID:       "u" + strconv.Itoa(len(users)+1),
			Login:    f[0],
			FullName: fullName,
			UID:      uid,
			Home:     f[5],
			Shell:    shell,
			Groups:   groups[f[0]],
		})
	}
	return users
}

func readUsers(root string) ([]inventory.User, error) {
	groups := map[string][]string{}
	if gf, err := os.Open(filepath.Join(root, "etc/group")); err == nil {
		groups = parseGroups(gf)
		gf.Close()
	}
	pf, err := os.Open(filepath.Join(root, "etc/passwd"))
	if err != nil {
		return nil, err
	}
	defer pf.Close()
	return parsePasswd(pf, groups), nil
}
