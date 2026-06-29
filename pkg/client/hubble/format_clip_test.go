package hubble_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/hubble"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFormatPlainLineClipsLongEndpoints verifies that an endpoint name longer
// than the fixed column width is truncated with an ellipsis so it cannot spill
// into the next column and break streaming-row alignment.
func TestFormatPlainLineClipsLongEndpoints(t *testing.T) {
	t.Parallel()

	longPod := strings.Repeat("a", 60)

	var out bytes.Buffer

	err := hubble.FormatPlainLine(&out, hubble.FlowRecord{
		Verdict:     "FORWARDED",
		Protocol:    "TCP",
		Source:      hubble.Endpoint{Namespace: "default", Pod: longPod},
		Destination: hubble.Endpoint{Namespace: "kube-system", Pod: "dns"},
	})
	require.NoError(t, err)

	line := out.String()
	assert.Contains(t, line, "…", "an overflowing endpoint name is ellipsized")
	assert.NotContains(t, line, longPod, "the full over-width name must not be emitted")
	// The 28-rune SOURCE column truncates to 27 runes + the ellipsis.
	assert.Contains(t, line, "default/"+strings.Repeat("a", 19)+"…")
}
