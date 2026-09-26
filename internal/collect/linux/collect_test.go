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
