package archive

import "github.com/moby/go-archive/compression"

// Compression is retained for compatibility with Docker 28.
//
// Deprecated: use [compression.Compression].
type Compression = compression.Compression

const (
	Uncompressed = compression.None  // Deprecated: use [compression.None].
	Bzip2        = compression.Bzip2 // Deprecated: use [compression.Bzip2].
	Gzip         = compression.Gzip  // Deprecated: use [compression.Gzip].
	Xz           = compression.Xz    // Deprecated: use [compression.Xz].
	Zstd         = compression.Zstd  // Deprecated: use [compression.Zstd].
)
