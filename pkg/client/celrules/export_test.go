package celrules

//nolint:gochecknoglobals // export_test.go pattern: expose internal seams to black-box tests.
var (
	// ParseDocument exposes parseDocument for black-box tests.
	ParseDocument = parseDocument
	// DocumentIdentity exposes documentIdentity for black-box tests.
	DocumentIdentity = documentIdentity
)
