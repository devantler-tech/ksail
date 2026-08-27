package archive

import (
	"testing"

	"github.com/moby/go-archive/compression"
)

func TestLegacyCompressionAliases(t *testing.T) {
	t.Parallel()

	var legacy Compression = Gzip
	if legacy != compression.Gzip {
		t.Fatalf("legacy Gzip alias = %v, want %v", legacy, compression.Gzip)
	}
}
