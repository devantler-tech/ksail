package tarheader

import (
	"archive/tar"
	"os"
	"testing"
)

func TestNosysFileInfo(t *testing.T) {
	st, err := os.Stat("tarheader_test.go")
	if err != nil {
		t.Fatal(err)
	}
	h, err := tar.FileInfoHeader(nosysFileInfo{st}, "")
	if err != nil {
		t.Fatal(err)
	}
	if h.Uname != "" {
		t.Errorf("uname should be empty; got %v", h.Uname)
	}
	if h.Gname != "" {
		t.Errorf("gname should be empty; got %v", h.Uname)
	}
}
