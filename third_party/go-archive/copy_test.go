package archive

import (
	"archive/tar"
	"bytes"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"
)

func TestRebaseArchiveEntriesPlatformPaths(t *testing.T) {
	cases := []struct {
		name         string
		oldBase      string
		newBase      string
		headerName   string
		linkName     string
		wantName     string
		wantLinkName string
	}{
		{
			name:       "rebase relative name from root",
			oldBase:    "/",
			newBase:    "prefix",
			headerName: "foo/bar",
			wantName:   "prefix/foo/bar",
		},
		{
			name:       "rebase absolute name from root",
			oldBase:    "/",
			newBase:    "prefix",
			headerName: "/foo/bar",
			wantName:   "prefix/foo/bar",
		},
		{
			name:       "rebase name with multiple leading slashes from root",
			oldBase:    "/",
			newBase:    "prefix",
			headerName: "///foo/bar",
			wantName:   "prefix/foo/bar",
		},
		{
			name:       "root with multiple trailing slashes",
			oldBase:    "///",
			newBase:    "prefix",
			headerName: "/foo/bar",
			wantName:   "prefix/foo/bar",
		},
		{
			name:       "rebase absolute name from root to empty",
			oldBase:    "/",
			newBase:    "",
			headerName: "/foo/bar",
			wantName:   "foo/bar",
		},
		{
			name:       "rebase absolute name from root to empty",
			oldBase:    "/",
			headerName: "/foo/bar",
			wantName:   "foo/bar",
		},
		{
			name:       "regular file",
			oldBase:    filepath.Join("origin", "subdir"),
			newBase:    filepath.Join("dest", "target"),
			headerName: "origin/subdir/file",
			wantName:   "dest/target/file",
		},
		{
			name:       "regular file POSIX",
			oldBase:    "origin/subdir",
			newBase:    "dest/target",
			headerName: "origin/subdir/file",
			wantName:   "dest/target/file",
		},
		{
			name:       "name equals old base",
			oldBase:    "origin",
			newBase:    "dest",
			headerName: "origin",
			wantName:   "dest",
		},
		{
			name:       "old base not at beginning",
			oldBase:    "origin",
			newBase:    "dest",
			headerName: "other/origin/file",
			wantName:   "other/origin/file",
		},
		{
			name:       "old base is partial path component",
			oldBase:    "origin",
			newBase:    "dest",
			headerName: "original/file",
			wantName:   "original/file",
		},
		{
			name:       "old base with multiple trailing slashes",
			oldBase:    "origin///",
			newBase:    "dest",
			headerName: "origin/file",
			wantName:   "dest/file",
		},
		{
			name:       "new base with multiple trailing slashes",
			oldBase:    "origin",
			newBase:    "dest///",
			headerName: "origin/file",
			wantName:   "dest/file",
		},
		{
			name:       "preserve unclean name",
			oldBase:    "/",
			newBase:    "prefix",
			headerName: "foo/../bar",
			wantName:   "prefix/foo/../bar",
		},
		{
			name:       "do not rebase partial path component match",
			oldBase:    "foo",
			newBase:    "prefix",
			headerName: "foobar/baz",
			wantName:   "foobar/baz",
		},
		{
			name:         "hardlink",
			oldBase:      filepath.Join("origin", "subdir"),
			newBase:      filepath.Join("dest", "target"),
			headerName:   "origin/subdir/link",
			linkName:     "origin/subdir/file",
			wantName:     "dest/target/link",
			wantLinkName: "dest/target/file",
		},
		{
			name:         "hardlink POSIX",
			oldBase:      "origin/subdir",
			newBase:      "dest/target",
			headerName:   "origin/subdir/link",
			linkName:     "origin/subdir/file",
			wantName:     "dest/target/link",
			wantLinkName: "dest/target/file",
		},
		{
			name:         "hardlink rebase relative names from root",
			oldBase:      "/",
			newBase:      "prefix",
			headerName:   "link",
			linkName:     "target",
			wantName:     "prefix/link",
			wantLinkName: "prefix/target",
		},
		{
			name:         "hardlink rebase absolute names from root",
			oldBase:      "/",
			newBase:      "prefix",
			headerName:   "/link",
			linkName:     "/target",
			wantName:     "prefix/link",
			wantLinkName: "prefix/target",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)

			hdr := &tar.Header{
				Name: tc.headerName,
				Mode: 0o644,
			}
			if tc.linkName != "" {
				hdr.Typeflag = tar.TypeLink
				hdr.Linkname = tc.linkName
			} else {
				hdr.Typeflag = tar.TypeReg
				hdr.Size = int64(len("content"))
			}

			assert.NilError(t, tw.WriteHeader(hdr))
			if hdr.Typeflag == tar.TypeReg {
				_, err := tw.Write([]byte("content"))
				assert.NilError(t, err)
			}
			assert.NilError(t, tw.Close())

			rc := RebaseArchiveEntries(&buf, tc.oldBase, tc.newBase)
			defer rc.Close()

			tr := tar.NewReader(rc)
			got, err := tr.Next()
			assert.NilError(t, err)

			assert.Check(t, is.Equal(got.Name, tc.wantName))
			assert.Check(t, is.Equal(got.Linkname, tc.wantLinkName))
		})
	}
}
