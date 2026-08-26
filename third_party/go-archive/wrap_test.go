package archive

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"testing"

	"gotest.tools/v3/assert"
)

func TestGenerateEmptyFile(t *testing.T) {
	archive, err := Generate("emptyFile")
	assert.NilError(t, err)
	if archive == nil {
		t.Fatal("The generated archive should not be nil.")
	}

	expectedFiles := [][]string{
		{"emptyFile", ""},
	}

	tr := tar.NewReader(archive)
	actualFiles := make([][]string, 0, 10)
	i := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		assert.NilError(t, err)
		buf := new(bytes.Buffer)
		_, err = buf.ReadFrom(tr)
		assert.NilError(t, err)
		content := buf.String()
		actualFiles = append(actualFiles, []string{hdr.Name, content})
		i++
	}
	if len(actualFiles) != len(expectedFiles) {
		t.Fatalf("Number of expected file %d, got %d.", len(expectedFiles), len(actualFiles))
	}
	for i := range expectedFiles {
		actual := actualFiles[i]
		expected := expectedFiles[i]
		if actual[0] != expected[0] {
			t.Fatalf("Expected name '%s', Actual name '%s'", expected[0], actual[0])
		}
		if actual[1] != expected[1] {
			t.Fatalf("Expected content '%s', Actual content '%s'", expected[1], actual[1])
		}
	}
}

func TestGenerateWithContent(t *testing.T) {
	archive, err := Generate("file", "content")
	assert.NilError(t, err)
	if archive == nil {
		t.Fatal("The generated archive should not be nil.")
	}

	expectedFiles := [][]string{
		{"file", "content"},
	}

	tr := tar.NewReader(archive)
	actualFiles := make([][]string, 0, 10)
	i := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		assert.NilError(t, err)
		buf := new(bytes.Buffer)
		_, err = buf.ReadFrom(tr)
		assert.NilError(t, err)
		content := buf.String()
		actualFiles = append(actualFiles, []string{hdr.Name, content})
		i++
	}
	if len(actualFiles) != len(expectedFiles) {
		t.Fatalf("Number of expected file %d, got %d.", len(expectedFiles), len(actualFiles))
	}
	for i := range expectedFiles {
		actual := actualFiles[i]
		expected := expectedFiles[i]
		if actual[0] != expected[0] {
			t.Fatalf("Expected name '%s', Actual name '%s'", expected[0], actual[0])
		}
		if actual[1] != expected[1] {
			t.Fatalf("Expected content '%s', Actual content '%s'", expected[1], actual[1])
		}
	}
}
