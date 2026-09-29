package transfer

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWriteSparse(t *testing.T) {
	p := filepath.Join(t.TempDir(), "disque.img")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 8<<20) // 8 Mo, presque entièrement nuls
	copy(data[4096*10:], []byte("début"))
	copy(data[len(data)-100:], []byte("fin"))
	for off := 0; off < len(data); off += 1 << 20 {
		if err := WriteSparse(f, data[off:off+1<<20]); err != nil {
			t.Fatal(err)
		}
	}
	f.Truncate(int64(len(data)))
	f.Close()
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, data) {
		t.Fatal("contenu différent")
	}
	fi, _ := os.Stat(p)
	used := fi.Sys().(*syscall.Stat_t).Blocks * 512
	if used > 1<<20 {
		t.Fatalf("fichier non creux : %d octets occupés pour %d", used, len(data))
	}
}
