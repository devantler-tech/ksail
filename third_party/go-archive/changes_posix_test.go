package archive

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/moby/sys/user"
)

func TestHardLinkOrder(t *testing.T) {
	names := []string{"file1.txt", "file2.txt", "file3.txt"}
	msg := []byte("Hey y'all")

	tmpDir := t.TempDir()
	// Create dir
	src, err := os.MkdirTemp(tmpDir, "docker-hardlink-test-src-")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		func() {
			err := os.WriteFile(filepath.Join(src, name), msg, 0o666)
			if err != nil {
				t.Fatal(err)
			}
		}()
	}
	// Create dest, with changes that includes hardlinks
	dest := filepath.Join(tmpDir, "docker-hardlink-test-dest")
	if err := copyDir(src, dest); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		for i := range 5 {
			if err := os.Link(filepath.Join(dest, name), filepath.Join(dest, fmt.Sprintf("%s.link%d", name, i))); err != nil {
				t.Fatal(err)
			}
		}
	}

	// get changes
	changes, err := ChangesDirs(dest, src)
	if err != nil {
		t.Fatal(err)
	}

	// sort
	sort.Sort(changesByPath(changes))

	// ExportChanges
	ar, err := ExportChanges(dest, changes, user.IdentityMapping{})
	if err != nil {
		t.Fatal(err)
	}
	hdrs, err := walkHeaders(ar)
	if err != nil {
		t.Fatal(err)
	}

	// reverse sort
	sort.Sort(sort.Reverse(changesByPath(changes)))
	// ExportChanges
	arRev, err := ExportChanges(dest, changes, user.IdentityMapping{})
	if err != nil {
		t.Fatal(err)
	}
	hdrsRev, err := walkHeaders(arRev)
	if err != nil {
		t.Fatal(err)
	}

	// line up the two sets
	sort.Sort(tarHeaders(hdrs))
	sort.Sort(tarHeaders(hdrsRev))

	// compare Size and LinkName
	for i := range hdrs {
		if hdrs[i].Name != hdrsRev[i].Name {
			t.Errorf("headers - expected name %q; but got %q", hdrs[i].Name, hdrsRev[i].Name)
		}
		if hdrs[i].Size != hdrsRev[i].Size {
			t.Errorf("headers - %q expected size %d; but got %d", hdrs[i].Name, hdrs[i].Size, hdrsRev[i].Size)
		}
		if hdrs[i].Typeflag != hdrsRev[i].Typeflag {
			t.Errorf("headers - %q expected type %d; but got %d", hdrs[i].Name, hdrs[i].Typeflag, hdrsRev[i].Typeflag)
		}
		if hdrs[i].Linkname != hdrsRev[i].Linkname {
			t.Errorf("headers - %q expected linkname %q; but got %q", hdrs[i].Name, hdrs[i].Linkname, hdrsRev[i].Linkname)
		}
	}
}

type tarHeaders []tar.Header

func (th tarHeaders) Len() int           { return len(th) }
func (th tarHeaders) Swap(i, j int)      { th[j], th[i] = th[i], th[j] }
func (th tarHeaders) Less(i, j int) bool { return th[i].Name < th[j].Name }

func walkHeaders(r io.Reader) ([]tar.Header, error) {
	t := tar.NewReader(r)
	var headers []tar.Header
	for {
		hdr, err := t.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return headers, err
		}
		headers = append(headers, *hdr)
	}
	return headers, nil
}
