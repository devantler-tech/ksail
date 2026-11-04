package ciharness_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// ksailActionPinPattern matches a step that runs one of KSail's own composite actions at a
// full commit SHA.
var ksailActionPinPattern = regexp.MustCompile(
	`^devantler-tech/ksail/\.github/actions/[a-z0-9-]+@[0-9a-f]{40}$`,
)

// ksailActionPin returns the one reference every CI and CD step uses for KSail's own composite
// action, failing when the steps disagree or the pin is not a full commit SHA. Tests compare a
// consumer against it instead of a copied SHA, so a grouped pin bump that moves every consumer
// together passes without editing them. The content at the pinned commit is audited separately:
// TestReleaseActionPinsMatchAuditedContent (pkg/client/kubescape) reads each audited action file
// at the pin and compares it with its reviewed witness.
func ksailActionPin(t *testing.T, action string) string {
	t.Helper()

	prefix := "devantler-tech/ksail/.github/actions/" + action + "@"

	var references []string

	for _, path := range []string{".github/workflows/ci.yaml", ".github/workflows/cd.yaml"} {
		for _, reference := range workflowStepUses(t, path) {
			if strings.HasPrefix(reference, prefix) && !slices.Contains(references, reference) {
				references = append(references, reference)
			}
		}
	}

	require.Len(
		t,
		references,
		1,
		"every step must pin %s at the same commit: %v",
		action,
		references,
	)
	require.Regexp(t, ksailActionPinPattern, references[0])

	return references[0]
}

// workflowStepUses returns every step's `uses:` value in the workflow at path.
func workflowStepUses(t *testing.T, path string) []string {
	t.Helper()

	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Uses string `yaml:"uses"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(readRepoFile(t, path), &workflow))

	var uses []string

	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if step.Uses != "" {
				uses = append(uses, step.Uses)
			}
		}
	}

	return uses
}

func TestKSailActionPinsAgreeAcrossConsumers(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"free-disk-space", "restore-ksail-binary", "setup-desktop-build"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()

			ksailActionPin(t, action)
		})
	}
}
