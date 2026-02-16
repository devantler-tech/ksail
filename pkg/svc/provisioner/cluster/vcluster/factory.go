package vclusterprovisioner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
	dockerprovider "github.com/devantler-tech/ksail/v5/pkg/svc/provider/docker"
)

// CreateProvisioner creates a Provisioner from the given configuration.
//
// Parameters:
//   - name: default cluster name
//   - valuesPath: optional path to a vcluster.yaml values file
//   - disableFlannel: disable the built-in flannel CNI
func CreateProvisioner(
	name string,
	valuesPath string,
	disableFlannel bool,
) (*Provisioner, error) {
	dockerClient, err := docker.GetConcreteDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	infraProvider := dockerprovider.NewProvider(dockerClient, dockerprovider.LabelSchemeVCluster)

	return NewProvisioner(name, valuesPath, disableFlannel, infraProvider), nil
}
