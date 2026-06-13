package clusterupdate_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecreationRequiredUpgrader_UpgradeStubsRecreate verifies both behavioral
// stubs return clustererr.ErrRecreationRequired with the distribution name in the
// message — the contract the orchestrator relies on to recreate at the target
// version.
func TestRecreationRequiredUpgrader_UpgradeStubsRecreate(t *testing.T) {
	t.Parallel()

	upg := clusterupdate.NewRecreationRequiredUpgrader("kind", clusterupdate.UpgraderMetadata{})

	k8sErr := upg.UpgradeKubernetes(context.Background(), "c", "v1.0.0", "v1.1.0")
	require.ErrorIs(t, k8sErr, clustererr.ErrRecreationRequired)
	assert.Contains(t, k8sErr.Error(), "kind")
	assert.Contains(t, k8sErr.Error(), "Kubernetes")

	distErr := upg.UpgradeDistribution(context.Background(), "c", "v1.0.0", "v1.1.0")
	require.ErrorIs(t, distErr, clustererr.ErrRecreationRequired)
	assert.Contains(t, distErr.Error(), "kind")
	assert.Contains(t, distErr.Error(), "distribution")
}

// TestRecreationRequiredUpgrader_MetadataAccessors verifies the five metadata
// accessors return the values supplied at construction and that Metadata returns
// the whole descriptor.
func TestRecreationRequiredUpgrader_MetadataAccessors(t *testing.T) {
	t.Parallel()

	meta := clusterupdate.UpgraderMetadata{
		KubernetesImageRef:        "rancher/k3s",
		DistributionImageRef:      "ghcr.io/example/dist",
		PinnedDistributionVersion: "v1.2.3",
		PinnedKubernetesVersion:   "v1.30.0",
		VersionSuffix:             "k3s",
	}
	upg := clusterupdate.NewRecreationRequiredUpgrader("k3d", meta)

	assert.Equal(t, meta, upg.Metadata())
	assert.Equal(t, "rancher/k3s", upg.KubernetesImageRef())
	assert.Equal(t, "ghcr.io/example/dist", upg.DistributionImageRef())
	assert.Equal(t, "v1.2.3", upg.PinnedDistributionVersion())
	assert.Equal(t, "v1.30.0", upg.PinnedKubernetesVersion())
	assert.Equal(t, "k3s", upg.VersionSuffix())
}
