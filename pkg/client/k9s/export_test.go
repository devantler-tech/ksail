package k9s

// SilenceKlogForTest exposes the unexported silenceKlog helper to tests in
// this package's _test variant. Production code should never call this.
func SilenceKlogForTest() {
	silenceKlog()
}
