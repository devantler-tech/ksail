package ciharness_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNestedKubeletJournalPublishesOnlyBoundedFixedSignals(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ name, input, want string }{
		{"file limit", "failed to create fsnotify watcher: too many open files opaque-bootstrap-token", "file-descriptors"},
		{"swap", "running with swap on is not supported: private.fixture", "swap-enabled"},
		{"runtime", "container runtime is down: https://private.fixture/token", "container-runtime"},
		{"unclassified", "opaque-bootstrap-token private.fixture", "unclassified"},
		{"bounded", strings.Repeat("x", 65536) + "too many open files", "unclassified"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			command := exec.CommandContext(t.Context(), "bash",
				"../../.github/scripts/summarize-nested-kubelet.sh")
			command.Stdin = strings.NewReader(testCase.input)
			output, err := command.CombinedOutput()
			require.NoError(t, err, string(output))
			assert.Contains(t, string(output), testCase.want)
			assert.NotContains(t, string(output), "opaque-bootstrap-token")
			assert.NotContains(t, string(output), "private.fixture")
		})
	}
}
