package vclusterprovisioner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
	dockerprovider "github.com/devantler-tech/ksail/v5/pkg/svc/provider/docker"
	"github.com/docker/docker/client"
)

// CreateProvisioner creates a Provisioner from the given configuration.
//
// Parameters:
//   - name: default cluster name
//   - valuesPath: optional path to a vcluster.yaml values file
func CreateProvisioner(
	name string,
	valuesPath string,
) (*Provisioner, error) {
	dockerClient, err := newDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	infraProvider := dockerprovider.NewProvider(dockerClient, dockerprovider.LabelSchemeVCluster)

	return NewProvisioner(name, valuesPath, infraProvider), nil
}

// newDockerClient creates a Docker API client using KSail's docker package.
func newDockerClient() (*client.Client, error) {
	dockerClient, err := docker.GetDockerClient()
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}

	clientPtr, ok := dockerClient.(*client.Client)
	if !ok {
		return nil, fmt.Errorf("unexpected Docker client type: %T", dockerClient)
	}

	return clientPtr, nil
}
