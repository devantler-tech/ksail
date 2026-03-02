package chat

// ExportSetStreaming sets Model.isStreaming for testing.
var ExportSetStreaming = func(m *Model, streaming bool) {
	m.isStreaming = streaming
}
