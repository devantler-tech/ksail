package clusterprovisioner_test

import (
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/runner"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

//nolint:paralleltest // swaps the existing Kind factory.
func TestDockerLifecycleForwardsPrivateKubeconfigThroughMultiProvisioner(t *testing.T) {
	for _, provider := range []v1alpha1.Provider{v1alpha1.ProviderDocker, ""} {
		t.Run(string(provider), func(t *testing.T) {
			privatePath := filepath.Join(t.TempDir(), "private-kubeconfig")

			var observedPath string

			mockRunner := runner.NewMockCommandRunner(t)
			mockRunner.EXPECT().Run(mock.Anything, mock.Anything, mock.Anything).
				Return(runner.CommandResult{Stdout: "alpha\n"}, nil).Once()

			t.Cleanup(clusterprovisioner.SetKindProvisionerFactory(
				func(config *v1alpha4.Cluster, path string) (*kindprovisioner.Provisioner, error) {
					observedPath = path

					return kindprovisioner.NewProvisionerWithRunner(
						config, path, nil, nil, mockRunner,
					), nil
				},
			))

			provisioner, err := lifecycle.CreateMinimalProvisionerForProvider(
				t.Context(), &clusterdetector.Info{
					Provider: provider, ClusterName: "alpha", KubeconfigPath: privatePath,
				}, lifecycle.MinimalProvisionerOptions{},
			)
			require.NoError(t, err)

			found, err := provisioner.Exists(t.Context(), "alpha")
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, privatePath, observedPath,
				"lifecycle and distribution routing must retain the resolved private path")
		})
	}
}
