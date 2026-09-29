package aptrepo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadAndKnown(t *testing.T) {
	root := t.TempDir()
	w := func(rel, c string) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(c), 0o644)
	}
	w("etc/apt/sources.list.d/brave-browser-release.list",
		"deb [signed-by=/usr/share/keyrings/brave-browser-archive-keyring.gpg arch=amd64] https://brave-browser-apt-release.s3.brave.com/ stable main\n")
	w("usr/share/keyrings/brave-browser-archive-keyring.gpg", "CLE")
	w("etc/apt/sources.list.d/vscode.sources",
		"Types: deb\nURIs: https://packages.microsoft.com/repos/code\nSuites: stable\nComponents: main\nSigned-By: /etc/apt/keyrings/packages.microsoft.gpg\n")
	w("etc/apt/keyrings/packages.microsoft.gpg", "MS")
	w("etc/apt/sources.list.d/desactive.sources", "Types: deb\nURIs: http://exemple.org/\nSuites: x\nEnabled: no\n")
	w("etc/apt/sources.list.d/../../passwd", "x")

	src := Read(root)
	if len(src) != 2 {
		t.Fatalf("dépôts : %+v", src)
	}
	if src[0].URIs[0] != "brave-browser-apt-release.s3.brave.com" || src[0].Keys["/usr/share/keyrings/brave-browser-archive-keyring.gpg"] != "Q0xF" {
		t.Errorf("brave : %+v", src[0])
	}
	if src[1].URIs[0] != "packages.microsoft.com/repos/code" || src[1].Keys["/etc/apt/keyrings/packages.microsoft.gpg"] == "" {
		t.Errorf("vscode : %+v", src[1])
	}
	if !Known(root)["packages.microsoft.com/repos/code"] {
		t.Error("Known")
	}
}

func TestKeyPathOK(t *testing.T) {
	for p, want := range map[string]bool{
		"/usr/share/keyrings/x.gpg": true, "/etc/apt/keyrings/a.asc": true, "/etc/apt/trusted.gpg.d/b.gpg": true,
		"/etc/passwd": false, "/usr/share/keyrings/../x": false, "/usr/share/keyrings/sub/x.gpg": false,
	} {
		if KeyPathOK(p) != want {
			t.Errorf("%s : %v", p, !want)
		}
	}
}

func TestOrigins(t *testing.T) {
	out := `brave-browser:
  Installed: 1.70.117
  Candidate: 1.70.117
  Version table:
 *** 1.70.117 500
        500 https://brave-browser-apt-release.s3.brave.com stable/main amd64 Packages
        100 /var/lib/dpkg/status
     1.69.0 500
        500 https://ailleurs.example.org stable/main amd64 Packages
gimp:
  Installed: 2.10.36-3
  Candidate: 2.10.36-3
  Version table:
 *** 2.10.36-3 500
        500 http://archive.ubuntu.com/ubuntu noble/universe amd64 Packages
        100 /var/lib/dpkg/status
maison:
  Installed: 1.0
  Candidate: 1.0
  Version table:
 *** 1.0 100
        100 /var/lib/dpkg/status
`
	o := Origins(out)
	if o["brave-browser"] != "brave-browser-apt-release.s3.brave.com" || o["gimp"] != "archive.ubuntu.com/ubuntu" || o["maison"] != "" {
		t.Errorf("origines : %v", o)
	}
}

func TestRetarget(t *testing.T) {
	in := "deb http://ppa.launchpadcontent.net/x/y/ubuntu jammy main\ndeb http://z jammy-updates main\n"
	want := "deb http://ppa.launchpadcontent.net/x/y/ubuntu noble main\ndeb http://z noble-updates main\n"
	if got := Retarget(in, "jammy", "noble"); got != want {
		t.Errorf("%q", got)
	}
}
