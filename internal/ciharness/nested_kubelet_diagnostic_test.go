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

	for _, testCase := range []struct{ name, input, want, absent string }{
		{
			"file limit", "failed to create fsnotify watcher: too many open files opaque-bootstrap-token",
			"file-descriptors", "",
		},
		{
			"watch limit", "inotify_add_watch /sys/fs/cgroup: no space left on device private.fixture",
			"inotify-watch-limit", "",
		},
		{"no space", "write /var/lib/kubelet: no space left on device", "storage-or-watch-limit", "inotify-watch-limit"},
		{"swap", "running with swap on is not supported: private.fixture", "swap-enabled", ""},
		{"runtime", "container runtime is down: https://private.fixture/token", "container-runtime", ""},
		{"unclassified", "opaque-bootstrap-token private.fixture", "unclassified", ""},
		{"keeps newest", strings.Repeat("x", 65536) + "too many open files", "file-descriptors", "unclassified"},
		{"drops oldest", "too many open files" + strings.Repeat("x", 65536), "unclassified", "file-descriptors"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			output, err := summarizeNestedKubelet(t, testCase.input)
			require.NoError(t, err, output)
			assert.Contains(t, output, testCase.want)

			if testCase.absent != "" {
				assert.NotContains(t, output, testCase.absent)
			}

			assert.NotContains(t, output, "opaque-bootstrap-token")
			assert.NotContains(t, output, "private.fixture")
		})
	}
}

func TestNestedKubeletJournalRejectsEmptyEvidence(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", " \n\t\n"} {
		output, err := summarizeNestedKubelet(t, input)
		require.Error(t, err)
		assert.Empty(t, output)
	}
}

func summarizeNestedKubelet(t *testing.T, input string) (string, error) {
	t.Helper()

	command := exec.CommandContext(t.Context(), "bash",
		"../../.github/scripts/summarize-nested-kubelet.sh")
	command.Stdin = strings.NewReader(input)
	output, err := command.CombinedOutput()

	return string(output), err
}
