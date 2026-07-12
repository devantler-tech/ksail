package kindprovisioner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
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

	spec, _, err := provisioner.GetCurrentConfig(ctx, "test-cluster")

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
	t.Setenv("KUBECONFIG", "")

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
	assert.Equal(t, k8s.DefaultKubeconfigPath(), provisioner.KubeConfigForTest())
}

// TestCreateProvisioner_KubeconfigEnvFallback pins a single KUBECONFIG entry
// as Kind's write target when no kubeconfig path is configured explicitly.
func TestCreateProvisioner_KubeconfigEnvFallback(t *testing.T) {
	t.Setenv("KUBECONFIG", "/tmp/env-kubeconfig")

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
	assert.Equal(t, "/tmp/env-kubeconfig", provisioner.KubeConfigForTest())
}

// TestCreateProvisioner_KubeconfigEnvPreservesLiteralPath verifies that an
// environment-derived target is not expanded like an explicit user path.
func TestCreateProvisioner_KubeconfigEnvPreservesLiteralPath(t *testing.T) {
	t.Setenv("KUBECONFIG", "~/literal-kubeconfig")

	cfg := &v1alpha4.Cluster{Name: "test-cluster"}
	infraProvider := provider.NewMockProvider()
	provisioner, err := kindprovisioner.CreateProvisionerWithProvider(cfg, "", infraProvider)

	require.NoError(t, err)
	require.NotNil(t, provisioner)
	assert.Equal(t, "~/literal-kubeconfig", provisioner.KubeConfigForTest())
}

// TestCreateProvisioner_ExplicitKubeconfigExpandsHome keeps expansion at the
// factory boundary for explicitly configured paths.
func TestCreateProvisioner_ExplicitKubeconfigExpandsHome(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("KUBECONFIG", "~/ignored-environment-target")

	cfg := &v1alpha4.Cluster{Name: "test-cluster"}
	infraProvider := provider.NewMockProvider()
	provisioner, err := kindprovisioner.CreateProvisionerWithProvider(
		cfg,
		"~/.kube/config",
		infraProvider,
	)

	require.NoError(t, err)
	require.NotNil(t, provisioner)
	assert.Equal(t, filepath.Join(homeDir, ".kube", "config"), provisioner.KubeConfigForTest())
}

// TestCreateProvisioner_KubeconfigEnvListPrefersExistingFile pins kind's
// write-target rule for KUBECONFIG path lists: the first entry naming an
// existing file wins, even when it is not the list's first entry.
func TestCreateProvisioner_KubeconfigEnvListPrefersExistingFile(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(existing, []byte("{}"), 0o600))
	t.Setenv("KUBECONFIG", "/tmp/nosuch-kubeconfig"+string(os.PathListSeparator)+existing)

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
	assert.Equal(t, existing, provisioner.KubeConfigForTest())
}

// TestCreateProvisioner_KubeconfigEnvListProbesEntriesLiterally verifies that
// a trailing separator is not cleaned before Kind's existence check. A regular
// file with a trailing slash is not a usable path, so the fallback must win.
func TestCreateProvisioner_KubeconfigEnvListProbesEntriesLiterally(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "config")
	fallback := filepath.Join(root, "fallback")

	require.NoError(t, os.WriteFile(existing, []byte("{}"), 0o600))
	t.Setenv(
		"KUBECONFIG",
		existing+string(os.PathSeparator)+string(os.PathListSeparator)+fallback,
	)

	cfg := &v1alpha4.Cluster{Name: "test-cluster"}
	infraProvider := provider.NewMockProvider()
	provisioner, err := kindprovisioner.CreateProvisionerWithProvider(cfg, "", infraProvider)

	require.NoError(t, err)
	require.NotNil(t, provisioner)
	assert.Equal(t, fallback, provisioner.KubeConfigForTest())
}

// TestCreateProvisioner_KubeconfigEnvListFallsBackToLastEntry pins the other
// half of kind's rule: when no listed file exists, the LAST entry is the
// write target.
func TestCreateProvisioner_KubeconfigEnvListFallsBackToLastEntry(t *testing.T) {
	t.Setenv(
		"KUBECONFIG",
		"/tmp/nosuch-one"+string(os.PathListSeparator)+"/tmp/nosuch-two",
	)

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
	assert.Equal(t, "/tmp/nosuch-two", provisioner.KubeConfigForTest())
}

// TestCreateProvisioner_KubeconfigEnvListDeduplicates pins Kind's ordered
// de-duplication before the last-entry fallback is selected.
func TestCreateProvisioner_KubeconfigEnvListDeduplicates(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "nosuch-one")
	second := filepath.Join(root, "nosuch-two")
	t.Setenv(
		"KUBECONFIG",
		first+string(os.PathListSeparator)+second+string(os.PathListSeparator)+first,
	)

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
	assert.Equal(t, second, provisioner.KubeConfigForTest())
}
