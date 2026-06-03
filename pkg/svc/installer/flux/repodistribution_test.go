package fluxinstaller_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	repoTestFilePerm    = 0o600
	repoTestDistDefault = "2.x"
	repoTestRegistry    = "ghcr.io/fluxcd"
	repoTestSyncRef     = "dev"
)

const repoFluxInstanceYAML = `apiVersion: fluxcd.controlplane.io/v1
kind: FluxInstance
metadata:
  name: flux
  namespace: flux-system
spec:
  distribution:
    version: "2.8.x"
    registry: "ghcr.io/example"
    artifact: "oci://example.com/manifests:latest"
`

func clusterWithFluxSourceDir(
	t *testing.T,
	sourceDir, distributionVersion string,
) *v1alpha1.Cluster {
	t.Helper()

	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineFlux
	cfg.Spec.Workload.SourceDirectory = sourceDir
	cfg.Spec.Workload.Flux.DistributionVersion = distributionVersion

	return cfg
}

func writeRepoFluxInstance(t *testing.T, dir string) {
	t.Helper()

	err := os.WriteFile(
		filepath.Join(dir, "flux-instance.yaml"),
		[]byte(repoFluxInstanceYAML),
		repoTestFilePerm,
	)
	require.NoError(t, err)
}

func TestResolveDesiredDistributionVersion(t *testing.T) {
	t.Parallel()

	t.Run("default when unset", func(t *testing.T) {
		t.Parallel()

		cfg := clusterWithFluxSourceDir(t, t.TempDir(), "")
		assert.Equal(t, repoTestDistDefault, fluxinstaller.ResolveDesiredDistributionVersion(cfg))
	})

	t.Run("config overrides default", func(t *testing.T) {
		t.Parallel()

		cfg := clusterWithFluxSourceDir(t, t.TempDir(), "2.7.x")
		assert.Equal(t, "2.7.x", fluxinstaller.ResolveDesiredDistributionVersion(cfg))
	})

	t.Run("repo FluxInstance overrides config", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		writeRepoFluxInstance(t, dir)
		cfg := clusterWithFluxSourceDir(t, dir, "2.7.x")
		assert.Equal(t, "2.8.x", fluxinstaller.ResolveDesiredDistributionVersion(cfg))
	})

	t.Run("nil cluster returns default", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, repoTestDistDefault, fluxinstaller.ResolveDesiredDistributionVersion(nil))
	})
}

func TestApplyDistributionOverride(t *testing.T) {
	t.Parallel()

	t.Run("repo distribution wins and sync is preserved", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		writeRepoFluxInstance(t, dir)
		cfg := clusterWithFluxSourceDir(t, dir, "2.5.x")

		instance := &fluxinstaller.FluxInstance{
			Spec: fluxinstaller.InstanceSpec{
				Distribution: fluxinstaller.Distribution{
					Version:  repoTestDistDefault,
					Registry: repoTestRegistry,
				},
				Sync: &fluxinstaller.Sync{Ref: repoTestSyncRef, URL: "oci://reg/app"},
			},
		}

		err := fluxinstaller.ApplyDistributionOverride(instance, cfg)
		require.NoError(t, err)

		assert.Equal(t, "2.8.x", instance.Spec.Distribution.Version)
		assert.Equal(t, "ghcr.io/example", instance.Spec.Distribution.Registry)
		assert.Equal(t, "oci://example.com/manifests:latest", instance.Spec.Distribution.Artifact)

		// The computed sync block must be left untouched.
		require.NotNil(t, instance.Spec.Sync)
		assert.Equal(t, repoTestSyncRef, instance.Spec.Sync.Ref)
		assert.Equal(t, "oci://reg/app", instance.Spec.Sync.URL)
	})

	t.Run("config-only override leaves registry and artifact defaults", func(t *testing.T) {
		t.Parallel()

		cfg := clusterWithFluxSourceDir(t, t.TempDir(), "2.9.x")

		instance := &fluxinstaller.FluxInstance{
			Spec: fluxinstaller.InstanceSpec{
				Distribution: fluxinstaller.Distribution{
					Version:  repoTestDistDefault,
					Registry: repoTestRegistry,
				},
				Sync: &fluxinstaller.Sync{Ref: repoTestSyncRef},
			},
		}

		err := fluxinstaller.ApplyDistributionOverride(instance, cfg)
		require.NoError(t, err)

		assert.Equal(t, "2.9.x", instance.Spec.Distribution.Version)
		assert.Equal(t, repoTestRegistry, instance.Spec.Distribution.Registry)
		assert.Empty(t, instance.Spec.Distribution.Artifact)
	})
}
