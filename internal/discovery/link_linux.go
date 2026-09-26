package discovery

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// linkInfo lit le type et la vitesse d'une interface dans /sys/class/net.
func linkInfo(name string) (string, int) {
	base := filepath.Join("/sys/class/net", name)
	speed := 0
	if b, err := os.ReadFile(filepath.Join(base, "speed")); err == nil {
		if v, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && v > 0 {
			speed = v
		}
	}
	if _, err := os.Stat(filepath.Join(base, "wireless")); err == nil {
		return LinkWifi, speed
	}
	if drv, err := os.Readlink(filepath.Join(base, "device", "driver")); err == nil {
		d := filepath.Base(drv)
		if strings.Contains(d, "thunderbolt") {
			return LinkThunderbolt, speed
		}
	}
	if strings.HasPrefix(name, "thunderbolt") {
		return LinkThunderbolt, speed
	}
	if b, err := os.ReadFile(filepath.Join(base, "type")); err == nil && strings.TrimSpace(string(b)) == "1" {
		return LinkEthernet, speed
	}
	return LinkOther, speed
}

// bindToDevice lie une socket à une interface (SO_BINDTODEVICE, réservé à
// l'administrateur ; sans effet sinon : la socket reste liée à l'adresse).
func bindToDevice(name string) func(string, string, syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error {
		c.Control(func(fd uintptr) {
			syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, name)
		})
		return nil
	}
}
