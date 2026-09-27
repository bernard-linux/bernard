package hardware

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestKeyboard(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "etc/default"), 0o755)
	os.WriteFile(filepath.Join(root, "etc/default/keyboard"), []byte("XKBMODEL=\"pc105\"\nXKBLAYOUT=\"be,fr\"\nXKBVARIANT=\",bepo\"\n"), 0o644)
	if got := Keyboard(root); got != "be,fr+bepo" {
		t.Fatalf("clavier = %q", got)
	}
	if Keyboard(t.TempDir()) != "" {
		t.Fatal("clavier inconnu attendu")
	}
}

func TestGPUs(t *testing.T) {
	root := t.TempDir()
	add := func(dev, class, vendor string) {
		d := filepath.Join(root, "sys/bus/pci/devices", dev)
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "class"), []byte(class+"\n"), 0o644)
		os.WriteFile(filepath.Join(d, "vendor"), []byte(vendor+"\n"), 0o644)
	}
	add("0000:00:02.0", "0x030000", "0x8086")
	add("0000:01:00.0", "0x030200", "0x10de")
	add("0000:00:1f.3", "0x040300", "0x8086") // carte son
	if got := GPUs(root); !reflect.DeepEqual(got, []string{"intel", "nvidia"}) {
		t.Fatalf("cartes = %v", got)
	}
}

func TestHardwarePackage(t *testing.T) {
	for name, want := range map[string]bool{
		"nvidia-driver-550": true, "libnvidia-gl-550": true, "linux-image-6.8.0-45-generic": true,
		"linux-generic-hwe-22.04": true, "intel-microcode": true, "grub-efi-amd64-signed": true,
		"firmware-sof-signed": true, "virtualbox-guest-utils": true, "bernard": true,
		"gimp": false, "rhythmbox": false, "nvidia-settings": true, "linux-tools-common": false,
		"libreoffice-writer": false, "brave-browser": false,
	} {
		if got := HardwarePackage(name); got != want {
			t.Errorf("%s : %v, attendu %v", name, got, want)
		}
	}
}
