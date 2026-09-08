package eksprovisioner_test

import (
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// TestManagedNodegroupCreationReservesTagCapacity accounts for metadata
// propagation, eksctl identifiers, the ownership marker and generated Name tags.
func TestManagedNodegroupCreationReservesTagCapacity(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name                     string
		metadata, group          int
		overlap, marker, wantErr bool
	}{
		{name: "group_boundary", group: 43},
		{name: "group_overflow", group: 44, wantErr: true},
		{name: "metadata_boundary", metadata: 1, group: 41},
		{name: "metadata_overflow", metadata: 1, group: 42, wantErr: true},
		{name: "overlap_boundary", metadata: 1, group: 42, overlap: true},
		{name: "overlap_overflow", metadata: 1, group: 43, overlap: true, wantErr: true},
		{name: "existing_marker", group: 43, marker: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			config := creationConfigWithTags(
				t,
				testCase.metadata,
				testCase.group,
				testCase.overlap,
				testCase.marker,
			)
			provisioner, runner, _ := newCreationProvisioner(t, config, allowCreation)

			for _, dryRun := range []bool{false, true} {
				if testCase.wantErr {
					_, err := runCreationUpdate(t, provisioner, dryRun)
					require.ErrorContains(t, err, "tag budget")
					assert.Zero(t, runner.creates)
					assert.Zero(t, runner.scales)
				} else {
					result, err := provisioner.DiffConfig(
						t.Context(),
						"",
						&v1alpha1.ClusterSpec{},
						&v1alpha1.ClusterSpec{},
					)
					require.NoError(t, err)
					assert.True(t, result.HasInPlaceChanges())
				}
			}
		})
	}
}

func creationConfigWithTags(
	t *testing.T,
	metadataCount, groupCount int,
	overlap, marker bool,
) string {
	t.Helper()

	var config map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(creationConfig), &config))
	metadata, valid := config["metadata"].(map[string]any)
	require.True(t, valid)

	metadataTags := make(map[string]any)
	for index := range metadataCount {
		metadataTags[fmt.Sprintf("metadata-%d", index)] = "value"
	}

	metadata["tags"] = metadataTags
	groups, valid := config["managedNodeGroups"].([]any)
	require.True(t, valid)
	group, valid := groups[0].(map[string]any)
	require.True(t, valid)

	tags := make(map[string]any)
	for index := range groupCount {
		tags[fmt.Sprintf("tag-%d", index)] = "value"
	}

	if overlap {
		delete(tags, "tag-0")
		tags["metadata-0"] = "override"
	}

	if marker {
		tags["ksail.io/nodegroup-creation-id"] = "old-marker"
	}

	group["tags"] = tags
	data, err := yaml.Marshal(config)
	require.NoError(t, err)

	return string(data)
}

// TestExistingManagedNodegroupBypassesCreationTagBudget preserves scaling for
// existing groups even when their source declarations exceed the creation reserve.
func TestExistingManagedNodegroupBypassesCreationTagBudget(t *testing.T) {
	t.Parallel()
	provisioner, runner, _ := newCreationProvisioner(
		t,
		creationConfigWithTags(t, 0, 50, false, false),
		allowCreation,
	)
	live := activeCreationGroup("workers")
	live.DesiredCap = 2
	runner.live = []eksctl.NodegroupSummary{live}
	result, err := provisioner.DiffConfig(
		t.Context(),
		"",
		&v1alpha1.ClusterSpec{},
		&v1alpha1.ClusterSpec{},
	)
	require.NoError(t, err)
	assert.True(t, result.HasInPlaceChanges())
	_, err = runCreationUpdate(t, provisioner, false)
	require.NoError(t, err)
	assert.Equal(t, 1, runner.scales)
	assert.Zero(t, runner.creates)
}

// TestCreationTagBudgetPreflightsAllAdditions prevents earlier scaling changes
// from being applied before discovering an oversized later addition.
func TestCreationTagBudgetPreflightsAllAdditions(t *testing.T) {
	t.Parallel()

	var config map[string]any
	require.NoError(
		t,
		yaml.Unmarshal([]byte(creationConfigWithTags(t, 0, 44, false, false)), &config),
	)
	groups, ok := config["managedNodeGroups"].([]any)
	require.True(t, ok)

	config["managedNodeGroups"] = append([]any{map[string]any{
		"name": "existing", "desiredCapacity": 2, "minSize": 1, "maxSize": 3,
	}}, groups...)
	data, err := yaml.Marshal(config)
	require.NoError(t, err)
	provisioner, runner, _ := newCreationProvisioner(t, string(data), allowCreation)
	runner.live = []eksctl.NodegroupSummary{activeCreationGroup("existing")}

	for _, dryRun := range []bool{false, true} {
		_, err = runCreationUpdate(t, provisioner, dryRun)
		require.ErrorContains(t, err, "tag budget")
		assert.Zero(t, runner.scales)
		assert.Zero(t, runner.creates)
	}
}

// TestCreationMarkerReplacesExistingMarker reserves one slot while preventing
// a caller-provided value from claiming ownership of the newly created group.
func TestCreationMarkerReplacesExistingMarker(t *testing.T) {
	t.Parallel()
	provisioner, runner, _ := newCreationProvisioner(
		t,
		creationConfigWithTags(t, 0, 43, false, true),
		allowCreation,
	)
	runner.create = func(config map[string]any) error {
		groups, ok := config["managedNodeGroups"].([]any)
		require.True(t, ok)
		group, ok := groups[0].(map[string]any)
		require.True(t, ok)
		tags, ok := group["tags"].(map[string]any)
		require.True(t, ok)
		assert.NotEmpty(t, tags["ksail.io/nodegroup-creation-id"])
		assert.NotEqual(t, "old-marker", tags["ksail.io/nodegroup-creation-id"])

		runner.live = []eksctl.NodegroupSummary{activeCreationGroup("workers")}

		return nil
	}
	_, err := runCreationUpdate(t, provisioner, false)
	require.NoError(t, err)
	assert.Equal(t, 1, runner.creates)
}
