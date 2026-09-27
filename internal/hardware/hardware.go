// Package hardware relève ce qui dépend de la machine elle-même et non de
// son utilisateur : disposition du clavier physique, cartes graphiques. Ces
// éléments ne sont jamais recopiés aveuglément d'un ordinateur à l'autre.
package hardware

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Keyboard lit la disposition du clavier choisie à l'installation
// (/etc/default/keyboard) : « be », « fr », « fr+bepo »… Vide si inconnue.
func Keyboard(root string) string {
	f, err := os.Open(filepath.Join(root, "etc/default/keyboard"))
	if err != nil {
		return ""
	}
	defer f.Close()
	kv := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if ok {
			kv[k] = strings.Trim(v, `"'`)
		}
	}
	layouts := strings.Split(kv["XKBLAYOUT"], ",")
	variants := strings.Split(kv["XKBVARIANT"], ",")
	var out []string
	for i, l := range layouts {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if i < len(variants) && strings.TrimSpace(variants[i]) != "" {
			l += "+" + strings.TrimSpace(variants[i])
		}
		out = append(out, l)
	}
	return strings.Join(out, ",")
}

// Fabricants de cartes graphiques, par identifiant PCI.
var gpuVendors = map[string]string{"0x10de": "nvidia", "0x1002": "amd", "0x8086": "intel"}

// GPUs liste les fabricants des cartes graphiques présentes (« intel »,
// « nvidia »…), triés, sans doublon.
func GPUs(root string) []string {
	devs, _ := filepath.Glob(filepath.Join(root, "sys/bus/pci/devices/*"))
	set := map[string]bool{}
	for _, d := range devs {
		class, err := os.ReadFile(filepath.Join(d, "class"))
		if err != nil || !strings.HasPrefix(strings.TrimSpace(string(class)), "0x03") {
			continue // 0x03xxxx : contrôleur d'affichage
		}
		v, _ := os.ReadFile(filepath.Join(d, "vendor"))
		name := gpuVendors[strings.TrimSpace(string(v))]
		if name == "" {
			name = "autre"
		}
		set[name] = true
	}
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Paquets liés au matériel : pilotes, micrologiciels, noyaux, amorçage,
// outils des machines virtuelles. Bernard ne les installe jamais depuis
// l'ancien ordinateur et ne les retire jamais du nouveau : chaque machine
// garde ceux que son installation a choisis pour elle.
var hardwarePkg = regexp.MustCompile(`^(` + strings.Join([]string{
	`nvidia-.*`, `libnvidia-.*`, `xserver-xorg-video-.*`, `.*-dkms`, `dkms`,
	`linux-(image|headers|modules|generic|signed|firmware|oem|hwe|lowlatency)(-.*)?`,
	`linux-firmware.*`, `firmware-.*`, `.*-firmware(-.*)?`, `.*-microcode`,
	`grub-.*`, `grub2?`, `shim(-.*)?`, `efibootmgr`, `mokutil`, `secureboot-db`,
	`oem-.*`, `bcmwl-.*`, `broadcom-sta-.*`, `r8168-.*`,
	`virtualbox-guest-.*`, `open-vm-tools.*`, `qemu-guest-agent`, `spice-vdagent`,
	`ubuntu-drivers-common`, `nvidia-prime`, `prime-.*`, `switcheroo-control`,
	`tlp(-.*)?`, `thermald`, `fwupd.*`, `bolt`, `fprintd`, `libfprint-.*`,
	`bernard`,
}, "|") + `)$`)

// HardwarePackage indique un paquet lié au matériel (voir hardwarePkg).
func HardwarePackage(name string) bool { return hardwarePkg.MatchString(name) }
