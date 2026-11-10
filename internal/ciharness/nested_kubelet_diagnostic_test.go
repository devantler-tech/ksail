package ciharness_test

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const nestedKubeletSignalPrefix = "Nested kubelet journal signal: "

// nestedKubeletLabels is the closed set of labels the summary may publish.
func nestedKubeletLabels() []string {
	return []string{
		"inotify-instance-limit", "inotify-watch-limit", "file-descriptors",
		"storage-or-watch-limit", "swap-enabled", "container-runtime", "runtime-connection",
		"cgroup-driver", "memory-cgroup", "cgroup-mount", "read-only-filesystem",
		"kubelet-configuration", "kubelet-flags", "qos-cgroups", "cpu-manager", "memory-manager",
		"kubelet-cgroup", "kubelet-privileges", "kubelet-startup", "unclassified", "unavailable",
	}
}

type nestedKubeletCase struct {
	name, input string
	want        []string
}

// privateTail stands in for journal text that must never be published.
const privateTail = " opaque-bootstrap-token https://private.fixture/path"

func nestedKubeletCases() []nestedKubeletCase {
	cutLine := "inotify_add_watch /sys/fs/cgroup: no space left on device\n"
	cutInput := cutLine + strings.Repeat("x", 65536+10-len(cutLine))

	return []nestedKubeletCase{
		{
			"descriptor limit",
			"open /proc: too many open files" + privateTail,
			[]string{"file-descriptors"},
		},
		{
			"inotify instance limit",
			"failed to create fsnotify watcher: too many open files" + privateTail,
			[]string{"inotify-instance-limit"},
		},
		{
			"inotify watch limit",
			"inotify_add_watch /sys/fs/cgroup: no space left on device" + privateTail,
			[]string{"inotify-watch-limit"},
		},
		{
			"storage",
			"write /var/lib/kubelet: no space left on device" + privateTail,
			[]string{"storage-or-watch-limit"},
		},
		{
			"inotify permission error is not a watch limit",
			"inotify_add_watch /sys/fs/cgroup: permission denied" + privateTail,
			[]string{"unclassified"},
		},
		{
			"signal phrases on different lines do not combine",
			"inotify_add_watch /sys: permission denied\nwrite /var: no space left on device",
			[]string{"storage-or-watch-limit"},
		},
		{"swap", "Running with swap on is not supported" + privateTail, []string{"swap-enabled"}},
		{
			"several signals in allow-list order",
			"failed to run kubelet\ncontainer runtime is down\ncontainer runtime is down",
			[]string{"container-runtime", "kubelet-startup"},
		},
		{
			"apostrophe phrase",
			"failed to get the kubelet's cgroup" + privateTail,
			[]string{"kubelet-cgroup"},
		},
		{"unclassified", "opaque-bootstrap-token private.fixture", []string{"unclassified"}},
		{"empty", "", []string{"unavailable"}},
		{"whitespace", " \n\t\n", []string{"unavailable"}},
		{
			"keeps newest bytes",
			strings.Repeat("x", 65536) + "\ntoo many open files",
			[]string{"file-descriptors"},
		},
		{
			"drops oldest bytes",
			"too many open files\n" + strings.Repeat("x", 65536),
			[]string{"unclassified"},
		},
		{"drops a line cut part-way", cutInput, []string{"unclassified"}},
	}
}

// TestNestedKubeletJournalPublishesOnlyFixedSignals pins the classifier: each signal needs all
// its phrases on one line, each output line is an allowed label, and no journal text leaks.
func TestNestedKubeletJournalPublishesOnlyFixedSignals(t *testing.T) {
	t.Parallel()

	for _, testCase := range nestedKubeletCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			lines := summarizeNestedKubelet(t, testCase.input)

			got := make([]string, 0, len(lines))
			for _, line := range lines {
				label, found := strings.CutPrefix(line, nestedKubeletSignalPrefix)
				require.True(t, found, "unexpected output line %q", line)
				require.True(t, slices.Contains(nestedKubeletLabels(), label), "label %q", label)

				got = append(got, label)
			}

			assert.Equal(t, testCase.want, got)
		})
	}
}

func summarizeNestedKubelet(t *testing.T, input string) []string {
	t.Helper()

	command := exec.CommandContext(t.Context(), "bash",
		"../../.github/scripts/summarize-nested-kubelet.sh")
	command.Stdin = strings.NewReader(input)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))

	return strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
}
