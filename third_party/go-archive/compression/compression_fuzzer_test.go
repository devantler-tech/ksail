package compression

import (
	"bytes"
	"testing"
)

func FuzzDecompressStream(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecompressStream(bytes.NewReader(data))
	})
}
