package clusterprovisioner_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	kindconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	kubeadmhetznerprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kubeadmhetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateProvisioner_VanillaHetzner verifies the factory dispatches the Vanilla ×
// Hetzner combination to the kubeadm-on-Hetzner provisioner without a Kind config:
// the DistributionConfig struct is present (Create requires it) but its Kind field is
// nil, so the Hetzner dispatch must resolve before the Kind-config requirement — the
// same shape the k3s × Hetzner path relies on. Cannot run in parallel: it sets
// HCLOUD_TOKEN so the provisioner's Hetzner provider construction succeeds.
func TestCreateProvisioner_VanillaHetzner(t *testing.T) {
	t.Setenv("HCLOUD_TOKEN", "dummy-token")

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{},
	}
	cluster := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:  v1alpha1.DistributionVanilla,
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 1,
			},
		},
	}

	provisioner, distributionConfig, err := factory.Create(context.Background(), cluster)

	require.NoError(t, err)
	require.NotNil(t, provisioner)
	assert.IsType(t, &kubeadmhetznerprovisioner.Provisioner{}, provisioner)
	// The Hetzner path returns no distribution config (unlike the Docker/Kind path).
	assert.Nil(t, distributionConfig)
}

// TestKubeadmInstallVersion verifies the kubeadm install version is derived from the
// Vanilla distribution's Kind node image: a bare "vMAJOR.MINOR[.PATCH]" tag with the
// image digest and repository path stripped, matching the tag kubeadm requires to
// select the community package repository.
func TestKubeadmInstallVersion(t *testing.T) {
	t.Parallel()

	version := clusterprovisioner.ExportKubeadmInstallVersion()

	require.NotEmpty(t, version)
	assert.Regexp(t, `^v[0-9]+\.[0-9]+(\.[0-9]+)?$`, version,
		"want a bare v-prefixed version with the digest stripped, got %q", version)
	// The version tracks the Kind node image tag exactly, so a Vanilla cluster runs
	// the same Kubernetes release on Docker (Kind) and Hetzner (kubeadm).
	assert.Contains(t, kindconfigmanager.DefaultKindNodeImage, version,
		"version must be a substring of the Kind node image")
}
