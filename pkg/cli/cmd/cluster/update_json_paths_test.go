package cluster_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pinnedUpgraderFake is the update fake with its distribution pinned one
// release ahead of the running version, as runUpdateCommand uses.
func pinnedUpgraderFake() *updatableUpgraderFake {
	return &updatableUpgraderFake{versionUpgraderFake: versionUpgraderFake{
		current: clusterupdate.VersionInfo{
			KubernetesVersion:   "v1.34.0",
			DistributionVersion: "v1.12.0",
		},
		distributionPin: "v1.13.0",
	}}
}

// inPlaceUpgraderFake reports one provisioner-level in-place change, so a real
// update takes the apply path.
func inPlaceUpgraderFake() *updatableUpgraderFake {
	provisioner := pinnedUpgraderFake()
	provisioner.diff = &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{{
			Field:    "talos.workers",
			OldValue: "1",
			NewValue: "2",
			Category: clusterupdate.ChangeCategoryInPlace,
		}},
	}

	return provisioner
}

// recreationUpgraderFake rejects the pinned upgrade as needing recreation, so a
// real update takes the recreation path.
func recreationUpgraderFake() *updatableUpgraderFake {
	provisioner := pinnedUpgraderFake()
	provisioner.upgradeErr = clustererr.ErrRecreationRequired

	return provisioner
}

// decodeOneDocument asserts stdout holds exactly one JSON document and returns it.
func decodeOneDocument(t *testing.T, stdout string) map[string]any {
	t.Helper()

	decoder := json.NewDecoder(bytes.NewBufferString(stdout))

	var decoded map[string]any

	require.NoError(t, decoder.Decode(&decoded),
		"stdout must start with a JSON document: %q", stdout)
	require.False(t, decoder.More(), "stdout must hold exactly one JSON document: %q", stdout)

	return decoded
}

// A real update whose only work is the pinned in-place version upgrade finds no
// further changes; under --output json it still leaves its (empty) change
// summary as the one document, and the upgrade and "No changes" text on stderr.
func TestUpdateCommandNoChangeJSONLeavesOneDocumentOnStdout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	stdout, stderr := runUpdateCommandWith(t, pinnedUpgraderFake(), "--output", "json")

	decoded := decodeOneDocument(t, stdout)
	assert.InDelta(t, 0, decoded["totalChanges"], 0)
	assert.Contains(t, stderr, "distribution upgraded to pinned version v1.13.0")
	assert.Contains(t, stderr, "No changes detected")
}

// The in-place apply path: the change summary is the one document, while the
// "Applying changes" title and the success line go to stderr.
func TestUpdateCommandInPlaceApplyJSONLeavesOneDocumentOnStdout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	stdout, stderr := runUpdateCommandWith(t, inPlaceUpgraderFake(), "--output", "json", "--yes")

	decoded := decodeOneDocument(t, stdout)
	assert.InDelta(t, 1, decoded["totalChanges"], 0)
	assert.Contains(t, stderr, "Applying changes...")
	assert.Contains(t, stderr, "applied 1 changes successfully")
}

// The counterpart: in text mode the in-place apply keeps reporting on stdout.
func TestUpdateCommandInPlaceApplyTextReportsOnStdout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	stdout, stderr := runUpdateCommandWith(t, inPlaceUpgraderFake(), "--yes")

	assert.Contains(t, stdout, "Change summary")
	assert.Contains(t, stdout, "Applying changes...")
	assert.Contains(t, stdout, "applied 1 changes successfully")
	assert.NotContains(t, stderr, "Applying changes...")
}

// A pinned upgrade that needs recreation emits the recreation as the one
// document before it asks for confirmation; the recreation notice, the prompt
// and the (declined, stdin is not a terminal) cancellation stay off stdout.
//
//nolint:paralleltest // uses t.Chdir and replaces the process's stdout/stderr.
func TestUpdateCommandRecreationJSONLeavesOneDocumentOnStdout(t *testing.T) {
	stdout, stderr := runUpdateCommandWith(t, recreationUpgraderFake(), "--output", "json")

	decoded := decodeOneDocument(t, stdout)
	assert.InDelta(t, 1, decoded["totalChanges"], 0)

	recreate, ok := decoded["recreateRequired"].([]any)
	require.True(t, ok, "recreateRequired must be a list: %v", decoded["recreateRequired"])
	require.Len(t, recreate, 1)

	change, ok := recreate[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "distribution.version", change["field"])
	assert.Equal(t, "v1.12.0", change["oldValue"])
	assert.Equal(t, "v1.13.0", change["newValue"])

	assert.Contains(t, stderr, "requires cluster recreation")
	assert.Contains(t, stderr, "Update cancelled")
}

// The counterpart: in text mode the recreation notice and prompt stay on
// stdout and no JSON is written.
//
//nolint:paralleltest // uses t.Chdir and replaces the process's stdout/stderr.
func TestUpdateCommandRecreationTextReportsOnStdout(t *testing.T) {
	stdout, _ := runUpdateCommandWith(t, recreationUpgraderFake())

	assert.Contains(t, stdout, "requires cluster recreation")
	assert.Contains(t, stdout, "Update cancelled")
	assert.NotContains(t, stdout, "recreateRequired")
}

// The provisioner factory carries the context's provisioner log writer, which
// a --output json update points at stderr so Talos progress cannot reach the
// document's stream.
func TestDefaultProvisionerFactoryCarriesProvisionerLogWriter(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	factory := cluster.ExportDefaultProvisionerFactory(
		&localregistry.Context{ProvisionerLogWriter: &logs},
	)

	assert.Same(t, &logs, factory.LogWriter)
}
