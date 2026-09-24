package clusterapi_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/clusterapi"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/siderolabs/talos/pkg/machinery/config/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServiceRejectsUnpinnedTalosISO exercises the real local backend's factory boundary.
func TestServiceRejectsUnpinnedTalosISO(t *testing.T) {
	t.Setenv("HCLOUD_TOKEN", "")

	cluster := clusterFor("custom-image", v1alpha1.DistributionTalos)
	cluster.Spec.Cluster.Provider = v1alpha1.ProviderHetzner
	cluster.Spec.Cluster.Talos.ISO = 123456

	err := clusterapi.NewService().BuildProvisionerForTest(t.Context(), cluster)
	require.ErrorIs(t, err, v1alpha1.ErrTalosCustomISOVersionRequired)
}

// TestLocalFactoryPreservesTalosVersionPins checks the requested release controls the actual
// machine-config format, while an explicit Kubernetes pin survives the local web factory.
func TestLocalFactoryPreservesTalosVersionPins(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"v1.12.4", "v1.14.0-alpha.2"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			cluster := clusterFor("custom-image", v1alpha1.DistributionTalos)
			cluster.Spec.Cluster.Provider = v1alpha1.ProviderHetzner
			cluster.Spec.Cluster.Talos.ISO = 123456
			cluster.Spec.Cluster.Talos.Version = version
			cluster.Spec.Cluster.KubernetesVersion = "v1.34.3"

			factory, err := clusterapi.DefaultFactoryForTest(cluster)
			require.NoError(t, err)

			production, isDefaultFactory := factory.(clusterprovisioner.DefaultFactory)
			require.True(t, isDefaultFactory)

			config := production.DistributionConfig.Talos
			assert.Equal(t, "custom-image", config.Name)
			assert.Equal(t, "1.34.3", config.KubernetesVersion())
			controlPlane, isContainer := config.ControlPlane().(*container.Container)
			require.True(t, isContainer)

			var hasAPIServerDocument bool

			for _, document := range controlPlane.Documents() {
				if document.Kind() == "KubeAPIServerConfig" {
					hasAPIServerDocument = true
				}
			}

			assert.Equal(t, version == "v1.14.0-alpha.2", hasAPIServerDocument)
		})
	}
}
