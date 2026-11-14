package lifecycle_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// mismatchedAWSTarget is the only shape ValidateStandaloneAWSTarget refuses: an AWS target whose
// region came from an eks.yaml that names a different cluster.
func mismatchedAWSTarget() *lifecycle.ResolvedClusterInfo {
	return &lifecycle.ResolvedClusterInfo{
		ClusterName:         "flag-name",
		ConfigClusterName:   "eks-name",
		Provider:            v1alpha1.ProviderAWS,
		AWSRegion:           "eu-west-1",
		AWSRegionFromConfig: true,
	}
}

// mismatchedAWSTargetWith returns the refused target after applying change.
func mismatchedAWSTargetWith(
	change func(*lifecycle.ResolvedClusterInfo),
) *lifecycle.ResolvedClusterInfo {
	resolved := mismatchedAWSTarget()
	change(resolved)

	return resolved
}

// TestValidateStandaloneAWSTarget pins each early return and the refusal that stops delete, start
// and stop from acting on a same-named cluster in the wrong region.
func TestValidateStandaloneAWSTarget(t *testing.T) {
	t.Parallel()

	accepted := map[string]*lifecycle.ResolvedClusterInfo{
		"nil target": nil,
		"non-aws provider": mismatchedAWSTargetWith(func(resolved *lifecycle.ResolvedClusterInfo) {
			resolved.Provider = v1alpha1.ProviderDocker
		}),
		"region not taken from config": mismatchedAWSTargetWith(
			func(resolved *lifecycle.ResolvedClusterInfo) { resolved.AWSRegionFromConfig = false },
		),
		"empty config name": mismatchedAWSTargetWith(
			func(resolved *lifecycle.ResolvedClusterInfo) { resolved.ConfigClusterName = "" },
		),
		"equal names": mismatchedAWSTargetWith(func(resolved *lifecycle.ResolvedClusterInfo) {
			resolved.ConfigClusterName = resolved.ClusterName
		}),
	}

	for name, resolved := range accepted {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.NoError(t, lifecycle.ValidateStandaloneAWSTarget(resolved))
		})
	}

	t.Run("mismatched target is refused", func(t *testing.T) {
		t.Parallel()

		err := lifecycle.ValidateStandaloneAWSTarget(mismatchedAWSTarget())
		require.ErrorIs(t, err, lifecycle.ErrAWSTargetConfigMismatch)
		assert.Contains(t, err.Error(), "set AWS_REGION")
	})
}

// TestGetClusterNameFromConfigMetadataName pins that a metadata name is used only when it is a
// valid cluster name, and that a valid one never needs the distribution config.
func TestGetClusterNameFromConfigMetadataName(t *testing.T) {
	t.Parallel()

	t.Run("valid metadata name skips distribution config", func(t *testing.T) {
		t.Parallel()

		cfg := &v1alpha1.Cluster{}
		cfg.Name = "meta-name"

		name, err := lifecycle.GetClusterNameFromConfig(
			cfg,
			&mockFactory{createErr: errFactoryError},
		)
		require.NoError(t, err)
		assert.Equal(t, "meta-name", name)
	})

	t.Run("invalid metadata name falls back to distribution config", func(t *testing.T) {
		t.Parallel()

		cfg := &v1alpha1.Cluster{}
		cfg.Name = "Invalid_Name"
		cfg.Spec.Cluster.Distribution = v1alpha1.DistributionVanilla

		factory := &mockFactory{
			provisioner:        &mockProvisioner{},
			distributionConfig: &v1alpha4.Cluster{Name: "dist-name"},
		}

		name, err := lifecycle.GetClusterNameFromConfig(cfg, factory)
		require.NoError(t, err)
		assert.Equal(t, "dist-name", name)
	})
}

// newLockProbeCmd returns a stop command for a named target whose guard records that it ran and
// then fails with errTestError.
func newLockProbeCmd(provider v1alpha1.Provider, guarded *bool) *cobra.Command {
	cmd := lifecycle.NewSimpleLifecycleCmd(lifecycle.SimpleLifecycleConfig{
		Use: "stop",
		Guard: func(context.Context, *lifecycle.ResolvedClusterInfo) error {
			*guarded = true

			return errTestError
		},
	})
	cmd.SetArgs([]string{"--name", "lock-target", "--provider", string(provider)})
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	return cmd
}

// TestSimpleLifecycleLocksOnlyAWSTargets pins that only AWS targets run under the EKS lifecycle
// lock: a cancelled context stops an AWS target before its guard, while a Docker target reaches its
// guard and returns the guard's error unwrapped.
func TestSimpleLifecycleLocksOnlyAWSTargets(t *testing.T) {
	t.Parallel()

	t.Run("aws target waits for the lock", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		guarded := false
		err := newLockProbeCmd(v1alpha1.ProviderAWS, &guarded).ExecuteContext(ctx)
		require.ErrorIs(t, err, context.Canceled)
		assert.Contains(t, err.Error(), "EKS lifecycle operation")
		assert.False(t, guarded)
	})

	t.Run("docker target skips the lock", func(t *testing.T) {
		t.Parallel()

		guarded := false
		err := newLockProbeCmd(v1alpha1.ProviderDocker, &guarded).ExecuteContext(t.Context())
		require.EqualError(t, err, errTestError.Error())
		assert.True(t, guarded)
	})
}
