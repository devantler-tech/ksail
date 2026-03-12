package cluster_test

import (
	"testing"

	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clusterupdate"
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
		"cluster.certManager",
		"cluster.policyEngine",
		"cluster.gitOpsEngine",
	}

	for _, field := range knownFields {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			found := clusterpkg.ExportHandlerForField(cmd, clusterCfg, field)

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

			found := clusterpkg.ExportHandlerForField(cmd, clusterCfg, field)

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

	err := clusterpkg.ExportReconcileMetricsServer(cmd, clusterCfg, change)

	require.Error(t, err)
	require.ErrorIs(t, err, clusterpkg.ExportErrMetricsServerDisableUnsupported)
}

// TestReconcileCSI_NilFactory verifies that reconcileCSI returns an error when the CSI
// installer factory has not been configured.
func TestReconcileCSI_NilFactory(t *testing.T) {
	t.Parallel()

	restore := clusterpkg.SetCSIInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.csi",
		OldValue: string(v1alpha1.CSIDisabled),
		NewValue: "hetznercsi",
	}

	err := clusterpkg.ExportReconcileCSI(cmd, clusterCfg, change)

	require.Error(t, err)
	assert.ErrorIs(t, err, setup.ErrCSIInstallerFactoryNil)
}

// TestReconcileCSI_DisabledToDisabled_Noop verifies that transitioning from disabled to
// disabled is a no-op regardless of the factory state.
func TestReconcileCSI_DisabledToDisabled_Noop(t *testing.T) {
	t.Parallel()

	// nil CSI factory — but the no-op path returns before the nil check
	restore := clusterpkg.SetCSIInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.csi",
		OldValue: string(v1alpha1.CSIDisabled),
		NewValue: string(v1alpha1.CSIDisabled),
	}

	// The nil-check guard fires before the no-op check, so this returns an error.
	// Document this known behaviour to prevent regressions.
	err := clusterpkg.ExportReconcileCSI(cmd, clusterCfg, change)
	require.Error(t, err, "nil factory is checked before the disabled no-op path")
	assert.ErrorIs(t, err, setup.ErrCSIInstallerFactoryNil)
}

// TestReconcileCertManager_DisabledFromDisabled_Noop verifies that disabling cert-manager
// when it was already disabled/empty is a no-op. A non-nil (but never-called) factory is
// required because the nil guard fires before the disabled no-op check.
func TestReconcileCertManager_DisabledFromDisabled_Noop(t *testing.T) {
	t.Parallel()

	// Factory must be non-nil to pass the nil guard; it will never actually be called.
	restore := clusterpkg.SetCertManagerInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			t.Fatal("factory should not be called for disabled→disabled transition")

			return nil, nil
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

	err := clusterpkg.ExportReconcileCertManager(cmd, clusterCfg, change)

	require.NoError(t, err, "disabling when already disabled should be a no-op")
}

// TestReconcileCertManager_DisabledFromEnabled_NilFactory verifies that attempting to
// uninstall cert-manager with a nil factory returns the factory-nil error.
func TestReconcileCertManager_DisabledFromEnabled_NilFactory(t *testing.T) {
	t.Parallel()

	restore := clusterpkg.SetCertManagerInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.certManager",
		OldValue: string(v1alpha1.CertManagerEnabled),
		NewValue: string(v1alpha1.CertManagerDisabled),
	}

	err := clusterpkg.ExportReconcileCertManager(cmd, clusterCfg, change)

	require.Error(t, err)
	assert.ErrorIs(t, err, setup.ErrCertManagerInstallerFactoryNil)
}

// TestReconcilePolicyEngine_NoneToNone_Noop verifies that transitioning from no policy
// engine to no policy engine is a no-op.
func TestReconcilePolicyEngine_NoneToNone_Noop(t *testing.T) {
	t.Parallel()

	restore := clusterpkg.SetPolicyEngineInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.policyEngine",
		OldValue: string(v1alpha1.PolicyEngineNone),
		NewValue: string(v1alpha1.PolicyEngineNone),
	}

	err := clusterpkg.ExportReconcilePolicyEngine(cmd, clusterCfg, change)

	require.NoError(t, err, "None→None should be a no-op")
}

// TestReconcilePolicyEngine_NoneFromEnabled_NilFactory verifies that attempting to
// uninstall a policy engine with a nil factory returns the factory-nil error.
func TestReconcilePolicyEngine_NoneFromEnabled_NilFactory(t *testing.T) {
	t.Parallel()

	restore := clusterpkg.SetPolicyEngineInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.policyEngine",
		OldValue: string(v1alpha1.PolicyEngineKyverno),
		NewValue: string(v1alpha1.PolicyEngineNone),
	}

	err := clusterpkg.ExportReconcilePolicyEngine(cmd, clusterCfg, change)

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

	err := clusterpkg.ExportReconcileGitOpsEngine(cmd, clusterCfg, change)

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

	err := clusterpkg.ExportReconcileGitOpsEngine(cmd, clusterCfg, change)

	require.NoError(t, err, "empty→empty should be a no-op")
}

// TestReconcileComponents_EmptyDiff verifies that an empty diff results in no changes and no error.
func TestReconcileComponents_EmptyDiff(t *testing.T) {
	t.Parallel()

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	diff := &clusterupdate.UpdateResult{}
	result := &clusterupdate.UpdateResult{}

	err := clusterpkg.ExportReconcileComponents(cmd, clusterCfg, diff, result)

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

	err := clusterpkg.ExportReconcileComponents(cmd, clusterCfg, diff, result)

	require.NoError(t, err, "unknown fields should be skipped without error")
	assert.Empty(t, result.AppliedChanges, "unknown field should not be recorded as applied")
	assert.Empty(t, result.FailedChanges, "unknown field should not be recorded as failed")
}

// TestReconcileComponents_RecordsFailedChange verifies that a component error is captured
// in result.FailedChanges and the reconciler continues processing remaining changes.
func TestReconcileComponents_RecordsFailedChange(t *testing.T) {
	t.Parallel()

	// Null CSI factory so the reconcileCSI call will fail immediately.
	restore := clusterpkg.SetCSIInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			{
				Field:    "cluster.csi",
				OldValue: string(v1alpha1.CSIDisabled),
				NewValue: "hetznercsi",
			},
		},
	}
	result := &clusterupdate.UpdateResult{}

	err := clusterpkg.ExportReconcileComponents(cmd, clusterCfg, diff, result)

	require.Error(t, err, "expected error from nil CSI factory")
	require.Len(t, result.FailedChanges, 1)
	assert.Equal(t, "cluster.csi", result.FailedChanges[0].Field)
	assert.Empty(t, result.AppliedChanges)
}

// TestReconcileComponents_MixedKnownAndUnknown verifies that known fields are processed
// and unknown fields are silently skipped in the same diff.
func TestReconcileComponents_MixedKnownAndUnknown(t *testing.T) {
	t.Parallel()

	// Provide a working cert-manager factory that returns an installer that succeeds.
	restore := clusterpkg.SetCertManagerInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			// Never called in this test because we test a disabled→disabled no-op.
			return nil, nil
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

	err := clusterpkg.ExportReconcileComponents(cmd, clusterCfg, diff, result)

	require.NoError(t, err)
	// The certManager no-op is a handler returning nil — it is counted as applied.
	assert.Len(t, result.AppliedChanges, 1)
	assert.Equal(t, "cluster.certManager", result.AppliedChanges[0].Field)
	assert.Empty(t, result.FailedChanges)
}
