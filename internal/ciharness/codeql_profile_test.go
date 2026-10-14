package ciharness_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Exercise the actual workflow entrypoint: report contents alone must never
// conceal a failure of the collector process that produced them.
func TestCodeQLProfileBindsActualProcessStatus(t *testing.T) {
	t.Parallel()

	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}

	err := yaml.Unmarshal(readRepoFile(t, ".github/workflows/codeql-go-profile.yaml"), &workflow)
	require.NoError(t, err)

	var entrypoint string

	for _, step := range workflow.Jobs["profile"].Steps {
		if step.Name == "Measure every Go project and verify function bodies" {
			entrypoint = step.Run
		}
	}

	require.NotEmpty(t, entrypoint)

	for _, test := range []struct {
		name, comparison string
		actual, reported int
		pass, validate   bool
	}{
		{"normal success", "false", 0, 0, true, false},
		{"normal failure", "false", 23, 0, false, false},
		{"complete pair observation", "true", 0, 0, true, true},
		{"unexpected collector failure", "true", 23, 0, false, false},
		{"honest failed baseline", "true", 23, 23, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			scripts := filepath.Join(directory, ".github/scripts")
			require.NoError(t, os.MkdirAll(scripts, 0o700))

			collector := fmt.Sprintf(
				"mkdir -p \"$1\"\nprintf '{\"profileExitCode\":%d}\\n' > \"$1/report.json\"\nexit %d\n",
				test.reported,
				test.actual,
			)
			require.NoError(
				t,
				os.WriteFile(
					filepath.Join(scripts, "profile-codeql-go.sh"),
					[]byte(collector),
					0o600,
				),
			)

			validator := "printf called > \"$RUNNER_TEMP/validated\"\n"
			require.NoError(
				t,
				os.WriteFile(
					filepath.Join(scripts, "profile-codeql-compare.sh"),
					[]byte(validator),
					0o600,
				),
			)
			command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
			command.Dir = directory
			command.Stdin = strings.NewReader(entrypoint)

			command.Env = append(
				os.Environ(),
				"RUNNER_TEMP="+directory,
				"SWAP_COMPARISON="+test.comparison,
				"ADDED_SWAP_GIB=0",
				"SOURCE_SHA="+strings.Repeat("a", 40),
			)
			output, runErr := command.CombinedOutput()
			assert.Equal(t, test.pass, runErr == nil, "%s: %v", output, runErr)

			_, statErr := os.Stat(filepath.Join(directory, "validated"))
			assert.Equal(
				t,
				test.validate,
				statErr == nil,
				"validator invocation disagrees with actual status",
			)
		})
	}
}
