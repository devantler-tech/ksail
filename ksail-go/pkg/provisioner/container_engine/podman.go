package containerengineprovisioner

import (
	"os/exec"

	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
)

// PodmanProvisioner implements ContainerEngineProvisioner for Podman.
type PodmanProvisioner struct{}

// CheckReady checks if the Podman service/socket is available.
func (p *PodmanProvisioner) CheckReady() (bool, error) {
	cmd := exec.Command("podman", "info")
	if err := cmd.Run(); err != nil {
		return false, err
	}
	return true, nil
}

// NewPodmanProvisioner creates a new PodmanProvisioner.
func NewPodmanProvisioner(ksailConfig *ksailcluster.Cluster) *PodmanProvisioner {
	return &PodmanProvisioner{}
}
