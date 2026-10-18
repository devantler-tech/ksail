package ciharness_test

import (
	"fmt"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type dockerMatrixAxis struct {
	name   string
	values []string
}

type dockerMatrix struct {
	axes     []dockerMatrixAxis
	includes []map[string]string
	excludes []map[string]string
}

// UnmarshalYAML retains the axis order that determines matrix creation order.
func (matrix *dockerMatrix) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return &yaml.TypeError{Errors: []string{"matrix must be a mapping"}}
	}

	for index := 0; index < len(node.Content); index += 2 {
		key, value := node.Content[index].Value, node.Content[index+1]

		var err error

		switch key {
		case "include":
			err = value.Decode(&matrix.includes)
		case "exclude":
			err = value.Decode(&matrix.excludes)
		default:
			axis := dockerMatrixAxis{name: key}
			err = value.Decode(&axis.values)
			matrix.axes = append(matrix.axes, axis)
		}

		if err != nil {
			return fmt.Errorf("decode matrix %s: %w", key, err)
		}
	}

	return nil
}

func TestDockerMatrixPreservesEveryEffectiveScenario(t *testing.T) {
	t.Parallel()

	//nolint:tagliatelle // GitHub Actions defines these external keys in kebab-case.
	var workflow struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix      dockerMatrix `yaml:"matrix"`
				MaxParallel int          `yaml:"max-parallel"`
				FailFast    bool         `yaml:"fail-fast"`
			} `yaml:"strategy"`
			Steps []harnessStep `yaml:"steps"`
		} `yaml:"jobs"`
	}

	require.NoError(t, yaml.Unmarshal(readRepoFile(t, ".github/workflows/ci.yaml"), &workflow))
	job, ok := workflow.Jobs["system-test-docker"]
	require.True(t, ok)
	assert.Equal(t, 15, job.Strategy.MaxParallel, "ordering must not buy speed with more runners")
	assert.False(t, job.Strategy.FailFast, "every selected scenario must run after a sibling fails")

	var baseline dockerMatrix

	require.NoError(t, yaml.Unmarshal(readRepoFile(t,
		"internal/ciharness/testdata/docker-matrix-baseline.yaml"), &baseline))
	want := expandDockerMatrix(t, baseline)
	got := expandDockerMatrix(t, job.Strategy.Matrix)
	require.Len(t, want, 30, "the frozen baseline contains all thirty scenarios")
	require.Len(t, got, 30)

	run := findHarnessStep(t, job.Steps, "🧪 Run KSail System Test")
	cleanup := findHarnessStep(t, job.Steps, "🧪 Cleanup KSail System Test")

	for _, scenario := range got {
		runArgs := dockerMatrixArgs(t, stringValue(run.With["args"]), scenario)
		cleanupArgs := dockerMatrixArgs(t, stringValue(cleanup.With["args"]), scenario)
		assert.Equal(t, runArgs, cleanupArgs, "cleanup must address the same named host")
		scenario["args"] = runArgs
		delete(scenario, "effective-args")
	}

	assert.ElementsMatch(
		t,
		want,
		got,
		"every option, upgrade, migration and nested trial must survive",
	)
	require.Equal(t, "Talos", got[0]["distribution"], "start the measured critical path first")
	assert.Equal(t, "--name system-test-cluster", got[0]["args"])
	assert.Equal(t, "true", got[0]["test-kubernetes-provider"])
	assert.Equal(t, "v1.36.2", got[0]["kubernetes-upgrade-from"])
	assert.Equal(t, "v1.37.1", got[0]["kubernetes-upgrade-to"])
	assertDockerLongCasesInFirstWave(t, got[:15])
}

func assertDockerLongCasesInFirstWave(t *testing.T, firstWave []map[string]string) {
	t.Helper()

	const fullStackArgs = "--name system-test-cluster --cni Cilium --csi Enabled --load-balancer Enabled " +
		"--metrics-server Enabled --policy-engine Kyverno --cert-manager Enabled --gitops-engine Flux"

	for _, distribution := range []string{"Talos", "VCluster", "K3s", "Vanilla"} {
		var nested, fullStack bool

		for _, scenario := range firstWave {
			if scenario["distribution"] != distribution {
				continue
			}

			nested = nested || scenario["test-kubernetes-provider"] == "true"
			fullStack = fullStack || scenario["args"] == fullStackArgs
		}

		assert.True(t, nested, "%s nested trials must be created in the first wave", distribution)
		assert.True(t, fullStack, "%s full stack must be created in the first wave", distribution)
	}
}

// Only the two argument contracts used by this workflow are accepted. Refuse an
// unfamiliar expression rather than silently assuming it forwards the host name.
func dockerMatrixArgs(t *testing.T, expression string, scenario map[string]string) string {
	t.Helper()

	switch expression {
	case "${{ matrix.args }}":
		return scenario["args"]
	case "${{ matrix.effective-args || matrix.args }}":
		if args := scenario["effective-args"]; args != "" {
			return args
		}

		return scenario["args"]
	default:
		t.Fatalf("unverified matrix argument expression: %q", expression)

		return ""
	}
}

// Preserve YAML axis order and GitHub's original-combination include semantics:
// include may replace a previous overlay, but not an original axis value, and
// appended combinations never become overlay targets. This narrow interpreter
// inspects the workflow's inputs; hosted CI remains the execution proof.
func expandDockerMatrix(t *testing.T, matrix dockerMatrix) []map[string]string {
	t.Helper()

	original := dockerMatrixAxes(matrix.axes)

	var kept, result []map[string]string

	for _, entry := range original {
		excluded := false

		for _, exclude := range matrix.excludes {
			excluded = excluded || maps.Equal(exclude, matrixSubset(entry, exclude))
		}

		if !excluded {
			kept = append(kept, entry)
			result = append(result, maps.Clone(entry))
		}
	}

	for _, include := range matrix.includes {
		matched := false

		for index, entry := range kept {
			if !matrixOverlayConflicts(entry, include) {
				maps.Copy(result[index], include)

				matched = true
			}
		}

		if !matched {
			result = append(result, maps.Clone(include))
		}
	}

	return result
}

func dockerMatrixAxes(axes []dockerMatrixAxis) []map[string]string {
	original := []map[string]string{{}}

	for _, axis := range axes {
		var expanded []map[string]string

		for _, entry := range original {
			for _, value := range axis.values {
				combination := maps.Clone(entry)
				combination[axis.name] = value
				expanded = append(expanded, combination)
			}
		}

		original = expanded
	}

	return original
}

func matrixOverlayConflicts(original, overlay map[string]string) bool {
	for key, value := range overlay {
		if originalValue, exists := original[key]; exists && value != originalValue {
			return true
		}
	}

	return false
}

func matrixSubset(entry, subset map[string]string) map[string]string {
	result := make(map[string]string, len(subset))

	for key := range subset {
		if value, exists := entry[key]; exists {
			result[key] = value
		}
	}

	return result
}

func TestDockerMatrixExpansionCharacterizesOriginalOnlyOverlays(t *testing.T) {
	t.Parallel()

	// GitHub's documented include example: the last banana is not merged into
	// the first appended banana, and a later pink overlay replaces green.
	var matrix dockerMatrix

	require.NoError(t, yaml.Unmarshal([]byte(`fruit: [apple, pear]
animal: [cat, dog]
include:
  - color: green
  - color: pink
    animal: cat
  - fruit: apple
    shape: circle
  - fruit: banana
  - fruit: banana
    animal: cat
`), &matrix))
	assert.Equal(t, []map[string]string{
		{"fruit": "apple", "animal": "cat", "color": "pink", "shape": "circle"},
		{"fruit": "apple", "animal": "dog", "color": "green", "shape": "circle"},
		{"fruit": "pear", "animal": "cat", "color": "pink"},
		{"fruit": "pear", "animal": "dog", "color": "green"},
		{"fruit": "banana"},
		{"fruit": "banana", "animal": "cat"},
	}, expandDockerMatrix(t, matrix))
}

func TestDockerMatrixExcludesBeforeReintroducingStandaloneEntries(t *testing.T) {
	t.Parallel()

	var matrix dockerMatrix

	require.NoError(t, yaml.Unmarshal([]byte(`distribution: [Talos, Vanilla]
args: ['', named]
exclude:
  - distribution: Talos
    args: ''
  - args: named
include:
  - distribution: Talos
    args: ''
    upgrade: pinned
`), &matrix))
	assert.Equal(t, []map[string]string{
		{"distribution": "Vanilla", "args": ""},
		{"distribution": "Talos", "args": "", "upgrade": "pinned"},
	}, expandDockerMatrix(t, matrix))
}
