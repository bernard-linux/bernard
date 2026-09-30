package settings

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/bernard-linux/bernard/internal/sysexec"
)

func TestSanitizeVPN(t *testing.T) {
	ovpn := "[connection]\nid=Bureau\nuuid=0a1b2c3d-1111-2222-3333-444455556666\ntype=vpn\n\n[vpn]\nservice-type=org.freedesktop.NetworkManager.openvpn\nremote=vpn.exemple.be\n"
	c, id, _, err := SanitizeWifi(ovpn, nil)
	if err != nil || id != "Bureau" || !strings.Contains(c, "remote=vpn.exemple.be") {
		t.Fatalf("openvpn : %q %v", id, err)
	}
	if VPNPlugin(ovpn) != "network-manager-openvpn-gnome" {
		t.Errorf("greffon : %q", VPNPlugin(ovpn))
	}
	wg := "[connection]\nid=wg-maison\nuuid=0a1b2c3d-1111-2222-3333-444455556667\ntype=wireguard\ninterface-name=wg0\n\n[wireguard]\nprivate-key=abc\n"
	c, _, _, err = SanitizeWifi(wg, nil)
	if err != nil || !strings.Contains(c, "interface-name=wg0") {
		t.Fatalf("wireguard : interface à garder : %v\n%s", err, c)
	}
	if VPNPlugin(wg) != "" {
		t.Error("wireguard : aucun greffon")
	}
	wifi := "[connection]\nid=Maison\nuuid=0a1b2c3d-1111-2222-3333-444455556668\ntype=wifi\ninterface-name=wlp2s0\n"
	c, _, _, _ = SanitizeWifi(wifi, nil)
	if strings.Contains(c, "interface-name") {
		t.Error("Wi-Fi : nom de carte à retirer")
	}
	if _, _, _, err := SanitizeWifi("[connection]\nid=Câble\nuuid=0a1b2c3d-1111-2222-3333-444455556669\ntype=ethernet\n", nil); err == nil {
		t.Error("filaire : à ignorer")
	}
}

func TestGnomeExtensions(t *testing.T) {
	in := ParseDump(`[org/gnome/shell]
favorite-apps=['firefox.desktop', 'org.gnome.Nautilus.desktop']
enabled-extensions=['zorin-taskbar@zorinos.com', 'dash-to-dock@micxgx.gmail.com']
command-history=['secret']
welcome-dialog-last-shown-version='45'

[org/gnome/shell/extensions/zorin-taskbar]
panel-positions='{"0":"TOP"}'

[org/gnome/mutter]
edge-tiling=true
`)
	out := Translate(in, "gnome", "gnome", nil, Hardware{KeepKeyboard: true})
	if out["org/gnome/shell"]["favorite-apps"] == "" || out["org/gnome/shell"]["enabled-extensions"] == "" {
		t.Errorf("dock ou extensions perdus : %v", out["org/gnome/shell"])
	}
	if _, ok := out["org/gnome/shell"]["command-history"]; ok {
		t.Error("historique interne repris")
	}
	if out["org/gnome/shell/extensions/zorin-taskbar"]["panel-positions"] == "" {
		t.Error("tableau de bord de Zorin perdu")
	}
	if out["org/gnome/mutter"]["edge-tiling"] != "true" {
		t.Error("mutter perdu")
	}
	cin := Translate(ParseDump("[org/cinnamon]\nenabled-applets=['panel1:left:0:menu@cinnamon.org:0']\nnext-applet-id=12\n"), "cinnamon", "cinnamon", nil, Hardware{KeepKeyboard: true})
	if cin["org/cinnamon"]["enabled-applets"] == "" || cin["org/cinnamon"]["next-applet-id"] != "" {
		t.Errorf("cinnamon : %v", cin["org/cinnamon"])
	}
}

func TestRebuildFontCache(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(home+"/.cache/fontconfig", 0o755)
	os.WriteFile(home+"/.cache/fontconfig/abc-le64.cache-9", []byte("index abîmé"), 0o644)
	os.MkdirAll(home+"/.fontconfig", 0o755)
	os.WriteFile(home+"/.fontconfig/abc-le64.cache-3", []byte("ancien index"), 0o644)
	os.MkdirAll(home+"/.config/fontconfig", 0o755)
	os.WriteFile(home+"/.config/fontconfig/fonts.conf", []byte("<fontconfig/>"), 0o644)
	var got []string
	a := &Applier{Exec: func(_ context.Context, c sysexec.Cmd) (string, error) {
		got = append(got, c.Name+" "+strings.Join(c.Args, " "))
		return "", nil
	}}
	if err := a.RebuildFontCache(context.Background(), "alice", home); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/.cache/fontconfig", "/.fontconfig"} {
		if _, err := os.Stat(home + p); err == nil {
			t.Errorf("%s : index non jeté", p)
		}
	}
	if _, err := os.Stat(home + "/.config/fontconfig/fonts.conf"); err != nil {
		t.Error("réglages des polices de l'utilisateur perdus")
	}
	if len(got) != 1 || !strings.HasPrefix(got[0], "runuser -u alice -- env HOME="+home) || !strings.HasSuffix(got[0], "fc-cache -f") {
		t.Errorf("commande : %v", got)
	}
	// ~/.fontconfig contenant autre chose qu'un index : laissé en place.
	os.MkdirAll(home+"/.fontconfig", 0o755)
	os.WriteFile(home+"/.fontconfig/fonts.conf", []byte("<fontconfig/>"), 0o644)
	a.RebuildFontCache(context.Background(), "alice", home)
	if _, err := os.Stat(home + "/.fontconfig/fonts.conf"); err != nil {
		t.Error("~/.fontconfig personnel supprimé")
	}
}
