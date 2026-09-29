package linux

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/bernard-linux/bernard/internal/inventory"
)

// Examen de tout le disque, en dehors des dossiers personnels.
//
// Principe : ce qu'un paquet a installé et que personne n'a modifié est
// réinstallé sur le nouvel ordinateur, jamais copié. Tout le reste appartient
// à l'utilisateur ou à un logiciel installé à la main, et doit suivre :
// fichiers qui n'appartiennent à aucun paquet, fichiers de configuration
// modifiés, données des services (sites, bases, conteneurs, machines
// virtuelles), autres disques.
//
// Sont écartés d'office les dossiers du système et ce qui tient à la machine
// (pilotes, amorçage, identité de la machine, caches, journaux).

// dpkgDB décrit ce que les paquets ont installé.
type dpkgDB struct {
	owned     map[string]bool   // chemins installés par un paquet
	conffiles map[string]string // fichier de configuration → empreinte MD5 d'origine
}

func readDpkgDB(root string) dpkgDB {
	db := dpkgDB{owned: map[string]bool{}, conffiles: map[string]string{}}
	lists, _ := filepath.Glob(filepath.Join(root, "var/lib/dpkg/info/*.list"))
	for _, l := range lists {
		f, err := os.Open(l)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			if p := sc.Text(); p != "" {
				db.owned[p] = true
			}
		}
		f.Close()
	}
	// Conffiles : bloc de lignes indentées « /etc/x md5 » dans le statut.
	f, err := os.Open(filepath.Join(root, "var/lib/dpkg/status"))
	if err != nil {
		return db
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	in := false
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.HasPrefix(l, "Conffiles:"):
			in = true
		case in && strings.HasPrefix(l, " "):
			fl := strings.Fields(l)
			if len(fl) >= 2 && fl[1] != "newconffile" {
				db.conffiles[fl[0]] = fl[1]
			}
		default:
			in = false
		}
	}
	return db
}

// Réglages système propres à la machine : jamais repris (identité, disques,
// amorçage, pilotes, réseau déjà traité à part, comptes déjà traités à part).
var etcMachine = []string{
	"machine-id", "hostname", "hosts.allow", "adjtime", "fstab", "crypttab", "mtab", "timezone", "localtime",
	"default/keyboard", "default/grub", "default/grub.d", "grub.d", "kernel", "initramfs-tools", "dkms",
	"modprobe.d", "modules-load.d", "X11/xorg.conf", "X11/xorg.conf.d", "udev", "nvidia",
	"NetworkManager/system-connections", "netplan", "resolv.conf", "cups", "ssh/ssh_host_",
	"passwd", "passwd-", "shadow", "shadow-", "group", "group-", "gshadow", "gshadow-", "subuid", "subgid",
	"subuid-", "subgid-", ".pwd.lock", "ld.so.cache", "alternatives", "ssl/certs", "apparmor.d/cache",
	"os-release", "lsb-release", "issue", "issue.net", "machine-info", "console-setup", "vconsole.conf",
	"cloud", "gdm3/custom.conf", "lightdm", "apt", "brltty", "sane.d", "bluetooth", "fwupd",
	"systemd/system/display-manager.service", "update-motd.d", "papersize", "printcap", "blkid.tab",
	"popularity-contest.conf", "rc", "rc0.d", "rc1.d", "rc2.d", "rc3.d", "rc4.d", "rc5.d", "rc6.d", "rcS.d",
	"python3", "fonts/conf.d", "ca-certificates.conf", "ssl/private/ssl-cert-snakeoil.key",
	"ssl/certs/ssl-cert-snakeoil.pem", "snmp", "mime.types", "mailcap", "xdg/autostart", ".updated",
	// Fichiers régénérés par les paquets eux-mêmes à l'installation.
	"xml", "sgml", "texmf", "dictionaries-common", "pam.d/common-", "shells", "locale.gen", "locale.conf",
	"default/locale", "libreoffice/registry", "dpkg/origins", "ld.so.conf.d", "sensors3.conf", "sensors.d",
}

// EtcMachine indique un fichier de /etc propre à la machine, qui ne doit
// jamais être repris (vérifié aussi côté cible).
func EtcMachine(rel string) bool { return etcIsMachine(rel) }

func etcIsMachine(rel string) bool {
	for _, p := range etcMachine {
		if rel == p || strings.HasPrefix(rel, p+"/") || (strings.HasSuffix(p, "_") && strings.HasPrefix(rel, p)) {
			return true
		}
	}
	return false
}

// Dossiers standard de la racine : examinés zone par zone ou ignorés.
var rootStandard = map[string]bool{
	"bin": true, "boot": true, "dev": true, "etc": true, "home": true, "lib": true, "lib32": true, "lib64": true,
	"libx32": true, "lost+found": true, "media": true, "mnt": true, "opt": true, "proc": true, "root": true,
	"run": true, "sbin": true, "snap": true, "srv": true, "sys": true, "tmp": true, "usr": true, "var": true,
	"cdrom": true, "swapfile": true, "swap.img": true, "efi": true,
}

// /var/lib/<x> propres au système ou déjà traités ailleurs.
var varLibSystem = map[string]bool{
	"apt": true, "dpkg": true, "snapd": true, "flatpak": true, "systemd": true, "NetworkManager": true,
	"bluetooth": true, "upower": true, "polkit-1": true, "AccountsService": true, "gdm3": true, "lightdm": true,
	"ucf": true, "dhcp": true, "PackageKit": true, "fwupd": true, "colord": true, "sudo": true, "usbutils": true,
	"bernard": true, "ubuntu-drivers-common": true, "dbus": true, "boltd": true, "misc": true, "pam": true,
	"python": true, "sgml-base": true, "xml-core": true, "ispell": true, "aspell": true, "dictionaries-common": true,
	"man-db": true, "update-notifier": true, "update-manager": true, "ubuntu-advantage": true, "ubuntu-release-upgrader": true,
	"os-prober": true, "grub": true, "shim-signed": true, "initramfs-tools": true, "dkms": true, "nvidia": true,
	"alsa": true, "pulse": true, "power-profiles-daemon": true, "fprint": true, "geoclue": true, "whoopsie": true,
	"cups": true, "saned": true, "avahi-autoipd": true, "logrotate": true, "plymouth": true, "private": true,
	"command-not-found": true, "app-info": true, "swcatalog": true, "fontconfig": true, "gnome-remote-desktop": true,
	"ghostscript": true, "tpm": true, "openvpn": true, "snmp": true, "vim": true, "emacsen-common": true,
	"os-release": true, "zorin-connect": true, "flatpak-system-helper": true, "hp": true, "texmf": true,
	"lxcfs": true, "sss": true, "realmd": true, "kerneloops": true, "unattended-upgrades": true, "apport": true,
	"wireplumber": true, "pipewire": true, "tlp": true, "thermald": true, "sddm": true, "xkb": true,
}

// Services reconnus par leur dossier de données.
var recognizers = []struct {
	rel, kind, label, service string
}{
	{"var/lib/mysql", inventory.SysDatabase, "Bases MySQL / MariaDB", "mysql"},
	{"var/lib/postgresql", inventory.SysDatabase, "Bases PostgreSQL", "postgresql"},
	{"var/lib/mongodb", inventory.SysDatabase, "Bases MongoDB", "mongod"},
	{"var/lib/redis", inventory.SysDatabase, "Données Redis", "redis-server"},
	{"var/lib/docker", inventory.SysContainer, "Docker : images, conteneurs et volumes", "docker.socket docker"},
	{"var/lib/containers", inventory.SysContainer, "Podman : images et conteneurs", "podman.socket podman"},
	{"var/snap/lxd/common/lxd", inventory.SysContainer, "LXD : conteneurs", "snap.lxd.daemon"},
	{"var/lib/libvirt/images", inventory.SysVM, "Machines virtuelles (libvirt)", "libvirtd"}, // définitions : dans /etc
	{"opt/FileMaker/FileMaker Server/Data", inventory.SysAppServer, "Serveur FileMaker : bases et réglages", "fmshelper"},
	{"var/www", inventory.SysWeb, "Sites web (/var/www)", ""},
}

// linkSeen repère les fichiers à plusieurs noms (liens durs), comptés une
// seule fois : Bernard ne les copie qu'une fois (sauvegardes Timeshift ou
// rsnapshot, où la même photo figure dans chaque instantané).
type linkSeen map[[2]uint64]bool

func (s linkSeen) again(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Nlink < 2 {
		return false
	}
	k := [2]uint64{uint64(st.Dev), uint64(st.Ino)}
	if s[k] {
		return true
	}
	s[k] = true
	return false
}

// usage mesure un dossier : fichiers, taille apparente, place occupée.
type usage struct{ files, bytes, used int64 }

func measureTree(root string, skip func(rel string, d fs.DirEntry) bool) usage {
	var u usage
	seen := linkSeen{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if skip != nil && p != root && skip(filepath.ToSlash(rel), d) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				u.files++
				if seen.again(fi) {
					return nil // autre nom d'un fichier déjà compté (lien dur)
				}
				u.bytes += fi.Size()
				if st, ok := fi.Sys().(*syscall.Stat_t); ok {
					u.used += st.Blocks * 512
				} else {
					u.used += fi.Size()
				}
			}
		} else if d.Type()&fs.ModeSymlink != 0 {
			u.files++
		}
		return nil
	})
	return u
}

// scanner examine un système (monté sur root).
type scanner struct {
	root     string
	db       dpkgDB
	items    []inventory.SystemItem
	datasets []inventory.DataSet
	claimed  map[string]bool // chemins déjà rattachés à un élément
}

// Genres copiés depuis la version 0.5. Les bases, conteneurs, machines
// virtuelles et serveurs d'application demandent l'arrêt du service pendant
// la copie (version 0.6) ; les autres disques, un choix d'emplacement.
var copyable = map[string]bool{
	inventory.SysEtc: true, inventory.SysOpt: true, inventory.SysSrv: true, inventory.SysLocal: true,
	inventory.SysWeb: true, inventory.SysCustom: true, inventory.SysRoot: true, inventory.SysService: true,
	// Depuis la 0.6 : service arrêté pendant la copie.
	inventory.SysDatabase: true, inventory.SysContainer: true, inventory.SysVM: true, inventory.SysAppServer: true,
	// Depuis la 0.6.1 : autres disques, copiés ailleurs ou rattachés tels quels.
	inventory.SysDisk: true, inventory.SysHomeElse: true, inventory.SysSteam: true, inventory.SysBackup: true,
}

// Copyable indique si un genre de données est copié par cette version.
func Copyable(kind string) bool { return copyable[kind] }

func (s *scanner) abs(rel string) string { return "/" + strings.TrimPrefix(filepath.ToSlash(rel), "/") }

func (s *scanner) exists(rel string) bool {
	_, err := os.Lstat(filepath.Join(s.root, rel))
	return err == nil
}

func (s *scanner) add(it inventory.SystemItem) {
	s.addWith(it, nil)
}

// addWith ajoute un élément ; include limite sa copie à ces chemins
// (relatifs à son dossier), nil pour tout copier.
func (s *scanner) addWith(it inventory.SystemItem, include []string) {
	if it.Files == 0 && it.Bytes == 0 && it.Kind != inventory.SysDisk && it.Kind != inventory.SysBackup {
		return
	}
	if it.Advice == "" {
		it.Advice = inventory.AdviceCopy
	}
	it.ID = "s" + strconv.Itoa(len(s.items)+1)
	s.items = append(s.items, it)
	for _, p := range it.Paths {
		s.claimed[strings.TrimPrefix(p, "/")] = true
	}
	if copyable[it.Kind] && len(it.Paths) == 1 {
		s.datasets = append(s.datasets, inventory.DataSet{
			ID: "x" + strconv.Itoa(len(s.datasets)+1), Kind: "system", System: it.ID,
			Path: filepath.Join(s.root, it.Paths[0]), Dest: it.Paths[0],
			Files: it.Files, SizeBytes: it.Bytes, Include: include, Service: it.Service,
		})
	}
}

// measureUnowned mesure un dossier sans les fichiers installés par les
// paquets, et renvoie la liste des fichiers restants quand il y en avait
// (nil : aucun fichier de paquet, tout est à copier).
func (s *scanner) measureUnowned(rel string) (usage, []string) {
	var u usage
	var list []string
	sawOwned := false
	base := filepath.Join(s.root, rel)
	filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == base {
			if err != nil && d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		r, _ := filepath.Rel(base, p)
		r = filepath.ToSlash(r)
		full := s.abs(filepath.Join(rel, r))
		if s.claimed[strings.TrimPrefix(full, "/")] {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if s.db.owned[full] {
			sawOwned = true
			return nil
		}
		if !(d.Type().IsRegular() || d.Type()&fs.ModeSymlink != 0) {
			return nil
		}
		u.files++
		list = append(list, r)
		if fi, err := d.Info(); err == nil && d.Type().IsRegular() {
			u.bytes += fi.Size()
			if st, ok := fi.Sys().(*syscall.Stat_t); ok {
				u.used += st.Blocks * 512
			} else {
				u.used += fi.Size()
			}
		}
		return nil
	})
	if !sawOwned {
		list = nil
	}
	return u, list
}

// unownedSkip ignore ce que les paquets ont installé sous base (relatif à la
// racine) : seuls restent les fichiers ajoutés à la main.
func (s *scanner) unownedSkip(base string) func(string, fs.DirEntry) bool {
	return func(rel string, d fs.DirEntry) bool {
		full := s.abs(filepath.Join(base, rel))
		if s.claimed[strings.TrimPrefix(full, "/")] {
			return true
		}
		return !d.IsDir() && s.db.owned[full]
	}
}

func (s *scanner) measure(rel string, skip func(string, fs.DirEntry) bool) usage {
	return measureTree(filepath.Join(s.root, rel), skip)
}

// recognized ajoute les services reconnus (bases, conteneurs, VM, web…).
func (s *scanner) recognized() {
	for _, r := range recognizers {
		if !s.exists(r.rel) {
			continue
		}
		u := s.measure(r.rel, nil)
		s.add(inventory.SystemItem{Kind: r.kind, Label: r.label, Paths: []string{s.abs(r.rel)},
			Files: u.files, Bytes: u.bytes, Used: u.used, Service: r.service})
	}
}

// etc relève les réglages système modifiés ou ajoutés.
func (s *scanner) etc() {
	var detail, include []string
	var u usage
	base := filepath.Join(s.root, "etc")
	filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == base {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, base+"/"))
		if etcIsMachine(rel) || s.claimed["etc/"+rel] {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !(d.Type().IsRegular() || d.Type()&fs.ModeSymlink != 0) {
			return nil
		}
		full := "/etc/" + rel
		changed := false
		if sum, ok := s.db.conffiles[full]; ok {
			changed = d.Type().IsRegular() && fileMD5(p) != sum
		} else if !s.db.owned[full] {
			changed = true
		}
		if !changed {
			return nil
		}
		u.files++
		include = append(include, rel)
		if fi, err := d.Info(); err == nil && d.Type().IsRegular() {
			u.bytes += fi.Size()
			u.used += fi.Size()
		}
		if len(detail) < 300 {
			detail = append(detail, full)
		}
		return nil
	})
	sort.Strings(detail)
	if len(include) == 0 {
		return
	}
	s.addWith(inventory.SystemItem{Kind: inventory.SysEtc, Label: "Réglages système modifiés ou ajoutés (/etc)",
		Paths: []string{"/etc"}, Files: u.files, Bytes: u.bytes, Used: u.used, Detail: detail}, include)
}

func fileMD5(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// perChild ajoute un élément par sous-dossier de rel qui contient des
// fichiers n'appartenant à aucun paquet (/opt/x, /srv/x).
func (s *scanner) perChild(rel, kind, prefix string) {
	entries, _ := os.ReadDir(filepath.Join(s.root, rel))
	for _, e := range entries {
		child := filepath.Join(rel, e.Name())
		if s.claimed[child] {
			continue
		}
		u, include := s.measureUnowned(child)
		s.addWith(inventory.SystemItem{Kind: kind, Label: prefix + e.Name(), Paths: []string{s.abs(child)},
			Files: u.files, Bytes: u.bytes, Used: u.used}, include)
	}
}

// varLib ajoute les données des autres services (/var/lib/x) dès qu'elles
// dépassent 1 Mo hors fichiers de paquets.
func (s *scanner) varLib() {
	entries, _ := os.ReadDir(filepath.Join(s.root, "var/lib"))
	for _, e := range entries {
		rel := "var/lib/" + e.Name()
		if !e.IsDir() || varLibSystem[e.Name()] || s.claimed[rel] {
			continue
		}
		u, include := s.measureUnowned(rel)
		if u.bytes < 1<<20 {
			continue
		}
		s.addWith(inventory.SystemItem{Kind: inventory.SysService, Label: "Données du service « " + e.Name() + " »",
			Paths: []string{s.abs(rel)}, Files: u.files, Bytes: u.bytes, Used: u.used, Advice: inventory.AdviceReview}, include)
	}
}

// custom ajoute les dossiers ajoutés à la racine (/data, /projets…).
func (s *scanner) custom(mounts map[string]bool) {
	entries, _ := os.ReadDir(s.root)
	for _, e := range entries {
		name := e.Name()
		if rootStandard[name] || !e.IsDir() || mounts["/"+name] {
			continue
		}
		if name == "timeshift" {
			u := s.measure(name, nil)
			s.add(inventory.SystemItem{Kind: inventory.SysBackup, Label: "Sauvegardes Timeshift (/timeshift)",
				Paths: []string{"/" + name}, Files: u.files, Bytes: u.bytes, Used: u.used, Advice: inventory.AdviceSkip})
			continue
		}
		u := s.measure(name, nil)
		s.add(inventory.SystemItem{Kind: inventory.SysCustom, Label: "Dossier /" + name,
			Paths: []string{"/" + name}, Files: u.files, Bytes: u.bytes, Used: u.used})
	}
}

// ---------------------------------------------------------------- disques

type mount struct{ device, point, fstype string }

// readMounts lit les systèmes de fichiers montés (fichier au format
// /proc/self/mounts).
func readMounts(path string) []mount {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []mount
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) < 3 || !strings.HasPrefix(f[0], "/dev/") {
			continue
		}
		out = append(out, mount{device: f[0], point: unescapeMount(f[1]), fstype: f[2]})
	}
	return out
}

var octal = regexp.MustCompile(`\\([0-7]{3})`)

func unescapeMount(s string) string {
	return octal.ReplaceAllStringFunc(s, func(m string) string {
		v, _ := strconv.ParseInt(m[1:], 8, 32)
		return string(rune(v))
	})
}

func diskRole(point string) string {
	switch {
	case point == "/":
		return "system"
	case point == "/home":
		return "home"
	case point == "/boot" || strings.HasPrefix(point, "/boot/") || point == "/efi":
		return "boot"
	}
	return "data"
}

// classifyDisk devine à quoi sert un disque de données : sauvegarde,
// bibliothèque de jeux, dossier personnel déplacé, ou simples données.
func classifyDisk(dir string) (kind, advice string) {
	entries, _ := os.ReadDir(dir)
	names := map[string]bool{}
	for _, e := range entries {
		names[strings.ToLower(e.Name())] = true
	}
	switch {
	case names["timeshift"] || names["backintime"] || names["deja-dup"] || names["borg"] || names["restic"] ||
		names["backup"] || names["backups"] || names["sauvegarde"] || names["sauvegardes"]:
		return inventory.SysBackup, inventory.AdviceSkip
	case names["steamlibrary"] || names["steamapps"]:
		return inventory.SysSteam, inventory.AdviceCopy
	case (names["documents"] || names["documenten"]) && (names["images"] || names["pictures"] || names["bureau"] || names["desktop"]):
		return inventory.SysHomeElse, inventory.AdviceCopy
	}
	return inventory.SysDisk, inventory.AdviceAttach
}

func statfs(p string) (size, used int64) {
	var st syscall.Statfs_t
	if syscall.Statfs(p, &st) != nil {
		return 0, 0
	}
	size = int64(st.Blocks) * int64(st.Bsize)
	used = size - int64(st.Bfree)*int64(st.Bsize)
	return
}

// disks relève les disques montés et ajoute un élément par disque de données.
func (s *scanner) disks(mountsFile string) ([]inventory.Disk, map[string]bool) {
	points := map[string]bool{}
	var out []inventory.Disk
	uuids := ReadUUIDs("/dev/disk/by-uuid")
	for _, m := range readMounts(mountsFile) {
		if m.fstype == "squashfs" || m.fstype == "iso9660" || points[m.point] {
			continue // snaps, CD-ROM
		}
		points[m.point] = true
		size, used := statfs(m.point)
		d := inventory.Disk{Device: m.device, Mount: m.point, FSType: m.fstype, Size: size, Used: used, Role: diskRole(m.point),
			UUID: uuids[resolveDev(m.device)]}
		out = append(out, d)
		if d.Role != "data" {
			continue
		}
		kind, advice := classifyDisk(filepath.Join(s.root, m.point))
		label := map[string]string{
			inventory.SysBackup:   "Disque de sauvegarde",
			inventory.SysSteam:    "Bibliothèque de jeux",
			inventory.SysHomeElse: "Dossier personnel sur un autre disque",
			inventory.SysDisk:     "Autre disque",
		}[kind] + " (" + m.point + ")"
		s.add(inventory.SystemItem{Kind: kind, Label: label, Paths: []string{m.point}, Bytes: used, Used: used, Advice: advice,
			UUID: d.UUID, FSType: m.fstype})
	}
	return out, points
}

// ReadUUIDs associe chaque périphérique (chemin réel) à l'identifiant de
// son système de fichiers, d'après les liens de /dev/disk/by-uuid.
func ReadUUIDs(dir string) map[string]string {
	out := map[string]string{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if dev, err := filepath.EvalSymlinks(filepath.Join(dir, e.Name())); err == nil {
			out[dev] = e.Name()
		}
	}
	return out
}

func resolveDev(dev string) string {
	if r, err := filepath.EvalSymlinks(dev); err == nil {
		return r
	}
	return dev
}

// ---------------------------------------------------------------- Steam

var vdfPath = regexp.MustCompile(`"path"\s+"([^"]+)"`)

// steamLibraries ajoute les bibliothèques Steam situées hors des dossiers
// personnels (second disque), déclarées dans libraryfolders.vdf.
func (s *scanner) steamLibraries(users []inventory.User) {
	for _, u := range users {
		for _, rel := range []string{".local/share/Steam/steamapps/libraryfolders.vdf", ".steam/steam/steamapps/libraryfolders.vdf",
			".var/app/com.valvesoftware.Steam/.local/share/Steam/steamapps/libraryfolders.vdf"} {
			b, err := os.ReadFile(filepath.Join(s.root, u.Home, rel))
			if err != nil {
				continue
			}
			for _, m := range vdfPath.FindAllStringSubmatch(string(b), -1) {
				p := strings.ReplaceAll(m[1], `\\`, `\`)
				if p == u.Home || strings.HasPrefix(p, u.Home+"/") || strings.HasPrefix(p, "/home/") || s.claimed[strings.TrimPrefix(p, "/")] {
					continue
				}
				if s.coveredByDisk(p) {
					continue
				}
				us := s.measure(p, nil)
				s.add(inventory.SystemItem{Kind: inventory.SysSteam, Label: "Bibliothèque Steam (" + p + ")",
					Paths: []string{p}, Files: us.files, Bytes: us.bytes, Used: us.used})
			}
		}
	}
}

func (s *scanner) coveredByDisk(p string) bool {
	for _, it := range s.items {
		if it.Kind == inventory.SysSteam || it.Kind == inventory.SysDisk || it.Kind == inventory.SysHomeElse {
			for _, q := range it.Paths {
				if p == q || strings.HasPrefix(p, q+"/") {
					return true
				}
			}
		}
	}
	return false
}

// homeExtras ajoute les dossiers de /home qui n'appartiennent à aucun compte
// (dossier partagé, ancien compte supprimé…).
func (s *scanner) homeExtras(users []inventory.User) {
	homes := map[string]bool{}
	for _, u := range users {
		homes[strings.TrimPrefix(filepath.Clean(u.Home), "/")] = true
	}
	entries, _ := os.ReadDir(filepath.Join(s.root, "home"))
	for _, e := range entries {
		rel := "home/" + e.Name()
		if homes[rel] || !e.IsDir() || e.Name() == "lost+found" || s.claimed[rel] {
			continue
		}
		u := s.measure(rel, nil)
		s.add(inventory.SystemItem{Kind: inventory.SysCustom, Label: "Dossier /" + rel + " (sans compte)",
			Paths: []string{"/" + rel}, Files: u.files, Bytes: u.bytes, Used: u.used, Advice: inventory.AdviceReview})
	}
}

// ScanSystem examine tout ce qui se trouve hors des dossiers personnels.
// mountsFile vaut /proc/self/mounts sur la machine courante, "" pour un
// système monté ailleurs (--root). Lecture seule.
//
// Renvoie aussi les jeux de données à copier (genres pris en charge par
// cette version, voir Copyable).
func ScanSystem(root, mountsFile string, users []inventory.User) ([]inventory.SystemItem, []inventory.Disk, []inventory.DataSet) {
	if root == "" {
		root = "/"
	}
	s := &scanner{root: root, db: readDpkgDB(root), claimed: map[string]bool{}}
	var disks []inventory.Disk
	mounts := map[string]bool{}
	if mountsFile != "" {
		disks, mounts = s.disks(mountsFile)
	}
	s.recognized()
	s.steamLibraries(users)
	s.etc()
	s.perChild("opt", inventory.SysOpt, "Logiciel installé à la main : /opt/")
	s.perChild("srv", inventory.SysSrv, "Données de service : /srv/")
	if !s.claimed["usr/local"] {
		u, include := s.measureUnowned("usr/local")
		s.addWith(inventory.SystemItem{Kind: inventory.SysLocal, Label: "Programmes et fichiers ajoutés (/usr/local)",
			Paths: []string{"/usr/local"}, Files: u.files, Bytes: u.bytes, Used: u.used}, include)
	}
	s.varLib()
	s.custom(mounts)
	s.homeExtras(users)
	if u := s.measure("root", nil); u.files > 0 {
		s.add(inventory.SystemItem{Kind: inventory.SysRoot, Label: "Dossier de l'administrateur (/root)",
			Paths: []string{"/root"}, Files: u.files, Bytes: u.bytes, Used: u.used, Advice: inventory.AdviceReview})
	}
	return s.items, disks, s.datasets
}
