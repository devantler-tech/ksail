package strictlinks_test

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

//nolint:tagliatelle // GitHub Actions defines these external keys in kebab-case.
type workflowDocument struct {
	On   map[string]any `yaml:"on"`
	Jobs map[string]struct {
		If             string `yaml:"if"`
		TimeoutMinutes int    `yaml:"timeout-minutes"`
		Steps          []struct {
			Name string         `yaml:"name"`
			If   string         `yaml:"if"`
			Uses string         `yaml:"uses"`
			With map[string]any `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func readWorkflow(t *testing.T, name string) workflowDocument {
	t.Helper()

	file, err := os.OpenInRoot("../../..", filepath.Join(".github/workflows", name))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, file.Close()) })

	contents, err := io.ReadAll(file)
	require.NoError(t, err)

	var document workflowDocument
	require.NoError(t, yaml.Unmarshal(contents, &document))

	return document
}

func TestTransportObservationPreservesBoundaries(t *testing.T) {
	t.Parallel()
	command := exec.CommandContext(
		t.Context(), "bash", "../../../.github/scripts/strict-link-transport.test.sh",
	)
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "strict link observation contract failed:\n%s", output)
	assert.Contains(t, string(output), "PASS: both fixed targets observed")
	assert.Contains(t, string(output), "PASS: ambiguous configuration fails")
}

func TestProbeRequiresManualOptIn(t *testing.T) {
	t.Parallel()
	document := readWorkflow(t, "image-factory-transport.yaml")
	dispatch, ok := document.On["workflow_dispatch"].(map[string]any)
	require.True(t, ok, "strict-link diagnostic requires an explicit default-off dispatch input")
	inputs, ok := dispatch["inputs"].(map[string]any)
	require.True(t, ok)
	input, ok := inputs["strict_links"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "boolean", input["type"])
	assert.Equal(t, false, input["default"])

	probe, exists := document.Jobs["strict-links"]
	require.True(t, exists)
	assert.Equal(t, "github.event_name == 'workflow_dispatch' && inputs.strict_links", probe.If)
	assert.Positive(t, probe.TimeoutMinutes)
	assert.LessOrEqual(t, probe.TimeoutMinutes, 20)
}

func TestProbeKeepsFocusedLinksStrict(t *testing.T) {
	t.Parallel()
	probe := readWorkflow(t, "image-factory-transport.yaml").Jobs["strict-links"]
	daily := readWorkflow(t, "maintenance.yaml").Jobs["check-external-links-strict"]

	for _, focused := range probe.Steps {
		if focused.Name != "Check the same two links with the strict daily client" {
			continue
		}

		assert.Equal(t, "--no-progress --config lychee.toml --accept-timeouts=false "+
			"${{ runner.temp }}/strict-links/targets.md", focused.With["args"])
		assert.Equal(t, true, focused.With["fail"])

		for _, caller := range daily.Steps {
			if caller.Name != "Check links with time-outs counted as errors" {
				continue
			}

			assert.Equal(t, caller.Uses, focused.Uses)
			assert.Equal(t, caller.With["lycheeVersion"], focused.With["lycheeVersion"])

			return
		}
	}

	t.Fatal("the focused strict client or its daily counterpart is missing")
}

func TestProbeRunsTheDailyStrictCallerAfterFocusedFailure(t *testing.T) {
	t.Parallel()
	probe := readWorkflow(t, "image-factory-transport.yaml").Jobs["strict-links"]
	daily := readWorkflow(t, "maintenance.yaml").Jobs["check-external-links-strict"]

	for _, strict := range probe.Steps {
		if strict.Name != "Run the complete daily strict caller" {
			continue
		}

		assert.Equal(t, "${{ !cancelled() && steps.prepare.outcome == 'success' }}", strict.If)

		for _, caller := range daily.Steps {
			if caller.Name != "Check links with time-outs counted as errors" {
				continue
			}

			assert.Equal(t, caller.Uses, strict.Uses)
			assert.Equal(t, caller.With, strict.With)
			assert.Equal(t, true, strict.With["fail"])

			return
		}
	}

	t.Fatal("the complete daily strict caller is missing")
}
