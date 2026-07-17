package eksprovisioner_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The wrapper must satisfy the orchestrator's Updater capability; the base
// provisioner must not, so the capability stays opt-in.
var (
	_ clusterprovisioner.Updater = (*eksprovisioner.UpdatableProvisioner)(nil)

	errScaleBoom = errors.New("scale exploded")
)

// clusterupdateOptions builds UpdateOptions with the given dry-run state.
func clusterupdateOptions(dryRun bool) clusterupdate.UpdateOptions {
	return clusterupdate.UpdateOptions{DryRun: dryRun}
}

const updateTestConfig = `apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: ksail-test
  region: us-east-1
managedNodeGroups:
  - name: ng-1
    instanceType: t3.medium
    desiredCapacity: 3
    minSize: 1
    maxSize: 3
`

const liveNodegroupsJSON = `[{"Cluster":"ksail-test","Name":"ng-1","Status":"ACTIVE",` +
	`"DesiredCapacity":2,"MinSize":1,"MaxSize":3,"InstanceType":"t3.medium",` +
	`"NodeGroupType":"managed"}]`

// writeUpdateTestConfig writes an eksctl.yaml into a temp dir and returns its path.
func writeUpdateTestConfig(t *testing.T, content string) string {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "eksctl.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))

	return configPath
}

func newUpdatableProvisioner(
	t *testing.T,
	responses map[string][]response,
	configPath string,
) (*eksprovisioner.UpdatableProvisioner, *scriptedRunner) {
	t.Helper()

	runner := &scriptedRunner{t: t, responses: responses}

	client := eksctlclient.NewClient(
		eksctlclient.WithBinary(testBinary),
		eksctlclient.WithRunner(runner),
	)

	prov, err := eksprovisioner.NewProvisioner(
		"ksail-test", "us-east-1", configPath, client, nil,
	)
	require.NoError(t, err)

	return eksprovisioner.NewUpdatableProvisioner(prov), runner
}

func TestBaseProvisionerDoesNotImplementUpdater(t *testing.T) {
	t.Parallel()

	var prov any = &eksprovisioner.Provisioner{}

	_, ok := prov.(clusterprovisioner.Updater)
	assert.False(t, ok, "the base EKS provisioner must not expose Updater; the capability is gated")
}

func TestDiffConfig_ScalingChangeIsInPlace(t *testing.T) {
	t.Parallel()

	configPath := writeUpdateTestConfig(t, updateTestConfig)
	prov, _ := newUpdatableProvisioner(t, map[string][]response{
		"get nodegroup": {{stdout: []byte(liveNodegroupsJSON)}},
	}, configPath)

	diff, err := prov.DiffConfig(
		t.Context(), "", &v1alpha1.ClusterSpec{}, &v1alpha1.ClusterSpec{},
	)
	require.NoError(t, err)

	require.Len(t, diff.InPlaceChanges, 1)
	assert.Empty(t, diff.RecreateRequired)
	assert.Equal(t, "eks.managedNodeGroups[ng-1].desiredCapacity", diff.InPlaceChanges[0].Field)
	assert.Equal(t, "2", diff.InPlaceChanges[0].OldValue)
	assert.Equal(t, "3", diff.InPlaceChanges[0].NewValue)
}

func TestDiffConfig_AddedNodegroupRequiresRecreate(t *testing.T) {
	t.Parallel()

	configPath := writeUpdateTestConfig(t, updateTestConfig+`  - name: ng-2
    desiredCapacity: 1
`)
	prov, _ := newUpdatableProvisioner(t, map[string][]response{
		"get nodegroup": {{stdout: []byte(liveNodegroupsJSON)}},
	}, configPath)

	diff, err := prov.DiffConfig(
		t.Context(), "", &v1alpha1.ClusterSpec{}, &v1alpha1.ClusterSpec{},
	)
	require.NoError(t, err)

	require.Len(t, diff.RecreateRequired, 1)
	assert.Equal(t, "eks.managedNodeGroups[ng-2]", diff.RecreateRequired[0].Field)
}

func TestDiffConfig_RemovedNodegroupRequiresRecreate(t *testing.T) {
	t.Parallel()

	liveTwoGroups := `[{"Name":"ng-1","DesiredCapacity":3,"MinSize":1,"MaxSize":3,` +
		`"NodeGroupType":"managed"},{"Name":"ng-old","DesiredCapacity":1,"MinSize":1,` +
		`"MaxSize":1,"NodeGroupType":"managed"}]`

	configPath := writeUpdateTestConfig(t, updateTestConfig)
	prov, _ := newUpdatableProvisioner(t, map[string][]response{
		"get nodegroup": {{stdout: []byte(liveTwoGroups)}},
	}, configPath)

	diff, err := prov.DiffConfig(
		t.Context(), "", &v1alpha1.ClusterSpec{}, &v1alpha1.ClusterSpec{},
	)
	require.NoError(t, err)

	assert.Empty(t, diff.InPlaceChanges)
	require.Len(t, diff.RecreateRequired, 1)
	assert.Equal(t, "eks.managedNodeGroups[ng-old]", diff.RecreateRequired[0].Field)
}

func TestDiffConfig_UnmanagedLiveGroupIsIgnored(t *testing.T) {
	t.Parallel()

	liveWithUnmanaged := `[{"Name":"ng-1","DesiredCapacity":3,"MinSize":1,"MaxSize":3,` +
		`"NodeGroupType":"managed"},{"Name":"legacy","DesiredCapacity":1,"MinSize":1,` +
		`"MaxSize":1,"NodeGroupType":"unmanaged"}]`

	configPath := writeUpdateTestConfig(t, updateTestConfig)
	prov, _ := newUpdatableProvisioner(t, map[string][]response{
		"get nodegroup": {{stdout: []byte(liveWithUnmanaged)}},
	}, configPath)

	diff, err := prov.DiffConfig(
		t.Context(), "", &v1alpha1.ClusterSpec{}, &v1alpha1.ClusterSpec{},
	)
	require.NoError(t, err)

	assert.Empty(t, diff.InPlaceChanges)
	assert.Empty(t, diff.RecreateRequired)
}

func TestDiffConfig_MissingConfigFileYieldsNoChanges(t *testing.T) {
	t.Parallel()

	prov, runner := newUpdatableProvisioner(t, map[string][]response{},
		filepath.Join(t.TempDir(), "absent.yaml"))

	diff, err := prov.DiffConfig(
		t.Context(), "", &v1alpha1.ClusterSpec{}, &v1alpha1.ClusterSpec{},
	)
	require.NoError(t, err)

	assert.Zero(t, diff.TotalChanges())
	assert.Empty(t, runner.calls)
}

func TestUpdate_AppliesNodegroupScaling(t *testing.T) {
	t.Parallel()

	configPath := writeUpdateTestConfig(t, updateTestConfig)
	prov, runner := newUpdatableProvisioner(t, map[string][]response{
		// One list for the diff, one for the apply pass.
		"get nodegroup": {
			{stdout: []byte(liveNodegroupsJSON)},
			{stdout: []byte(liveNodegroupsJSON)},
		},
		"scale nodegroup": {{}},
	}, configPath)

	result, err := prov.Update(
		t.Context(), "", &v1alpha1.ClusterSpec{}, &v1alpha1.ClusterSpec{},
		clusterupdateOptions(false),
	)
	require.NoError(t, err)

	require.Len(t, result.AppliedChanges, 1)
	assert.Empty(t, result.FailedChanges)

	scaleCall := lastCallWithPrefix(t, runner, "scale")
	assert.Equal(t, []string{
		"scale", "nodegroup",
		"--cluster", "ksail-test",
		"--name", "ng-1",
		"--nodes", "3",
		"--region", "us-east-1",
	}, scaleCall)
}

func TestUpdate_DryRunDoesNotScale(t *testing.T) {
	t.Parallel()

	configPath := writeUpdateTestConfig(t, updateTestConfig)
	prov, runner := newUpdatableProvisioner(t, map[string][]response{
		"get nodegroup": {{stdout: []byte(liveNodegroupsJSON)}},
	}, configPath)

	result, err := prov.Update(
		t.Context(), "", &v1alpha1.ClusterSpec{}, &v1alpha1.ClusterSpec{},
		clusterupdateOptions(true),
	)
	require.NoError(t, err)

	require.Len(t, result.InPlaceChanges, 1)
	assert.Empty(t, result.AppliedChanges)

	for _, call := range runner.calls {
		assert.NotEqual(t, "scale", call[0], "dry run must not scale")
	}
}

func TestUpdate_RecreateRequiredErrors(t *testing.T) {
	t.Parallel()

	configPath := writeUpdateTestConfig(t, updateTestConfig+`  - name: ng-2
    desiredCapacity: 1
`)
	prov, _ := newUpdatableProvisioner(t, map[string][]response{
		"get nodegroup": {{stdout: []byte(liveNodegroupsJSON)}},
	}, configPath)

	_, err := prov.Update(
		t.Context(), "", &v1alpha1.ClusterSpec{}, &v1alpha1.ClusterSpec{},
		clusterupdateOptions(false),
	)
	require.ErrorIs(t, err, clustererr.ErrRecreationRequired)
}

func TestUpdate_ScaleFailureIsRecorded(t *testing.T) {
	t.Parallel()

	configPath := writeUpdateTestConfig(t, updateTestConfig)
	prov, _ := newUpdatableProvisioner(t, map[string][]response{
		"get nodegroup": {
			{stdout: []byte(liveNodegroupsJSON)},
			{stdout: []byte(liveNodegroupsJSON)},
		},
		"scale nodegroup": {{err: errScaleBoom}},
	}, configPath)

	result, err := prov.Update(
		t.Context(), "", &v1alpha1.ClusterSpec{}, &v1alpha1.ClusterSpec{},
		clusterupdateOptions(false),
	)
	require.Error(t, err)
	require.Len(t, result.FailedChanges, 1)
	assert.Empty(t, result.AppliedChanges)
}

func TestUpdate_NoChangesIsANoOp(t *testing.T) {
	t.Parallel()

	liveMatchingConfig := `[{"Name":"ng-1","DesiredCapacity":3,"MinSize":1,"MaxSize":3,` +
		`"NodeGroupType":"managed"}]`

	configPath := writeUpdateTestConfig(t, updateTestConfig)
	prov, runner := newUpdatableProvisioner(t, map[string][]response{
		"get nodegroup": {
			{stdout: []byte(liveMatchingConfig)},
			{stdout: []byte(liveMatchingConfig)},
		},
	}, configPath)

	result, err := prov.Update(
		t.Context(), "", &v1alpha1.ClusterSpec{}, &v1alpha1.ClusterSpec{},
		clusterupdateOptions(false),
	)
	require.NoError(t, err)

	assert.Empty(t, result.AppliedChanges)
	assert.Empty(t, result.FailedChanges)

	for _, call := range runner.calls {
		assert.NotEqual(t, "scale", call[0], "no-op update must not scale")
	}
}

func TestDiffConfig_InstanceTypeChangeRequiresRecreate(t *testing.T) {
	t.Parallel()

	liveOtherType := `[{"Name":"ng-1","DesiredCapacity":3,"MinSize":1,"MaxSize":3,` +
		`"InstanceType":"t3.large","NodeGroupType":"managed"}]`

	configPath := writeUpdateTestConfig(t, updateTestConfig)
	prov, _ := newUpdatableProvisioner(t, map[string][]response{
		"get nodegroup": {{stdout: []byte(liveOtherType)}},
	}, configPath)

	diff, err := prov.DiffConfig(
		t.Context(), "", &v1alpha1.ClusterSpec{}, &v1alpha1.ClusterSpec{},
	)
	require.NoError(t, err)

	require.Len(t, diff.RecreateRequired, 1)
	assert.Equal(t, "eks.managedNodeGroups[ng-1].instanceType", diff.RecreateRequired[0].Field)
	assert.Equal(t, "t3.large", diff.RecreateRequired[0].OldValue)
	assert.Equal(t, "t3.medium", diff.RecreateRequired[0].NewValue)
}

func TestDiffConfig_EmptyDeclaredListFlagsLiveGroupsAsRemovals(t *testing.T) {
	t.Parallel()

	noGroupsConfig := `apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: ksail-test
  region: us-east-1
`

	configPath := writeUpdateTestConfig(t, noGroupsConfig)
	prov, _ := newUpdatableProvisioner(t, map[string][]response{
		"get nodegroup": {{stdout: []byte(liveNodegroupsJSON)}},
	}, configPath)

	diff, err := prov.DiffConfig(
		t.Context(), "", &v1alpha1.ClusterSpec{}, &v1alpha1.ClusterSpec{},
	)
	require.NoError(t, err)

	require.Len(t, diff.RecreateRequired, 1)
	assert.Equal(t, "eks.managedNodeGroups[ng-1]", diff.RecreateRequired[0].Field)
}

func TestUpdate_AutoscalerOwnedDesiredIsClampedIntoNewBounds(t *testing.T) {
	t.Parallel()

	// desiredCapacity deliberately undeclared: the autoscaler owns the size.
	// Raising minSize above the live desired count must clamp --nodes up to
	// the new minimum rather than replaying a now-invalid live value.
	minOnlyConfig := `apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: ksail-test
  region: us-east-1
managedNodeGroups:
  - name: ng-1
    minSize: 4
    maxSize: 6
`

	liveBelowNewMin := `[{"Name":"ng-1","DesiredCapacity":2,"MinSize":1,"MaxSize":3,` +
		`"NodeGroupType":"managed"}]`

	configPath := writeUpdateTestConfig(t, minOnlyConfig)
	prov, runner := newUpdatableProvisioner(t, map[string][]response{
		"get nodegroup": {
			{stdout: []byte(liveBelowNewMin)},
			{stdout: []byte(liveBelowNewMin)},
		},
		"scale nodegroup": {{}},
	}, configPath)

	_, err := prov.Update(
		t.Context(), "", &v1alpha1.ClusterSpec{}, &v1alpha1.ClusterSpec{},
		clusterupdateOptions(false),
	)
	require.NoError(t, err)

	scaleCall := lastCallWithPrefix(t, runner, "scale")
	assert.Equal(t, []string{
		"scale", "nodegroup",
		"--cluster", "ksail-test",
		"--name", "ng-1",
		"--nodes", "4",
		"--nodes-min", "4",
		"--nodes-max", "6",
		"--region", "us-east-1",
	}, scaleCall)
}

func TestUpdatableProvisionerIsComponentDetectorAware(t *testing.T) {
	t.Parallel()

	var prov any = &eksprovisioner.UpdatableProvisioner{}

	_, ok := prov.(clusterprovisioner.ComponentDetectorAware)
	assert.True(t, ok, "the orchestrator injects the detector via ComponentDetectorAware")
}

func TestGetCurrentConfig_MarksComponentsUnknownWithoutDetector(t *testing.T) {
	t.Parallel()

	prov, _ := newUpdatableProvisioner(t, map[string][]response{},
		filepath.Join(t.TempDir(), "absent.yaml"))

	spec, _, err := prov.GetCurrentConfig(t.Context(), "update-test-no-state")
	require.NoError(t, err)

	assert.Equal(t, v1alpha1.CNI(clusterupdate.UnknownBaselineValue), spec.CNI)
	assert.Equal(t, v1alpha1.GitOpsEngine(clusterupdate.UnknownBaselineValue), spec.GitOpsEngine)
}

func TestGetCurrentConfig_ReturnsEKSDefaults(t *testing.T) {
	t.Parallel()

	prov, _ := newUpdatableProvisioner(t, map[string][]response{},
		filepath.Join(t.TempDir(), "absent.yaml"))

	spec, providerSpec, err := prov.GetCurrentConfig(t.Context(), "update-test-no-state")
	require.NoError(t, err)

	assert.Nil(t, providerSpec)
	assert.Equal(t, v1alpha1.DistributionEKS, spec.Distribution)
	assert.Equal(t, v1alpha1.ProviderAWS, spec.Provider)
}

// lastCallWithPrefix returns the last recorded runner call whose first arg matches.
func lastCallWithPrefix(t *testing.T, runner *scriptedRunner, first string) []string {
	t.Helper()

	for _, v := range slices.Backward(runner.calls) {
		if v[0] == first {
			return v
		}
	}

	t.Fatalf("no %q call recorded (calls=%v)", first, runner.calls)

	return nil
}
