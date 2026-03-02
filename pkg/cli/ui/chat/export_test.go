//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package chat

// SetStreaming exposes Model.isStreaming for testing.
func (m *Model) SetStreaming(streaming bool) {
	m.isStreaming = streaming
}
