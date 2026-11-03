package ciharness_test

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNestedKindHostPreparationIsLimitedToRoutingReproducer(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	steps := workflow.Jobs["system-test-docker"].Steps
	setup := findHarnessStep(t, steps, "Prepare nested Kind watcher capacity")
	assert.Equal(
		t,
		"${{ matrix.test-kubernetes-provider == 'true' && matrix.kubernetes-provider-distributions == 'Vanilla' }}",
		setup.If,
	)
	assert.Equal(t, "bash .github/scripts/prepare-nested-kind-host.sh\n", setup.Run)
	assert.False(t, setup.ContinueOnError)

	positions := make(map[string]int)
	for index, step := range steps {
		positions[step.Name] = index
	}

	assert.Less(t, positions[setup.Name], positions["🧪 Run KSail System Test"])
}

func TestNestedKindHostPreparationRejectsUnsafeEvidence(t *testing.T) {
	t.Parallel()

	command := exec.CommandContext(
		t.Context(),
		"bash",
		"../../.github/scripts/prepare-nested-kind-host.test.sh",
	)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
}
