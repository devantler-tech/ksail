package clusterupdate_test

import (
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errDiffComputation  = errors.New("diff computation failed")
	errRecreateRequired = errors.New("recreate required")
)

const fieldHetznerCPServerType = "provider.hetzner.controlPlaneServerType"

func TestUpdateResult_NoChangesIsNoOp(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()

	if result.TotalChanges() != 0 {
		t.Errorf("empty result should have 0 changes, got %d", result.TotalChanges())
	}

	if result.HasInPlaceChanges() {
		t.Error("empty result should not have in-place changes")
	}

	if result.HasRebootRequired() {
		t.Error("empty result should not have reboot-required changes")
	}

	if result.HasRecreateRequired() {
		t.Error("empty result should not have recreate-required changes")
	}

	if result.NeedsUserConfirmation() {
		t.Error("empty result should not need user confirmation")
	}
}

func TestUpdateResult_RecreateChangesAreDetected(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	result.RecreateRequired = append(result.RecreateRequired, clusterupdate.Change{
		Field:    "cluster.distribution",
		OldValue: "Vanilla",
		NewValue: "Talos",
		Category: clusterupdate.ChangeCategoryRecreateRequired,
		Reason:   "distribution change requires recreation",
	})

	if result.TotalChanges() != 1 {
		t.Errorf("result with recreate should have 1 change, got %d", result.TotalChanges())
	}

	if !result.HasRecreateRequired() {
		t.Error("result should have recreate-required changes")
	}

	// Recreate-required changes are not reflected in HasInPlaceChanges
	// or HasRebootRequired, but TotalChanges must still count them.
	if result.HasInPlaceChanges() {
		t.Error("result should not have in-place changes")
	}

	if result.HasRebootRequired() {
		t.Error("result should not have reboot-required changes")
	}
}

// TestApplyGitOpsLocalRegistryDefault_FluxEngine tests that Flux triggers the default.
func TestApplyGitOpsLocalRegistryDefault_FluxEngine(t *testing.T) {
	t.Parallel()

	spec := &v1alpha1.ClusterSpec{
		GitOpsEngine: v1alpha1.GitOpsEngineFlux,
	}

	clusterupdate.ApplyGitOpsLocalRegistryDefault(spec)

	assert.Equal(t, clusterupdate.DefaultLocalRegistryAddress, spec.LocalRegistry.Registry)
}

// TestApplyGitOpsLocalRegistryDefault_ArgoCDEngine tests that ArgoCD triggers the default.
func TestApplyGitOpsLocalRegistryDefault_ArgoCDEngine(t *testing.T) {
	t.Parallel()

	spec := &v1alpha1.ClusterSpec{
		GitOpsEngine: v1alpha1.GitOpsEngineArgoCD,
	}

	clusterupdate.ApplyGitOpsLocalRegistryDefault(spec)

	assert.Equal(t, clusterupdate.DefaultLocalRegistryAddress, spec.LocalRegistry.Registry)
}

// TestApplyGitOpsLocalRegistryDefault_NoEngine tests that no GitOps engine leaves registry empty.
func TestApplyGitOpsLocalRegistryDefault_NoEngine(t *testing.T) {
	t.Parallel()

	spec := &v1alpha1.ClusterSpec{
		GitOpsEngine: v1alpha1.GitOpsEngineNone,
	}

	clusterupdate.ApplyGitOpsLocalRegistryDefault(spec)

	assert.Empty(t, spec.LocalRegistry.Registry)
}

// TestApplyGitOpsLocalRegistryDefault_EmptyEngine tests that empty GitOps engine leaves registry empty.
func TestApplyGitOpsLocalRegistryDefault_EmptyEngine(t *testing.T) {
	t.Parallel()

	spec := &v1alpha1.ClusterSpec{
		GitOpsEngine: "",
	}

	clusterupdate.ApplyGitOpsLocalRegistryDefault(spec)

	assert.Empty(t, spec.LocalRegistry.Registry)
}

// TestApplyGitOpsLocalRegistryDefault_ExistingRegistry tests that existing registry is preserved.
func TestApplyGitOpsLocalRegistryDefault_ExistingRegistry(t *testing.T) {
	t.Parallel()

	existingRegistry := "custom.registry:8080"
	spec := &v1alpha1.ClusterSpec{
		GitOpsEngine: v1alpha1.GitOpsEngineFlux,
		LocalRegistry: v1alpha1.LocalRegistry{
			Registry: existingRegistry,
		},
	}

	clusterupdate.ApplyGitOpsLocalRegistryDefault(spec)

	assert.Equal(t, existingRegistry, spec.LocalRegistry.Registry)
}

// TestDefaultCurrentSpec_VanillaDocker verifies the default spec for Vanilla on Docker.
func TestDefaultCurrentSpec_VanillaDocker(t *testing.T) {
	t.Parallel()

	spec := clusterupdate.DefaultCurrentSpec(
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
	)

	require.NotNil(t, spec)
	assert.Equal(t, v1alpha1.DistributionVanilla, spec.Distribution)
	assert.Equal(t, v1alpha1.ProviderDocker, spec.Provider)
	assert.Equal(t, v1alpha1.CNIDefault, spec.CNI)
	assert.Equal(t, v1alpha1.CSIDefault, spec.CSI)
	assert.Equal(t, v1alpha1.MetricsServerDefault, spec.MetricsServer)
	assert.Equal(t, v1alpha1.LoadBalancerDefault, spec.LoadBalancer)
	assert.Equal(t, v1alpha1.CertManagerDisabled, spec.CertManager)
	assert.Equal(t, v1alpha1.PolicyEngineNone, spec.PolicyEngine)
	assert.Equal(t, v1alpha1.GitOpsEngineNone, spec.GitOpsEngine)
}

// TestDefaultCurrentSpec_K3sDocker verifies the default spec for K3s on Docker.
func TestDefaultCurrentSpec_K3sDocker(t *testing.T) {
	t.Parallel()

	spec := clusterupdate.DefaultCurrentSpec(
		v1alpha1.DistributionK3s,
		v1alpha1.ProviderDocker,
	)

	require.NotNil(t, spec)
	assert.Equal(t, v1alpha1.DistributionK3s, spec.Distribution)
	assert.Equal(t, v1alpha1.ProviderDocker, spec.Provider)
}

// TestDefaultCurrentSpec_TalosHetzner verifies the default spec for Talos on Hetzner.
func TestDefaultCurrentSpec_TalosHetzner(t *testing.T) {
	t.Parallel()

	spec := clusterupdate.DefaultCurrentSpec(
		v1alpha1.DistributionTalos,
		v1alpha1.ProviderHetzner,
	)

	require.NotNil(t, spec)
	assert.Equal(t, v1alpha1.DistributionTalos, spec.Distribution)
	assert.Equal(t, v1alpha1.ProviderHetzner, spec.Provider)
}

// TestMarkComponentsUnknown sets the sentinel on every detector-derived component.
func TestMarkComponentsUnknown(t *testing.T) {
	t.Parallel()

	spec := clusterupdate.DefaultCurrentSpec(
		v1alpha1.DistributionTalos,
		v1alpha1.ProviderHetzner,
	)

	clusterupdate.MarkComponentsUnknown(spec)

	sentinel := clusterupdate.UnknownBaselineValue
	assert.Equal(t, sentinel, string(spec.CNI))
	assert.Equal(t, sentinel, string(spec.CSI))
	assert.Equal(t, sentinel, string(spec.MetricsServer))
	assert.Equal(t, sentinel, string(spec.LoadBalancer))
	assert.Equal(t, sentinel, string(spec.CertManager))
	assert.Equal(t, sentinel, string(spec.PolicyEngine))
	assert.Equal(t, sentinel, string(spec.GitOpsEngine))

	// Distribution and provider must remain known.
	assert.Equal(t, v1alpha1.DistributionTalos, spec.Distribution)
	assert.Equal(t, v1alpha1.ProviderHetzner, spec.Provider)
}

// TestMarkComponentsUnknown_NilIsSafe ensures the helper tolerates a nil spec.
func TestMarkComponentsUnknown_NilIsSafe(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, func() { clusterupdate.MarkComponentsUnknown(nil) })
}

// TestHasUnknownBaseline reports unknown-baseline entries.
func TestHasUnknownBaseline(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	assert.False(t, result.HasUnknownBaseline())

	result.UnknownBaseline = append(result.UnknownBaseline, clusterupdate.Change{
		Field:    "cluster.cni",
		OldValue: clusterupdate.UnknownBaselineValue,
		NewValue: "Cilium",
		Category: clusterupdate.ChangeCategoryUnknown,
	})

	assert.True(t, result.HasUnknownBaseline())
	// Unknown entries are informational and never counted as applicable changes.
	assert.Zero(t, result.TotalChanges())
}

// TestHasFailedChanges reports whether any change failed to apply. A non-empty
// FailedChanges set must be treated as a failed update (issue #4935).
func TestHasFailedChanges(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	assert.False(t, result.HasFailedChanges())

	result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
		Field:    "talos.config",
		Category: clusterupdate.ChangeCategoryInPlace,
		Reason:   "apply control-plane config: rpc error",
	})

	assert.True(t, result.HasFailedChanges())
	// Failed changes are execution outcomes, not detected diff changes, so they
	// are not counted by TotalChanges.
	assert.Zero(t, result.TotalChanges())
}

// TestChangeCategory_String tests the string representation of change categories.
func TestChangeCategory_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		category clusterupdate.ChangeCategory
		want     string
	}{
		{"in-place", clusterupdate.ChangeCategoryInPlace, "in-place"},
		{"reboot-required", clusterupdate.ChangeCategoryRebootRequired, "reboot-required"},
		{"recreate-required", clusterupdate.ChangeCategoryRecreateRequired, "recreate-required"},
		{"wipe-required", clusterupdate.ChangeCategoryWipeRequired, "wipe-required"},
		{"unknown-category", clusterupdate.ChangeCategoryUnknown, "unknown"},
		{"out-of-range", clusterupdate.ChangeCategory(999), "unknown"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.want, testCase.category.String())
		})
	}
}

// TestNewDiffResult_BothNil tests that NewDiffResult returns false when both specs are nil.
func TestNewDiffResult_BothNil(t *testing.T) {
	t.Parallel()

	result, ok := clusterupdate.NewDiffResult(nil, nil)

	require.NotNil(t, result)
	assert.False(t, ok)
}

// TestNewDiffResult_OldNil tests that NewDiffResult returns false when old spec is nil.
func TestNewDiffResult_OldNil(t *testing.T) {
	t.Parallel()

	result, ok := clusterupdate.NewDiffResult(nil, &v1alpha1.ClusterSpec{})

	require.NotNil(t, result)
	assert.False(t, ok)
}

// TestNewDiffResult_NewNil tests that NewDiffResult returns false when new spec is nil.
func TestNewDiffResult_NewNil(t *testing.T) {
	t.Parallel()

	result, ok := clusterupdate.NewDiffResult(&v1alpha1.ClusterSpec{}, nil)

	require.NotNil(t, result)
	assert.False(t, ok)
}

// TestNewDiffResult_BothValid tests that NewDiffResult returns true when both specs are valid.
func TestNewDiffResult_BothValid(t *testing.T) {
	t.Parallel()

	result, ok := clusterupdate.NewDiffResult(&v1alpha1.ClusterSpec{}, &v1alpha1.ClusterSpec{})

	require.NotNil(t, result)
	assert.True(t, ok)
}

// TestNewUpdateResultFromDiff tests seeding a result from a diff.
func TestNewUpdateResultFromDiff(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field:    "cluster.cni",
		OldValue: "cilium",
		NewValue: "flannel",
		Category: clusterupdate.ChangeCategoryInPlace,
		Reason:   "CNI change is in-place",
	})
	diff.RebootRequired = append(diff.RebootRequired, clusterupdate.Change{
		Field:    "talos.kernel_args",
		OldValue: "",
		NewValue: "console=ttyS0",
		Category: clusterupdate.ChangeCategoryRebootRequired,
		Reason:   "kernel args require reboot",
	})

	result := clusterupdate.NewUpdateResultFromDiff(diff)

	require.NotNil(t, result)
	assert.Equal(t, diff.InPlaceChanges, result.InPlaceChanges)
	assert.Equal(t, diff.RebootRequired, result.RebootRequired)
	assert.Equal(t, diff.RecreateRequired, result.RecreateRequired)
	assert.NotNil(t, result.AppliedChanges)
	assert.NotNil(t, result.FailedChanges)
	assert.Empty(t, result.AppliedChanges)
	assert.Empty(t, result.FailedChanges)
}

// TestUpdateResult_AllChanges tests that AllChanges aggregates all change categories.
func TestUpdateResult_AllChanges(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	result.InPlaceChanges = append(result.InPlaceChanges, clusterupdate.Change{
		Field:    "cluster.cni",
		Category: clusterupdate.ChangeCategoryInPlace,
	})
	result.RebootRequired = append(result.RebootRequired, clusterupdate.Change{
		Field:    "talos.kernel_args",
		Category: clusterupdate.ChangeCategoryRebootRequired,
	})
	result.RecreateRequired = append(result.RecreateRequired, clusterupdate.Change{
		Field:    "cluster.distribution",
		Category: clusterupdate.ChangeCategoryRecreateRequired,
	})

	all := result.AllChanges()

	assert.Len(t, all, 3)
	assert.Contains(t, all, result.InPlaceChanges[0])
	assert.Contains(t, all, result.RebootRequired[0])
	assert.Contains(t, all, result.RecreateRequired[0])
}

// TestUpdateResult_AllChanges_Empty tests that AllChanges returns empty slice for empty result.
func TestUpdateResult_AllChanges_Empty(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	all := result.AllChanges()

	assert.NotNil(t, all)
	assert.Empty(t, all)
}

// TestPrepareUpdate_DiffError tests that PrepareUpdate returns diff error immediately.
func TestPrepareUpdate_DiffError(t *testing.T) {
	t.Parallel()

	opts := clusterupdate.UpdateOptions{}

	result, shouldContinue, err := clusterupdate.PrepareUpdate(
		nil,
		errDiffComputation,
		opts,
		errRecreateRequired,
	)

	assert.Nil(t, result)
	assert.False(t, shouldContinue)
	require.Error(t, err)
	assert.ErrorIs(t, err, errDiffComputation)
}

// TestPrepareUpdate_DryRun tests that PrepareUpdate returns diff immediately in dry-run mode.
func TestPrepareUpdate_DryRun(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field: "cluster.cni",
	})
	opts := clusterupdate.UpdateOptions{DryRun: true}

	result, shouldContinue, err := clusterupdate.PrepareUpdate(diff, nil, opts, errRecreateRequired)

	assert.Same(t, diff, result)
	assert.False(t, shouldContinue)
	require.NoError(t, err)
}

// TestPrepareUpdate_RecreateRequired tests that PrepareUpdate returns error for recreate-required changes.
func TestPrepareUpdate_RecreateRequired(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.RecreateRequired = append(diff.RecreateRequired, clusterupdate.Change{
		Field: "cluster.distribution",
	})
	opts := clusterupdate.UpdateOptions{}

	result, shouldContinue, err := clusterupdate.PrepareUpdate(diff, nil, opts, errRecreateRequired)

	require.NotNil(t, result)
	assert.False(t, shouldContinue)
	require.Error(t, err)
	assert.ErrorIs(t, err, errRecreateRequired)
}

// TestPrepareUpdate_Success tests that PrepareUpdate returns true for valid in-place changes.
func TestPrepareUpdate_Success(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field: "cluster.cni",
	})
	opts := clusterupdate.UpdateOptions{}

	result, shouldContinue, err := clusterupdate.PrepareUpdate(diff, nil, opts, errRecreateRequired)

	require.NotNil(t, result)
	assert.True(t, shouldContinue)
	require.NoError(t, err)
	assert.Equal(t, diff.InPlaceChanges, result.InPlaceChanges)
}

// TestPrepareUpdate_NoChanges tests that PrepareUpdate succeeds with empty diff.
func TestPrepareUpdate_NoChanges(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	opts := clusterupdate.UpdateOptions{}

	result, shouldContinue, err := clusterupdate.PrepareUpdate(diff, nil, opts, errRecreateRequired)

	require.NotNil(t, result)
	assert.True(t, shouldContinue)
	require.NoError(t, err)
}

// TestUpdateResult_WipeRequiredChangesAreDetected tests that wipe-required changes are properly detected.
func TestUpdateResult_WipeRequiredChangesAreDetected(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	result.WipeRequired = append(result.WipeRequired, clusterupdate.Change{
		Field:    "talos.volumeConfig.encryption",
		OldValue: "",
		NewValue: "LUKS2",
		Category: clusterupdate.ChangeCategoryWipeRequired,
		Reason:   "disk encryption requires partition wipe",
	})

	assert.Equal(t, 1, result.TotalChanges())
	assert.True(t, result.HasWipeRequired())
	assert.True(t, result.NeedsUserConfirmation())
	assert.False(t, result.HasInPlaceChanges())
	assert.False(t, result.HasRebootRequired())
	assert.False(t, result.HasRecreateRequired())
}

// TestUpdateResult_AllChanges_IncludesWipeRequired tests that AllChanges includes wipe-required changes.
func TestUpdateResult_AllChanges_IncludesWipeRequired(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	result.WipeRequired = append(result.WipeRequired, clusterupdate.Change{
		Field:    "talos.volumeConfig.encryption",
		Category: clusterupdate.ChangeCategoryWipeRequired,
	})

	all := result.AllChanges()
	assert.Len(t, all, 1)
	assert.Contains(t, all, result.WipeRequired[0])
}

// TestNewUpdateResultFromDiff_CopiesWipeRequired tests that NewUpdateResultFromDiff copies WipeRequired.
func TestNewUpdateResultFromDiff_CopiesWipeRequired(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.WipeRequired = append(diff.WipeRequired, clusterupdate.Change{
		Field:    "talos.volumeConfig.encryption",
		Category: clusterupdate.ChangeCategoryWipeRequired,
	})

	result := clusterupdate.NewUpdateResultFromDiff(diff)

	require.NotNil(t, result)
	assert.Equal(t, diff.WipeRequired, result.WipeRequired)
}

// TestPrepareUpdate_WipeRequiredBlocksWithoutForce tests that wipe-required changes block without --force.
func TestPrepareUpdate_WipeRequiredBlocksWithoutForce(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.WipeRequired = append(diff.WipeRequired, clusterupdate.Change{
		Field: "talos.volumeConfig.encryption",
	})
	opts := clusterupdate.UpdateOptions{}

	result, shouldContinue, err := clusterupdate.PrepareUpdate(diff, nil, opts, errRecreateRequired)

	require.NotNil(t, result)
	assert.False(t, shouldContinue)
	require.Error(t, err)
	assert.ErrorIs(t, err, clusterupdate.ErrWipeRequired)
}

// TestPrepareUpdate_WipeRequiredAllowedWithForce tests that wipe-required changes proceed with --force.
func TestPrepareUpdate_WipeRequiredAllowedWithForce(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.WipeRequired = append(diff.WipeRequired, clusterupdate.Change{
		Field: "talos.volumeConfig.encryption",
	})
	opts := clusterupdate.UpdateOptions{Force: true}

	result, shouldContinue, err := clusterupdate.PrepareUpdate(diff, nil, opts, errRecreateRequired)

	require.NotNil(t, result)
	assert.True(t, shouldContinue)
	require.NoError(t, err)
}

// TestPrepareUpdate_RollingRecreateBlocksWithoutConsent tests that rolling-recreate
// changes block without explicit consent.
func TestPrepareUpdate_RollingRecreateBlocksWithoutConsent(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.RollingRecreate = append(diff.RollingRecreate, clusterupdate.Change{
		Field: fieldHetznerCPServerType,
	})
	opts := clusterupdate.UpdateOptions{}

	result, shouldContinue, err := clusterupdate.PrepareUpdate(diff, nil, opts, errRecreateRequired)

	require.NotNil(t, result)
	assert.False(t, shouldContinue)
	require.Error(t, err)
	assert.ErrorIs(t, err, clusterupdate.ErrRollingRecreateRequired)
}

// TestPrepareUpdate_RollingRecreateNotAuthorizedByForceAlone verifies that Force
// alone does not authorize rolling node replacement: the dedicated
// AllowRollingRecreate gate is required so confirming a wipe never implicitly
// triggers a rolling replacement (and vice versa).
func TestPrepareUpdate_RollingRecreateNotAuthorizedByForceAlone(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.RollingRecreate = append(diff.RollingRecreate, clusterupdate.Change{
		Field: fieldHetznerCPServerType,
	})
	opts := clusterupdate.UpdateOptions{Force: true}

	result, shouldContinue, err := clusterupdate.PrepareUpdate(diff, nil, opts, errRecreateRequired)

	require.NotNil(t, result)
	assert.False(t, shouldContinue)
	require.Error(t, err)
	assert.ErrorIs(t, err, clusterupdate.ErrRollingRecreateRequired)
}

// TestPrepareUpdate_RollingRecreateAllowedWithConsent tests that rolling-recreate
// changes proceed when AllowRollingRecreate is set.
func TestPrepareUpdate_RollingRecreateAllowedWithConsent(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.RollingRecreate = append(diff.RollingRecreate, clusterupdate.Change{
		Field: fieldHetznerCPServerType,
	})
	opts := clusterupdate.UpdateOptions{AllowRollingRecreate: true}

	result, shouldContinue, err := clusterupdate.PrepareUpdate(diff, nil, opts, errRecreateRequired)

	require.NotNil(t, result)
	assert.True(t, shouldContinue)
	require.NoError(t, err)
}

// TestUpdateResult_RollingRecreateChangesAreDetected verifies the helpers and
// aggregations account for rolling-recreate changes.
func TestUpdateResult_RollingRecreateChangesAreDetected(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	change := clusterupdate.Change{
		Field:    fieldHetznerCPServerType,
		OldValue: "cx23",
		NewValue: "cpx41",
		Category: clusterupdate.ChangeCategoryRollingRecreate,
	}
	result.RollingRecreate = append(result.RollingRecreate, change)

	assert.Equal(t, 1, result.TotalChanges())
	assert.True(t, result.HasRollingRecreate())
	assert.True(t, result.NeedsUserConfirmation())
	assert.False(t, result.HasInPlaceChanges())
	assert.False(t, result.HasRecreateRequired())
	assert.Contains(t, result.AllChanges(), change)
}

// TestNewUpdateResultFromDiff_CopiesRollingRecreate tests that
// NewUpdateResultFromDiff copies RollingRecreate.
func TestNewUpdateResultFromDiff_CopiesRollingRecreate(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.RollingRecreate = append(diff.RollingRecreate, clusterupdate.Change{
		Field:    "provider.hetzner.workerServerType",
		Category: clusterupdate.ChangeCategoryRollingRecreate,
	})

	result := clusterupdate.NewUpdateResultFromDiff(diff)

	require.NotNil(t, result)
	assert.Equal(t, diff.RollingRecreate, result.RollingRecreate)
}

// TestChangeCategory_RollingRecreateString tests the String representation.
func TestChangeCategory_RollingRecreateString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "rolling-recreate", clusterupdate.ChangeCategoryRollingRecreate.String())
}

// TestControlPlaneServerTypeChangeCategory verifies the quorum-aware classification.
func TestControlPlaneServerTypeChangeCategory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		controlPlanes int
		expected      clusterupdate.ChangeCategory
	}{
		{controlPlanes: 1, expected: clusterupdate.ChangeCategoryRecreateRequired},
		{controlPlanes: 2, expected: clusterupdate.ChangeCategoryRecreateRequired},
		{controlPlanes: 3, expected: clusterupdate.ChangeCategoryRollingRecreate},
		{controlPlanes: 5, expected: clusterupdate.ChangeCategoryRollingRecreate},
	}

	for _, testCase := range tests {
		assert.Equal(t, testCase.expected,
			clusterupdate.ControlPlaneServerTypeChangeCategory(testCase.controlPlanes),
			"controlPlanes=%d", testCase.controlPlanes)
	}
}

// TestWorkerServerTypeChangeCategory verifies worker classification by node count.
func TestWorkerServerTypeChangeCategory(t *testing.T) {
	t.Parallel()

	assert.Equal(t, clusterupdate.ChangeCategoryInPlace,
		clusterupdate.WorkerServerTypeChangeCategory(0))
	assert.Equal(t, clusterupdate.ChangeCategoryRollingRecreate,
		clusterupdate.WorkerServerTypeChangeCategory(1))
	assert.Equal(t, clusterupdate.ChangeCategoryRollingRecreate,
		clusterupdate.WorkerServerTypeChangeCategory(4))
}
