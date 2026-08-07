package cluster_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newReconcileTestCmd creates a minimal cobra command for reconcile tests.
func newReconcileTestCmd() *cobra.Command {
	return &cobra.Command{Use: "test"}
}

// newReconcileTestClusterCfg creates a minimal cluster config for reconcile tests.
func newReconcileTestClusterCfg() *v1alpha1.Cluster {
	return &v1alpha1.Cluster{}
}

// TestHandlerForField_KnownFields verifies that each recognised component field has a handler.
func TestHandlerForField_KnownFields(t *testing.T) {
	t.Parallel()

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()

	knownFields := []string{
		"cluster.cni",
		"cluster.csi",
		"cluster.metricsServer",
		"cluster.loadBalancer",
		specdiff.EKSLoadBalancerControllerField,
		"cluster.certManager",
		"cluster.policyEngine",
		"cluster.gitOpsEngine",
		"cluster.autoscaler.node.enabled",
		"cluster.autoscaler.node.maxNodesTotal",
		"cluster.autoscaler.node.expander",
		"cluster.autoscaler.node.scaleDownUnneededTime",
		"cluster.autoscaler.node.pools[my-pool]",
	}

	for _, field := range knownFields {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			found := cluster.ExportHandlerForField(cmd, clusterCfg, field)

			assert.True(t, found, "expected a handler to be registered for field %q", field)
		})
	}
}

// TestHandlerForField_UnknownField verifies that unrecognised fields have no handler.
func TestHandlerForField_UnknownField(t *testing.T) {
	t.Parallel()

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()

	fields := []string{
		"cluster.unknown",
		"cluster.nodes",
		"cluster.mirrorRegistries",
		"",
	}

	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			found := cluster.ExportHandlerForField(cmd, clusterCfg, field)

			assert.False(t, found, "expected no handler for field %q", field)
		})
	}
}

// TestReconcileMetricsServer_DisabledReturnsError verifies that attempting to disable
// metrics-server in-place returns the unsupported-operation error.
func TestReconcileMetricsServer_DisabledReturnsError(t *testing.T) {
	t.Parallel()

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.metricsServer",
		OldValue: string(v1alpha1.MetricsServerEnabled),
		NewValue: string(v1alpha1.MetricsServerDisabled),
	}

	err := cluster.ExportReconcileMetricsServer(cmd, clusterCfg, change)

	require.Error(t, err)
	require.ErrorIs(t, err, cluster.ErrMetricsServerDisableUnsupported)
}

// TestReconcileCSI_NilFactory verifies that reconcileCSI returns an error when the CSI
// installer factory has not been configured.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcileCSI_NilFactory(t *testing.T) {
	restore := cluster.SetCSIInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.csi",
		OldValue: string(v1alpha1.CSIDisabled),
		NewValue: "hetznercsi",
	}

	err := cluster.ExportReconcileCSI(cmd, clusterCfg, change)

	require.Error(t, err)
	assert.ErrorIs(t, err, setup.ErrCSIInstallerFactoryNil)
}

// TestReconcileCSI_NilFactory_DisabledToDisabled documents that the nil-factory guard fires
// before the disabled-to-disabled no-op check, so a nil factory returns an error even for
// disabled→disabled transitions.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcileCSI_NilFactory_DisabledToDisabled(t *testing.T) {
	// nil CSI factory — the nil-factory guard fires before the disabled no-op check
	restore := cluster.SetCSIInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.csi",
		OldValue: string(v1alpha1.CSIDisabled),
		NewValue: string(v1alpha1.CSIDisabled),
	}

	// The nil-factory guard fires before the no-op check, so this returns an error.
	// Document this known behaviour to prevent regressions.
	err := cluster.ExportReconcileCSI(cmd, clusterCfg, change)
	require.Error(t, err, "nil factory is checked before the disabled no-op path")
	assert.ErrorIs(t, err, setup.ErrCSIInstallerFactoryNil)
}

// TestReconcileCertManager_DisabledFromDisabled_Noop verifies that disabling cert-manager
// when it was already disabled/empty is a no-op. A non-nil (but never-called) factory is
// required because the nil guard fires before the disabled no-op check.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcileCertManager_DisabledFromDisabled_Noop(t *testing.T) {
	// Factory must be non-nil to pass the nil guard; it will never actually be called.
	restore := cluster.SetCertManagerInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			t.Fatal("factory should not be called for disabled→disabled transition") //nolint:revive
			panic("unreachable")
		},
	)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.certManager",
		OldValue: string(v1alpha1.CertManagerDisabled),
		NewValue: string(v1alpha1.CertManagerDisabled),
	}

	err := cluster.ExportReconcileCertManager(cmd, clusterCfg, change)

	require.NoError(t, err, "disabling when already disabled should be a no-op")
}

// TestReconcileCertManager_DisabledFromEnabled_NilFactory verifies that attempting to
// uninstall cert-manager with a nil factory returns the factory-nil error.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcileCertManager_DisabledFromEnabled_NilFactory(t *testing.T) {
	restore := cluster.SetCertManagerInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.certManager",
		OldValue: string(v1alpha1.CertManagerEnabled),
		NewValue: string(v1alpha1.CertManagerDisabled),
	}

	err := cluster.ExportReconcileCertManager(cmd, clusterCfg, change)

	require.Error(t, err)
	assert.ErrorIs(t, err, setup.ErrCertManagerInstallerFactoryNil)
}

// TestReconcilePolicyEngine_NoneToNone_Noop verifies that transitioning from no policy
// engine to no policy engine is a no-op.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcilePolicyEngine_NoneToNone_Noop(t *testing.T) {
	restore := cluster.SetPolicyEngineInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.policyEngine",
		OldValue: string(v1alpha1.PolicyEngineNone),
		NewValue: string(v1alpha1.PolicyEngineNone),
	}

	err := cluster.ExportReconcilePolicyEngine(cmd, clusterCfg, change)

	require.NoError(t, err, "None→None should be a no-op")
}

// TestReconcilePolicyEngine_NoneFromEnabled_NilFactory verifies that attempting to
// uninstall a policy engine with a nil factory returns the factory-nil error.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcilePolicyEngine_NoneFromEnabled_NilFactory(t *testing.T) {
	restore := cluster.SetPolicyEngineInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.policyEngine",
		OldValue: string(v1alpha1.PolicyEngineKyverno),
		NewValue: string(v1alpha1.PolicyEngineNone),
	}

	err := cluster.ExportReconcilePolicyEngine(cmd, clusterCfg, change)

	require.Error(t, err)
	assert.ErrorIs(t, err, setup.ErrPolicyEngineInstallerFactoryNil)
}

// TestReconcileGitOpsEngine_NoneToNone_Noop verifies that None→None is a no-op.
func TestReconcileGitOpsEngine_NoneToNone_Noop(t *testing.T) {
	t.Parallel()

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.gitOpsEngine",
		OldValue: string(v1alpha1.GitOpsEngineNone),
		NewValue: string(v1alpha1.GitOpsEngineNone),
	}

	err := cluster.ExportReconcileGitOpsEngine(cmd, clusterCfg, change)

	require.NoError(t, err, "None→None should be a no-op")
}

// TestReconcileGitOpsEngine_EmptyToEmpty_Noop verifies that empty→empty is a no-op.
func TestReconcileGitOpsEngine_EmptyToEmpty_Noop(t *testing.T) {
	t.Parallel()

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.gitOpsEngine",
		OldValue: "",
		NewValue: "",
	}

	err := cluster.ExportReconcileGitOpsEngine(cmd, clusterCfg, change)

	require.NoError(t, err, "empty→empty should be a no-op")
}

// TestReconcileClusterAutoscaler_NilFactory verifies that reconcileClusterAutoscaler returns
// the factory-nil error when the autoscaler is needed but the factory is nil.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcileClusterAutoscaler_NilFactory(t *testing.T) {
	restore := cluster.SetClusterAutoscalerInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	clusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderHetzner
	clusterCfg.Spec.Cluster.Autoscaler.Node.Enabled = v1alpha1.NodeAutoscalerEnabledEnabled

	change := clusterupdate.Change{
		Field:    "cluster.autoscaler.node.enabled",
		OldValue: "false",
		NewValue: "true",
	}

	err := cluster.ExportReconcileClusterAutoscaler(cmd, clusterCfg, change)

	require.Error(t, err)
	assert.ErrorIs(t, err, setup.ErrClusterAutoscalerInstallerFactoryNil)
}

// TestReconcileClusterAutoscaler_NotNeeded_Noop verifies that reconcileClusterAutoscaler
// is a no-op when the cluster does not require the autoscaler (e.g. non-Hetzner provider)
// and the factory is nil (no uninstall needed for a component that was never installed).
func TestReconcileClusterAutoscaler_NotNeeded_Noop(t *testing.T) {
	t.Parallel()

	cmd := newReconcileTestCmd()
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionVanilla
	clusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderDocker
	clusterCfg.Spec.Cluster.Autoscaler.Node.Enabled = v1alpha1.NodeAutoscalerEnabledDisabled

	change := clusterupdate.Change{
		Field:    "cluster.autoscaler.node.enabled",
		OldValue: "false",
		NewValue: "false",
	}

	err := cluster.ExportReconcileClusterAutoscaler(cmd, clusterCfg, change)

	require.NoError(t, err, "non-Hetzner clusters should not attempt autoscaler install")
}

// TestReconcileClusterAutoscaler_Uninstall_NilFactory verifies that reconcileClusterAutoscaler
// returns the factory-nil error when the autoscaler is no longer needed on a Talos×Hetzner
// cluster and the factory is nil.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcileClusterAutoscaler_Uninstall_NilFactory(t *testing.T) {
	restore := cluster.SetClusterAutoscalerInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	clusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderHetzner
	clusterCfg.Spec.Cluster.Autoscaler.Node.Enabled = v1alpha1.NodeAutoscalerEnabledDisabled

	change := clusterupdate.Change{
		Field:    "cluster.autoscaler.node.enabled",
		OldValue: "true",
		NewValue: "false",
	}

	err := cluster.ExportReconcileClusterAutoscaler(cmd, clusterCfg, change)

	require.Error(t, err)
	assert.ErrorIs(t, err, setup.ErrClusterAutoscalerInstallerFactoryNil)
}

// TestReconcileComponents_EmptyDiff verifies that an empty diff results in no changes and no error.
func TestReconcileComponents_EmptyDiff(t *testing.T) {
	t.Parallel()

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	diff := &clusterupdate.UpdateResult{}
	result := &clusterupdate.UpdateResult{}

	err := cluster.ExportReconcileComponents(cmd, clusterCfg, diff, result)

	require.NoError(t, err)
	assert.Empty(t, result.AppliedChanges)
	assert.Empty(t, result.FailedChanges)
}

// TestReconcileComponents_UnknownField_Skipped verifies that unknown field names are skipped.
func TestReconcileComponents_UnknownField_Skipped(t *testing.T) {
	t.Parallel()

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			{
				Field:    "cluster.nodes",
				OldValue: "1",
				NewValue: "3",
			},
		},
	}
	result := &clusterupdate.UpdateResult{}

	err := cluster.ExportReconcileComponents(cmd, clusterCfg, diff, result)

	require.NoError(t, err, "unknown fields should be skipped without error")
	assert.Empty(t, result.AppliedChanges, "unknown field should not be recorded as applied")
	assert.Empty(t, result.FailedChanges, "unknown field should not be recorded as failed")
}

// TestReconcileComponents_RecordsFailedChange verifies that a component error is captured
// in result.FailedChanges while processing of remaining changes continues.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcileComponents_RecordsFailedChange(t *testing.T) {
	// Null CSI factory so the reconcileCSI call will fail immediately.
	restore := cluster.SetCSIInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			// This change will fail: nil CSI factory.
			{
				Field:    "cluster.csi",
				OldValue: string(v1alpha1.CSIDisabled),
				NewValue: "hetznercsi",
			},
			// This change will succeed: GitOps None→None is a no-op, no factory needed.
			{
				Field:    "cluster.gitOpsEngine",
				OldValue: string(v1alpha1.GitOpsEngineNone),
				NewValue: string(v1alpha1.GitOpsEngineNone),
			},
		},
	}
	result := &clusterupdate.UpdateResult{}

	err := cluster.ExportReconcileComponents(cmd, clusterCfg, diff, result)

	require.Error(t, err, "expected error from nil CSI factory")
	require.Len(t, result.FailedChanges, 1)
	assert.Equal(t, "cluster.csi", result.FailedChanges[0].Field)
	// The GitOps no-op change is applied after the failure, confirming processing continues.
	require.Len(t, result.AppliedChanges, 1)
	assert.Equal(t, "cluster.gitOpsEngine", result.AppliedChanges[0].Field)
}

// TestReconcileComponents_MixedKnownAndUnknown verifies that known fields are processed
// and unknown fields are silently skipped in the same diff.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcileComponents_MixedKnownAndUnknown(t *testing.T) {
	// Provide a working cert-manager factory that returns an installer that succeeds.
	restore := cluster.SetCertManagerInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			// Never called in this test because we test a disabled→disabled no-op.
			t.Fatal("factory should not be called for disabled→disabled transition") //nolint:revive
			panic("unreachable")
		},
	)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			// Unknown field — should be skipped
			{Field: "cluster.nodes", OldValue: "1", NewValue: "3"},
			// Known field, disabled→disabled — should be a no-op (no error)
			{
				Field:    "cluster.certManager",
				OldValue: string(v1alpha1.CertManagerDisabled),
				NewValue: string(v1alpha1.CertManagerDisabled),
			},
			// Another unknown field — should be skipped
			{Field: "cluster.mirrorRegistries", OldValue: "a", NewValue: "b"},
		},
	}
	result := &clusterupdate.UpdateResult{}

	err := cluster.ExportReconcileComponents(cmd, clusterCfg, diff, result)

	require.NoError(t, err)
	// The certManager no-op is a handler returning nil — it is counted as applied.
	assert.Len(t, result.AppliedChanges, 1)
	assert.Equal(t, "cluster.certManager", result.AppliedChanges[0].Field)
	assert.Empty(t, result.FailedChanges)
}

// TestApplyInPlaceChanges_FailedChangesReturnError verifies that provisioner-level
// failures — recorded in result.FailedChanges with a nil Update error, as the
// Talos provisioner does for a rejected machine config — make applyInPlaceChanges
// return a non-nil error so the command exits non-zero (issue #4935). Before the
// fix it returned nil and automation gating on the exit code saw a failed update
// as success.
func TestApplyInPlaceChanges_FailedChangesReturnError(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
		Field:  "talos.config",
		Reason: "apply control-plane config: rpc error",
	})

	// A non-component in-place change keeps reconcileComponents a no-op, so the
	// only failure originates from the provisioner — isolating the bug's path.
	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field: "controlPlanes", OldValue: "1", NewValue: "3",
	})

	ctx := &localregistry.Context{ClusterCfg: &v1alpha1.Cluster{}}

	err := cluster.ExportApplyInPlaceChanges(
		newReconcileTestCmd(), &fakeUpdater{result: result},
		"update-exit-code-fail", &v1alpha1.ClusterSpec{}, ctx, diff,
		nil, false, false,
	)

	require.Error(t, err)
	require.ErrorIs(t, err, cluster.ErrUpdateChangesFailed)
}

// TestApplyInPlaceChanges_NoFailuresSucceeds verifies the happy path: with no
// failed changes, applyInPlaceChanges returns nil so a clean update still exits
// zero.
func TestApplyInPlaceChanges_NoFailuresSucceeds(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	result.AppliedChanges = append(result.AppliedChanges, clusterupdate.Change{
		Field: "controlPlanes", OldValue: "1", NewValue: "3",
	})

	ctx := &localregistry.Context{ClusterCfg: &v1alpha1.Cluster{}}

	err := cluster.ExportApplyInPlaceChanges(
		newReconcileTestCmd(), &fakeUpdater{result: result},
		"update-exit-code-ok", &v1alpha1.ClusterSpec{}, ctx,
		clusterupdate.NewEmptyUpdateResult(), nil, false, false,
	)

	require.NoError(t, err)
}
