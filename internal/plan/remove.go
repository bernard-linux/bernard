package plan

import (
	"sort"
	"strings"

	"github.com/bernard-linux/bernard/internal/hardware"
	"github.com/bernard-linux/bernard/internal/inventory"
)

// removals propose de retirer de la cible les applications que
// l'utilisateur n'a pas (ou plus) sur l'ancien ordinateur : typiquement,
// celles installées d'office par la distribution et qu'il avait supprimées.
//
// Garde-fous, du plus général au plus fin :
//   - même distribution des deux côtés (Zorin → Zorin…), agent 0.3 ou plus
//     récent (liste complète des paquets de la source) ;
//   - seulement de vraies applications (présentes dans le menu), installées
//     à la main ou par l'installateur, jamais des bibliothèques ;
//   - jamais un paquet lié au matériel (pilote graphique, micrologiciel,
//     noyau), ni un élément indispensable au système ou au bureau ;
//   - simulation apt : si le retrait emporte un paquet présent sur
//     l'ancien ordinateur, ou un paquet protégé, il n'est pas proposé ;
//   - rien n'est coché d'office quand le retrait emporte d'autres paquets,
//     ou quand les deux systèmes ne sont pas de la même version.
func removals(inv *inventory.Inventory, t Target) []Action {
	if inv.Source.OS != "linux" || inv.Source.Distro == "" || inv.Source.Distro != t.Distro || len(inv.Packages) == 0 {
		return nil
	}
	onSource := map[string]bool{}
	for _, p := range inv.Packages {
		onSource[p] = true
	}
	sameVersion := inv.Source.Version == t.Version
	var out []Action

	var pkgs []string
	for p := range t.AptManual {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	for _, pkg := range pkgs {
		name, isApp := t.AptApps[pkg]
		if !isApp || onSource[pkg] || !removable(pkg, t) {
			continue
		}
		if t.RemovalImpact == nil {
			continue
		}
		gone, err := t.RemovalImpact(pkg)
		if err != nil || len(gone) == 0 {
			continue
		}
		var also []string
		safe := true
		var size int64
		for _, g := range gone {
			size += t.AptSize[g]
			if g == pkg {
				continue
			}
			if onSource[g] || !removable(g, t) {
				safe = false
				break
			}
			also = append(also, g)
		}
		if !safe {
			continue
		}
		a := Action{Op: OpRemove, Via: "apt", Package: pkg, Label: name, Bytes: size, Also: also, Fidelity: FidelityFull}
		switch date := inv.PackagesRemoved[pkg]; {
		case date != "":
			a.Reason, a.Date = ReasonRemovedOnSource, date
		case sameVersion:
			a.Reason = ReasonAbsentOnSource
		default:
			a.Reason = ReasonAbsentOlder
		}
		a.Selected = len(also) == 0 && a.Reason != ReasonAbsentOlder
		out = append(out, a)
	}

	// Flatpak : applications de la cible absentes de la source.
	onSourceFP := map[string]bool{}
	for _, app := range inv.Apps {
		if id, ok := strings.CutPrefix(app.SourceID, "flatpak:"); ok {
			onSourceFP[id] = true
		}
	}
	var ids []string
	for id := range t.FlatpakInstalled {
		if !onSourceFP[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		name := t.FlatpakNames[id]
		if name == "" {
			name = id
		}
		reason := ReasonAbsentOnSource
		if !sameVersion {
			reason = ReasonAbsentOlder
		}
		out = append(out, Action{Op: OpRemove, Via: "flatpak", Package: id, Label: name, Reason: reason,
			Fidelity: FidelityFull, Selected: reason == ReasonAbsentOnSource})
	}
	return out
}

// Éléments du système et du bureau jamais proposés au retrait, même s'ils
// ont une entrée dans le menu.
var coreApps = []string{
	"gnome-shell", "gnome-session", "gnome-control-center", "gnome-terminal", "nautilus", "nemo",
	"gnome-software", "gnome-software-*", "software-properties-*", "update-manager", "update-notifier",
	"gdm3", "lightdm", "network-manager", "network-manager-*", "pulseaudio", "pipewire", "pipewire-*",
	"wireplumber", "xorg", "xwayland", "snapd", "flatpak", "apt", "apt-*", "dpkg", "sudo", "policykit-*",
	"polkitd", "systemd", "systemd-*", "ubiquity", "ubiquity-*", "gnome-disk-utility", "gnome-system-monitor",
	"gnome-tweaks", "zorin-*", "mint*", "cinnamon", "cinnamon-*", "ubuntu-*", "language-selector-*",
	"yelp", "gnome-keyring", "seahorse", "cups", "system-config-printer*", "bluez", "gnome-bluetooth*",
	"ibus", "ibus-*", "fcitx*", "im-config", "gnome-initial-setup", "xdg-desktop-portal*", "gnome-remote-desktop",
	"file-roller", "gnome-text-editor", "gedit", "xed", "ubuntu-drivers-common", "software-properties-gtk",
}

// removable : ni matériel, ni protégé par dpkg (Essential, priorité
// required/important), ni élément du système ou du bureau.
func removable(pkg string, t Target) bool {
	if hardware.HardwarePackage(pkg) || t.AptProtected[pkg] {
		return false
	}
	for _, pat := range coreApps {
		if pat == pkg || (strings.HasSuffix(pat, "*") && strings.HasPrefix(pkg, strings.TrimSuffix(pat, "*"))) {
			return false
		}
	}
	return true
}
