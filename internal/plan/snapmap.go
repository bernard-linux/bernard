package plan

// SnapToFlatpak associe les Snap courants à leur équivalent Flathub.
// Une valeur vide désigne un Snap technique, qui n'est pas une application.
//
// Table provisoire : elle sera remplacée par la base de correspondances YAML
// (dossier mappings/), enrichissable sans recompiler.
var SnapToFlatpak = map[string]string{
	"firefox":          "org.mozilla.firefox",
	"thunderbird":      "org.mozilla.Thunderbird",
	"chromium":         "org.chromium.Chromium",
	"vlc":              "org.videolan.VLC",
	"gimp":             "org.gimp.GIMP",
	"inkscape":         "org.inkscape.Inkscape",
	"libreoffice":      "org.libreoffice.LibreOffice",
	"spotify":          "com.spotify.Client",
	"code":             "com.visualstudio.code",
	"telegram-desktop": "org.telegram.desktop",
	"signal-desktop":   "org.signal.Signal",
	"discord":          "com.discordapp.Discord",
	"slack":            "com.slack.Slack",
	"zoom-client":      "us.zoom.Zoom",
	"obs-studio":       "com.obsproject.Studio",
	"audacity":         "org.audacityteam.Audacity",
	"kdenlive":         "org.kde.kdenlive",
	"bitwarden":        "com.bitwarden.desktop",
	"postman":          "com.getpostman.Postman",
	"teams-for-linux":  "com.github.IsmaelMartinez.teams_for_linux",

	// Snap techniques propres à Ubuntu.
	"snap-store":                "",
	"firmware-updater":          "",
	"snapd-desktop-integration": "",
	"desktop-security-center":   "",
	"prompting-client":          "",
}
