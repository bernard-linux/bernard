package linux

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bernard-linux/bernard/internal/inventory"
)

const passwd = `root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
arnaud:x:1000:1000:Arnaud,,,:/home/arnaud:/bin/bash
invite:x:1001:1001::/home/invite:/bin/bash
service:x:1002:1002::/var/lib/service:/usr/sbin/nologin
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin
`

const group = `sudo:x:27:arnaud
lpadmin:x:120:arnaud,invite
arnaud:x:1000:
`

func TestParsePasswdKeepsOnlyHumans(t *testing.T) {
	users := parsePasswd(strings.NewReader(passwd), parseGroups(strings.NewReader(group)))
	if len(users) != 2 {
		t.Fatalf("attendu 2 comptes humains, obtenu %d : %+v", len(users), users)
	}
	a := users[0]
	if a.Login != "arnaud" || a.FullName != "Arnaud" || a.UID != 1000 || a.ID != "u1" {
		t.Errorf("compte mal lu : %+v", a)
	}
	if strings.Join(a.Groups, ",") != "lpadmin,sudo" {
		t.Errorf("groupes inattendus : %v", a.Groups)
	}
}

func TestNormalizeDesktop(t *testing.T) {
	cases := map[string]string{
		"zorin:GNOME": "gnome", "ubuntu:GNOME": "gnome", "X-Cinnamon": "cinnamon",
		"KDE": "kde", "": "",
	}
	for in, want := range cases {
		if got := normalizeDesktop(in); got != want {
			t.Errorf("normalizeDesktop(%q) = %q, attendu %q", in, got, want)
		}
	}
}

func TestExcluded(t *testing.T) {
	for rel, want := range map[string]bool{
		".cache":                              true,
		".cache/mozilla/firefox":              true,
		".local/share/Trash/files/x.pdf":      true,
		".local/share/applications/a.desktop": false,
		".var/app/org.gimp.GIMP/cache/x":      true,
		".var/app/org.gimp.GIMP/config/x":     false,
		"Documents/.cache-notes.txt":          false,
		"Documents/rapport.odt":               false,
	} {
		if got := excluded(rel, DefaultExcludes); got != want {
			t.Errorf("excluded(%q) = %v, attendu %v", rel, got, want)
		}
	}
}

// fakeRunner renvoie des sorties préenregistrées ; toute autre commande est
// considérée comme absente.
func fakeRunner(outputs map[string]string) Runner {
	return func(_ context.Context, name string, args ...string) (string, error) {
		key := strings.Join(append([]string{name}, args...), " ")
		if out, ok := outputs[key]; ok {
			return out, nil
		}
		return "", ErrMissingCommand
	}
}

func TestCollectOnFakeSystem(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("etc/os-release", "ID=zorin\nVERSION_ID=\"17\"\n")
	write("etc/passwd", passwd)
	write("etc/group", group)
	write("home/arnaud/Documents/rapport.odt", "12345")
	write("home/arnaud/.cache/gros-cache.bin", strings.Repeat("x", 1000))
	write("home/invite/notes.txt", "ab")

	run := fakeRunner(map[string]string{
		"apt-mark showmanual":                                   "gimp\nvlc\n",
		"dpkg-query -W -f ${Package}\t${Version}\n":             "gimp\t2.10.36-3\nvlc\t3.0.20-3\n",
		"flatpak list --app --columns=application,name,version": "org.signal.Signal\tSignal\t7.30.0\n",
		"snap list": "Name  Version  Rev  Tracking  Publisher  Notes\n" +
			"core22  20240111  1122  latest/stable  canonical  base\n" +
			"firefox  131.0  4955  latest/stable  mozilla  -\n" +
			"snapd  2.63  21759  latest/stable  canonical  snapd\n",
		"nmcli -t -f NAME,TYPE connection show": "Bureau\\:5G:802-11-wireless\nWired connection 1:802-3-ethernet\n",
	})

	inv, err := Collect(context.Background(), Options{Root: root, Runner: run})
	if err != nil {
		t.Fatal(err)
	}
	if err := inv.Validate(); err != nil {
		t.Fatalf("inventaire invalide : %v", err)
	}
	if inv.Source.Distro != "zorin" || inv.Source.Version != "17" {
		t.Errorf("source mal lue : %+v", inv.Source)
	}
	var ids []string
	for _, a := range inv.Apps {
		ids = append(ids, a.SourceID)
	}
	if got := strings.Join(ids, " "); got != "apt:gimp apt:vlc flatpak:org.signal.Signal snap:firefox" {
		t.Errorf("applications inattendues : %s", got)
	}
	if inv.Apps[0].Version != "2.10.36-3" {
		t.Errorf("version apt manquante : %+v", inv.Apps[0])
	}
	if len(inv.Network.Wifi) != 1 || inv.Network.Wifi[0] != "Bureau:5G" {
		t.Errorf("Wi-Fi mal lu : %v", inv.Network.Wifi)
	}
	if len(inv.DataSets) != 2 {
		t.Fatalf("attendu 2 jeux de données, obtenu %d", len(inv.DataSets))
	}
	if d := inv.DataSets[0]; d.Files != 1 || d.SizeBytes != 5 {
		t.Errorf("le cache aurait dû être exclu : %+v", d)
	}
	if inv.Apps[3].Origin != inventory.OriginSnap {
		t.Errorf("origine Snap attendue : %+v", inv.Apps[3])
	}
}

// Un système monté ailleurs (--root) est lu dans ses fichiers, jamais via
// les commandes de la machine qui exécute l'agent.
func TestOfflineRootReadsItsOwnApps(t *testing.T) {
	root := t.TempDir()
	put := func(rel, content string) {
		os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755)
		os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644)
	}
	put("etc/os-release", "ID=zorin\nVERSION_ID=\"17\"\n")
	put("etc/passwd", "root:x:0:0::/root:/bin/bash\nalice:x:1000:1000:Alice,,,:/home/alice:/bin/bash\n")
	put("etc/group", "alice:x:1000:\n")
	put("home/alice/Documents/a.txt", "a")
	put("var/lib/dpkg/status", "Package: gimp\nStatus: install ok installed\nVersion: 2.10\nDescription: x\n continuation\n\n"+
		"Package: libfoo\nStatus: install ok installed\nVersion: 1\n\nPackage: removed\nStatus: deinstall ok config-files\nVersion: 1\n")
	put("var/lib/apt/extended_states", "Package: libfoo\nArchitecture: amd64\nAuto-Installed: 1\n")
	put("var/lib/flatpak/app/org.videolan.VLC/current", "")
	put("home/alice/.local/share/flatpak/app/com.valvesoftware.Steam/x", "")
	os.MkdirAll(filepath.Join(root, "snap/firefox/4000"), 0o755)
	os.Symlink("4000", filepath.Join(root, "snap/firefox/current"))
	os.MkdirAll(filepath.Join(root, "snap/core22/1"), 0o755)
	os.Symlink("1", filepath.Join(root, "snap/core22/current"))
	put("etc/cups/printers.conf", "<DefaultPrinter Bureau>\n</DefaultPrinter>\n")

	calls := 0
	inv, err := Collect(context.Background(), Options{Root: root, Offline: true, Runner: func(ctx context.Context, name string, args ...string) (string, error) {
		calls++
		return "", ErrMissingCommand
	}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range inv.Apps {
		got = append(got, a.SourceID)
	}
	want := "apt:gimp flatpak:com.valvesoftware.Steam flatpak:org.videolan.VLC snap:firefox"
	if strings.Join(got, " ") != want {
		t.Errorf("applications : %v, attendu %s", got, want)
	}
	if len(inv.Network.Printers) != 1 || inv.Network.Printers[0] != "Bureau" {
		t.Errorf("imprimantes : %v", inv.Network.Printers)
	}
	if calls > 0 {
		t.Errorf("%d commandes lancées pour un système hors ligne", calls)
	}
}

func TestBrowserExcludes(t *testing.T) {
	cases := map[string]bool{
		".config/BraveSoftware/Brave-Browser/SingletonLock":                              true,
		".config/BraveSoftware/Brave-Browser/SingletonSocket":                            true,
		".config/BraveSoftware/Brave-Browser/Default/GPUCache/data_0":                    true,
		".config/BraveSoftware/Brave-Browser/GrShaderCache/x":                            true,
		".config/google-chrome/Profile 1/Code Cache/js/a":                                true,
		".mozilla/firefox/abcd.default-release/lock":                                     true,
		".mozilla/firefox/abcd.default-release/.parentlock":                              true,
		".var/app/com.brave.Browser/config/BraveSoftware/Brave-Browser/Default/GPUCache": true,
		// Conservés : mots de passe, favoris, clés.
		".config/BraveSoftware/Brave-Browser/Default/Login Data": false,
		".config/BraveSoftware/Brave-Browser/Default/Bookmarks":  false,
		".config/BraveSoftware/Brave-Browser/Local State":        false,
		".mozilla/firefox/abcd.default-release/logins.json":      false,
		".mozilla/firefox/abcd.default-release/key4.db":          false,
		".mozilla/firefox/abcd.default-release/places.sqlite":    false,
		".local/share/keyrings/login.keyring":                    false,
	}
	for rel, want := range cases {
		if got := excluded(rel, DefaultExcludes); got != want {
			t.Errorf("%s : exclu = %v, attendu %v", rel, got, want)
		}
	}
}

func TestRemovedPackages(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "var/lib/dpkg"), 0o755)
	os.MkdirAll(filepath.Join(root, "var/log"), 0o755)
	os.WriteFile(filepath.Join(root, "var/lib/dpkg/status"), []byte(
		"Package: gimp\nStatus: install ok installed\n\nPackage: cheese\nStatus: install ok installed\n\nPackage: old\nStatus: deinstall ok config-files\n"), 0o644)
	os.WriteFile(filepath.Join(root, "var/log/dpkg.log"), []byte(
		"2026-03-01 10:00:00 remove rhythmbox:amd64 3.4.7 <none>\n"+
			"2026-03-02 10:00:00 remove cheese:amd64 44 <none>\n"+ // réinstallé depuis
			"2026-03-03 10:00:00 install cheese:amd64 <none> 44\n"+
			"2026-03-04 10:00:00 purge old:amd64 1 <none>\n"), 0o644)
	pk := allPackages(root)
	if len(pk) != 2 || pk[0] != "cheese" || pk[1] != "gimp" {
		t.Fatalf("paquets = %v", pk)
	}
	rm := removedPackages(root, pk)
	if rm["rhythmbox"] != "2026-03-01" || rm["old"] != "2026-03-04" || rm["cheese"] != "" {
		t.Fatalf("retirés = %v", rm)
	}
}

func TestScanSystem(t *testing.T) {
	root := t.TempDir()
	w := func(rel, content string) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	// Base dpkg : /etc/app.conf est un fichier de configuration d'origine
	// « abc » ; /opt/paquet/bin est installé par un paquet.
	w("var/lib/dpkg/info/app.list", "/etc/app.conf\n/etc/intact.conf\n/opt/paquet\n/opt/paquet/bin\n/usr/local/share\n")
	w("var/lib/dpkg/status", "Package: app\nStatus: install ok installed\nConffiles:\n /etc/app.conf 900150983cd24fb0d6963f7d28e17f72\n /etc/intact.conf 900150983cd24fb0d6963f7d28e17f72\n\n")
	w("etc/app.conf", "modifié")      // conffile modifié
	w("etc/intact.conf", "abc")       // conffile d'origine (md5 de « abc »)
	w("etc/ajout.conf", "x")          // ajouté à la main
	w("etc/machine-id", "1234")       // propre à la machine
	w("etc/fstab", "UUID=…")          // propre à la machine
	w("opt/paquet/bin", "binaire")    // installé par un paquet
	w("opt/monlogiciel/app", "12345") // installé à la main
	w("var/www/html/index.php", "<?php")
	w("var/lib/mysql/ibdata1", "donnees")
	w("var/lib/apt/lists/x", "cache")
	w("data/projets/plan.odt", "plan")
	w("timeshift/snapshots/1/x", "s")
	w("home/partage/film.mkv", "film")
	w("home/arnaud/Documents/a.txt", "a")
	w("usr/local/bin/outil", "#!/bin/sh")

	items, _, sets := ScanSystem(root, "", []inventory.User{{ID: "u1", Login: "arnaud", Home: "/home/arnaud"}})
	setBy := map[string]inventory.DataSet{}
	for _, d := range sets {
		setBy[d.Dest] = d
	}
	if d := setBy["/etc"]; strings.Join(d.Include, ",") != "ajout.conf,app.conf" || d.Kind != "system" {
		t.Errorf("jeu /etc : %+v", d)
	}
	if d := setBy["/opt/monlogiciel"]; d.Include != nil {
		t.Errorf("/opt/monlogiciel : aucun fichier de paquet, tout copier : %+v", d.Include)
	}
	if d := setBy["/var/lib/mysql"]; d.Service != "mysql" {
		t.Errorf("base MySQL : copiée avec arrêt du service : %+v", d)
	}
	byLabel := map[string]inventory.SystemItem{}
	for _, it := range items {
		byLabel[it.Kind+"|"+it.Paths[0]] = it
	}
	etc := byLabel["etc|/etc"]
	if got := strings.Join(etc.Detail, ","); got != "/etc/ajout.conf,/etc/app.conf" {
		t.Errorf("/etc : %q", got)
	}
	if it, ok := byLabel["opt|/opt/monlogiciel"]; !ok || it.Files != 1 {
		t.Errorf("/opt/monlogiciel manquant : %v", byLabel)
	}
	if _, ok := byLabel["opt|/opt/paquet"]; ok {
		t.Error("/opt/paquet appartient à un paquet : il sera réinstallé, pas copié")
	}
	if _, ok := byLabel["web|/var/www"]; !ok {
		t.Error("sites web non reconnus")
	}
	if it := byLabel["database|/var/lib/mysql"]; it.Service != "mysql" {
		t.Errorf("MySQL : %+v", it)
	}
	if _, ok := byLabel["custom|/data"]; !ok {
		t.Error("/data manquant")
	}
	if it := byLabel["backup|/timeshift"]; it.Advice != inventory.AdviceSkip {
		t.Errorf("Timeshift : %+v", it)
	}
	if _, ok := byLabel["custom|/home/partage"]; !ok {
		t.Error("/home/partage (sans compte) manquant")
	}
	if _, ok := byLabel["custom|/home/arnaud"]; ok {
		t.Error("le dossier personnel d'arnaud est déjà migré à part")
	}
	if it := byLabel["local|/usr/local"]; it.Files != 1 {
		t.Errorf("/usr/local : %+v", it)
	}
	for k := range byLabel {
		if strings.Contains(k, "var/lib/apt") {
			t.Error("les données d'apt ne doivent pas apparaître")
		}
	}
}

func TestClassifyDisk(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "timeshift"), 0o755)
	if k, a := classifyDisk(d); k != inventory.SysBackup || a != inventory.AdviceSkip {
		t.Errorf("sauvegarde : %s %s", k, a)
	}
	d = t.TempDir()
	os.MkdirAll(filepath.Join(d, "Documents"), 0o755)
	os.MkdirAll(filepath.Join(d, "Images"), 0o755)
	if k, _ := classifyDisk(d); k != inventory.SysHomeElse {
		t.Errorf("dossier personnel déplacé : %s", k)
	}
	d = t.TempDir()
	os.MkdirAll(filepath.Join(d, "SteamLibrary"), 0o755)
	if k, _ := classifyDisk(d); k != inventory.SysSteam {
		t.Errorf("Steam : %s", k)
	}
}
