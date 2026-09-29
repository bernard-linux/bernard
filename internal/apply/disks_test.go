package apply

import (
	"strings"
	"testing"
)

func TestFstabLine(t *testing.T) {
	if got := fstabLine("1111-AAAA", "ext4", "/mnt/Jeux", 1000, 1000); got != "UUID=1111-AAAA /mnt/Jeux ext4 defaults,nofail 0 2 # ajouté par Bernard\n" {
		t.Errorf("ext4 : %q", got)
	}
	if got := fstabLine("ABCD", "fuseblk", "/mnt/Mes Jeux", 1000, 1000); !strings.Contains(got, `/mnt/Mes\040Jeux ntfs3 defaults,nofail,uid=1000,gid=1000`) {
		t.Errorf("NTFS : %q", got)
	}
}
