package cipher

// --- Exported seams for internal helper tests ---

// WriteDecryptedOutput exposes the private writeDecryptedOutput for testing.
var WriteDecryptedOutput = writeDecryptedOutput //nolint:gochecknoglobals // Standard Go export_test.go pattern.

// ShowRotatePreview exposes the private showRotatePreview for testing.
var ShowRotatePreview = showRotatePreview //nolint:gochecknoglobals // Standard Go export_test.go pattern.

// BuildRotateOpts exposes the private buildRotateOpts for testing.
var BuildRotateOpts = buildRotateOpts //nolint:gochecknoglobals // Standard Go export_test.go pattern.

// ErrRotateKeyConflict exposes the private errRotateKeyConflict for testing.
var ErrRotateKeyConflict = errRotateKeyConflict
