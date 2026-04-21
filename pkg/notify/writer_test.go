package notify_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStageSeparatingWriter_AddsNewlineBeforeTitles(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	writer := notify.NewStageSeparatingWriter(&buf)

	// First write - no leading newline
	_, _ = writer.Write([]byte("🚀 Create cluster...\n"))
	_, _ = writer.Write([]byte("► creating cluster\n"))
	_, _ = writer.Write([]byte("✔ cluster created\n"))

	// Second title - should have leading newline
	_, _ = writer.Write([]byte("📦 Installing components...\n"))
	_, _ = writer.Write([]byte("► installing flux\n"))
	_, _ = writer.Write([]byte("✔ flux installed\n"))

	expected := `🚀 Create cluster...
► creating cluster
✔ cluster created

📦 Installing components...
► installing flux
✔ flux installed
`
	assert.Equal(t, expected, buf.String())
}

func TestStageSeparatingWriter_NoNewlineForFirstTitle(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	writer := notify.NewStageSeparatingWriter(&buf)

	// First title - no leading newline
	_, _ = writer.Write([]byte("🚀 Create cluster...\n"))

	expected := "🚀 Create cluster...\n"
	assert.Equal(t, expected, buf.String())
}

func TestStageSeparatingWriter_NoNewlineForNonTitleLines(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	writer := notify.NewStageSeparatingWriter(&buf)

	// First write
	_, _ = writer.Write([]byte("🚀 Create cluster...\n"))

	// Non-title lines - no leading newline
	_, _ = writer.Write([]byte("► creating cluster\n"))
	_, _ = writer.Write([]byte("✔ cluster created\n"))

	expected := `🚀 Create cluster...
► creating cluster
✔ cluster created
`
	assert.Equal(t, expected, buf.String())
}

func TestStageSeparatingWriter_Reset(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	writer := notify.NewStageSeparatingWriter(&buf)

	// Write some content
	_, _ = writer.Write([]byte("🚀 Create cluster...\n"))
	assert.True(t, writer.HasWritten())

	// Reset
	writer.Reset()
	assert.False(t, writer.HasWritten())

	// Next title should not have leading newline
	_, _ = writer.Write([]byte("📦 Installing components...\n"))

	expected := `🚀 Create cluster...
📦 Installing components...
`
	assert.Equal(t, expected, buf.String())
}

func TestStageSeparatingWriter_MultipleTitles(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	writer := notify.NewStageSeparatingWriter(&buf)

	// Multiple stages with titles
	_, _ = writer.Write([]byte("🗄️ Create local registry...\n"))
	_, _ = writer.Write([]byte("► creating local registry\n"))
	_, _ = writer.Write([]byte("✔ local registry created\n"))

	_, _ = writer.Write([]byte("🚀 Create cluster...\n"))
	_, _ = writer.Write([]byte("► creating cluster\n"))
	_, _ = writer.Write([]byte("✔ cluster created\n"))

	_, _ = writer.Write([]byte("🔌 Attach local registry...\n"))
	_, _ = writer.Write([]byte("► attaching local registry\n"))
	_, _ = writer.Write([]byte("✔ local registry attached\n"))

	_, _ = writer.Write([]byte("📦 Installing components...\n"))
	_, _ = writer.Write([]byte("► flux installing\n"))
	_, _ = writer.Write([]byte("✔ flux installed\n"))

	expected := `🗄️ Create local registry...
► creating local registry
✔ local registry created

🚀 Create cluster...
► creating cluster
✔ cluster created

🔌 Attach local registry...
► attaching local registry
✔ local registry attached

📦 Installing components...
► flux installing
✔ flux installed
`
	assert.Equal(t, expected, buf.String())
}

func TestStageSeparatingWriter_EmptyWrite(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	writer := notify.NewStageSeparatingWriter(&buf)

	n, err := writer.Write([]byte{})

	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.False(t, writer.HasWritten())
}

func TestStageSeparatingWriter_ASCIIOnly(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	writer := notify.NewStageSeparatingWriter(&buf)

	// ASCII content - should not trigger leading newline detection
	_, _ = writer.Write([]byte("Hello World\n"))
	_, _ = writer.Write([]byte("Another line\n"))

	expected := `Hello World
Another line
`
	assert.Equal(t, expected, buf.String())
}
