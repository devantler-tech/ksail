package containerengineprovisioner

import (
	"context"
	"fmt"
	"os"

	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
	"github.com/docker/docker/client"
)

// PodmanProvisioner implements ContainerEngineProvisioner for Podman.
type PodmanProvisioner struct {
	client *client.Client
}

// CheckReady checks if the Podman service/socket is available.
func (p *PodmanProvisioner) CheckReady() (bool, error) {
	ctx := context.Background()
	_, err := p.client.Ping(ctx)
	if err != nil {
		return false, err
	}
	return true, nil
}

// NewPodmanProvisioner creates a new PodmanProvisioner.
func NewPodmanProvisioner(cfg *ksailcluster.Cluster) *PodmanProvisioner {
	podmanSock := fmt.Sprintf("unix:///run/user/%d/podman/podman.sock", os.Getuid())
	os.Setenv("DOCKER_HOST", podmanSock)
	cli, _ := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	return &PodmanProvisioner{client: cli}
}
