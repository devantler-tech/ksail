package kindprovisioner_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

func TestProvisioner_Update_NilSpecs(t *testing.T) {
	t.Parallel()

	provisioner, _, _ := newProvisionerForTest(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		oldSpec *v1alpha1.ClusterSpec
		newSpec *v1alpha1.ClusterSpec
	}{
		{
			name:    "both nil",
			oldSpec: nil,
			newSpec: nil,
		},
		{
			name:    "old nil",
			oldSpec: nil,
			newSpec: &v1alpha1.ClusterSpec{},
		},
		{
			name:    "new nil",
			oldSpec: &v1alpha1.ClusterSpec{},
			newSpec: nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := provisioner.Update(
				ctx,
				"test-cluster",
				testCase.oldSpec,
				testCase.newSpec,
				clusterupdate.UpdateOptions{},
			)

			require.NoError(t, err)
			assert.NotNil(t, result)
			assert.Empty(t, result.InPlaceChanges)
			assert.Empty(t, result.RecreateRequired)
		})
	}
}

func TestProvisioner_DiffConfig_NilSpecs(t *testing.T) {
	t.Parallel()

	provisioner, _, _ := newProvisionerForTest(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		oldSpec *v1alpha1.ClusterSpec
		newSpec *v1alpha1.ClusterSpec
	}{
		{
			name:    "both nil",
			oldSpec: nil,
			newSpec: nil,
		},
		{
			name:    "old nil",
			oldSpec: nil,
			newSpec: &v1alpha1.ClusterSpec{},
		},
		{
			name:    "new nil",
			oldSpec: &v1alpha1.ClusterSpec{},
			newSpec: nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := provisioner.DiffConfig(
				ctx,
				"test-cluster",
				testCase.oldSpec,
				testCase.newSpec,
			)

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Empty(t, result.InPlaceChanges)
			assert.Empty(t, result.RecreateRequired)
		})
	}
}

func TestProvisioner_DiffConfig_SameMirrorsDir(t *testing.T) {
	t.Parallel()

	provisioner, _, _ := newProvisionerForTest(t)
	ctx := context.Background()

	oldSpec := &v1alpha1.ClusterSpec{
		Vanilla: v1alpha1.OptionsVanilla{MirrorsDir: "/etc/containerd/certs.d"},
	}
	newSpec := &v1alpha1.ClusterSpec{
		Vanilla: v1alpha1.OptionsVanilla{MirrorsDir: "/etc/containerd/certs.d"},
	}

	result, err := provisioner.DiffConfig(ctx, "test-cluster", oldSpec, newSpec)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.RecreateRequired, "same mirrorsDir should produce no changes")
}

func TestProvisioner_DiffConfig_MirrorsDirChange(t *testing.T) {
	t.Parallel()

	provisioner, _, _ := newProvisionerForTest(t)
	ctx := context.Background()

	oldSpec := &v1alpha1.ClusterSpec{
		Vanilla: v1alpha1.OptionsVanilla{MirrorsDir: "/etc/containerd/certs.d"},
	}
	newSpec := &v1alpha1.ClusterSpec{
		Vanilla: v1alpha1.OptionsVanilla{MirrorsDir: "/custom/mirrors"},
	}

	result, err := provisioner.DiffConfig(ctx, "test-cluster", oldSpec, newSpec)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.RecreateRequired, 1)
	assert.Equal(t, "vanilla.mirrorsDir", result.RecreateRequired[0].Field)
	assert.Equal(t, "/etc/containerd/certs.d", result.RecreateRequired[0].OldValue)
	assert.Equal(t, "/custom/mirrors", result.RecreateRequired[0].NewValue)
	assert.Equal(t,
		clusterupdate.ChangeCategoryRecreateRequired,
		result.RecreateRequired[0].Category,
	)
}

func TestProvisioner_DiffConfig_MirrorsDirFromEmpty(t *testing.T) {
	t.Parallel()

	provisioner, _, _ := newProvisionerForTest(t)
	ctx := context.Background()

	oldSpec := &v1alpha1.ClusterSpec{}
	newSpec := &v1alpha1.ClusterSpec{
		Vanilla: v1alpha1.OptionsVanilla{MirrorsDir: "/custom/mirrors"},
	}

	result, err := provisioner.DiffConfig(ctx, "test-cluster", oldSpec, newSpec)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.RecreateRequired, 1, "adding a mirrorsDir requires recreate")
	assert.Equal(t, "vanilla.mirrorsDir", result.RecreateRequired[0].Field)
}

func TestProvisioner_DiffConfig_MirrorsDirToEmpty(t *testing.T) {
	t.Parallel()

	provisioner, _, _ := newProvisionerForTest(t)
	ctx := context.Background()

	oldSpec := &v1alpha1.ClusterSpec{
		Vanilla: v1alpha1.OptionsVanilla{MirrorsDir: "/etc/containerd/certs.d"},
	}
	newSpec := &v1alpha1.ClusterSpec{}

	result, err := provisioner.DiffConfig(ctx, "test-cluster", oldSpec, newSpec)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.RecreateRequired, 1, "removing mirrorsDir requires recreate")
	assert.Equal(t, "vanilla.mirrorsDir", result.RecreateRequired[0].Field)
}

func TestProvisioner_DiffConfig_NoChanges(t *testing.T) {
	t.Parallel()

	provisioner, _, _ := newProvisionerForTest(t)
	ctx := context.Background()

	spec := &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionVanilla,
		Provider:     v1alpha1.ProviderDocker,
		Vanilla:      v1alpha1.OptionsVanilla{MirrorsDir: ""},
	}

	result, err := provisioner.DiffConfig(ctx, "test-cluster", spec, spec)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.InPlaceChanges)
	assert.Empty(t, result.RecreateRequired)
}

func TestProvisioner_Update_DelegatesViaResultToDiffConfig(t *testing.T) {
	t.Parallel()

	provisioner, _, _ := newProvisionerForTest(t)
	ctx := context.Background()

	oldSpec := &v1alpha1.ClusterSpec{
		Vanilla: v1alpha1.OptionsVanilla{MirrorsDir: "/etc/containerd/certs.d"},
	}
	newSpec := &v1alpha1.ClusterSpec{
		Vanilla: v1alpha1.OptionsVanilla{MirrorsDir: "/new/mirrors"},
	}

	result, err := provisioner.Update(
		ctx,
		"test-cluster",
		oldSpec,
		newSpec,
		clusterupdate.UpdateOptions{},
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.RecreateRequired, 1, "Update should delegate to DiffConfig")
	assert.Equal(t, "vanilla.mirrorsDir", result.RecreateRequired[0].Field)
}

func TestProvisioner_GetCurrentConfig_NoDetector(t *testing.T) {
	t.Parallel()

	provisioner, _, _ := newProvisionerForTest(t)
	ctx := context.Background()

	spec, _, err := provisioner.GetCurrentConfig(ctx)

	require.NoError(t, err)
	require.NotNil(t, spec)
	assert.Equal(t, v1alpha1.DistributionVanilla, spec.Distribution)
	assert.Equal(t, v1alpha1.ProviderDocker, spec.Provider)
}

func TestCreateProvisioner_WithConfig(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha4.Cluster{
		TypeMeta: v1alpha4.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "kind.x-k8s.io/v1alpha4",
		},
		Name: "test-cluster",
	}

	infraProvider := provider.NewMockProvider()
	provisioner, err := kindprovisioner.CreateProvisionerWithProvider(
		cfg,
		"/tmp/test-kubeconfig",
		infraProvider,
	)

	require.NoError(t, err)
	assert.NotNil(t, provisioner)
}

func TestCreateProvisioner_DefaultKubeconfig(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha4.Cluster{
		TypeMeta: v1alpha4.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "kind.x-k8s.io/v1alpha4",
		},
		Name: "test-cluster",
	}

	infraProvider := provider.NewMockProvider()
	provisioner, err := kindprovisioner.CreateProvisionerWithProvider(cfg, "", infraProvider)

	require.NoError(t, err)
	require.NotNil(t, provisioner)
	assert.Equal(t, "~/.kube/config", provisioner.KubeConfigForTest())
}
