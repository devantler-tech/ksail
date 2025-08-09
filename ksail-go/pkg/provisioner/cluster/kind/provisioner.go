package provkind

import (
	"bytes"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cluster"
	kindcmd "sigs.k8s.io/kind/pkg/cmd"
)

// KindClusterProvisioner is an implementation of the ClusterProvisioner interface for provisioning kind clusters.
type KindClusterProvisioner struct {
	ksailConfig    *ksailcluster.Cluster
	kindConfig     *v1alpha4.Cluster
	dockerProvider *cluster.Provider
}

// Create creates a kind cluster.
func (k *KindClusterProvisioner) Create() error {
	return k.dockerProvider.Create(
		// Use ksail's cluster name to match CLI behavior
		k.ksailConfig.Metadata.Name,
		// Pass the structured kind config; kind will handle marshalling
		cluster.CreateWithV1Alpha4Config(k.kindConfig),
		cluster.CreateWithDisplayUsage(true),
		cluster.CreateWithDisplaySalutation(true),
	)
}

// Delete deletes a kind cluster.
func (k *KindClusterProvisioner) Delete() error {
	return k.dockerProvider.Delete(
		k.ksailConfig.Metadata.Name,
		k.ksailConfig.Spec.Connection.Kubeconfig,
	)
}

// Starts a kind cluster.
func (k *KindClusterProvisioner) Start() error {
	return k.StartByName(k.ksailConfig.Metadata.Name)
}

// Stops a kind cluster.
func (k *KindClusterProvisioner) Stop() error {
	return k.StopByName(k.ksailConfig.Metadata.Name)
}

// Lists all kind clusters.
func (k *KindClusterProvisioner) List() ([]string, error) {
	return k.dockerProvider.List()
}

// Checks if a kind cluster exists.
func (k *KindClusterProvisioner) Exists() (bool, error) {
	clusters, err := k.dockerProvider.List()
	if err != nil {
		return false, err
	}
	target := k.ksailConfig.Metadata.Name
	if slices.Contains(clusters, target) {
			return true, nil
		}
	return false, nil
}

// StartByName starts all containers for the kind cluster with the given name (docker runtime).
func (k *KindClusterProvisioner) StartByName(name string) error {
	ids, err := k.dockerContainerIDsByCluster(name)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("cluster '%s' not found", name)
	}
	args := append([]string{"start"}, ids...)
	cmd := exec.Command("docker", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker start failed: %v: %s", err, stderr.String())
	}
	return nil
}

// StopByName stops all containers for the kind cluster with the given name (docker runtime).
func (k *KindClusterProvisioner) StopByName(name string) error {
	ids, err := k.dockerContainerIDsByCluster(name)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("cluster '%s' not found", name)
	}
	args := append([]string{"stop"}, ids...)
	cmd := exec.Command("docker", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker stop failed: %v: %s", err, stderr.String())
	}
	return nil
}

// dockerContainerIDsByCluster returns container IDs for the given kind cluster name using labels.
func (k *KindClusterProvisioner) dockerContainerIDsByCluster(name string) ([]string, error) {
	// List all containers (running or not) with the label for the cluster
	// NOTE: This currently assumes docker runtime.
	cmd := exec.Command("docker", "ps", "-a", "-q", "--filter", fmt.Sprintf("label=io.x-k8s.kind.cluster=%s", name))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker ps failed: %v: %s", err, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return []string{}, nil
	}
	ids := strings.Split(out, "\n")
	return ids, nil
}

// ExistsByName checks if a kind cluster with the given name exists.
func (k *KindClusterProvisioner) ExistsByName(name string) (bool, error) {
	clusters, err := k.dockerProvider.List()
	if err != nil {
		return false, err
	}
	if slices.Contains(clusters, name) {
		return true, nil
	}
	return false, nil
}

// DeleteByName deletes a cluster by the provided name.
func (k *KindClusterProvisioner) DeleteByName(name string) error {
	return k.dockerProvider.Delete(name, k.ksailConfig.Spec.Connection.Kubeconfig)
}

// / NewKindClusterProvisioner creates a new KindClusterProvisioner.
func NewKindClusterProvisioner(ksailConfig *ksailcluster.Cluster, kindConfig *v1alpha4.Cluster) *KindClusterProvisioner {
	return &KindClusterProvisioner{
		ksailConfig: ksailConfig,
		kindConfig:  kindConfig,
		// Initialize kind's provider with the same logger the CLI uses,
		// so we get progress spinners and rich output.
		dockerProvider: cluster.NewProvider(
			cluster.ProviderWithLogger(kindcmd.NewLogger()),
		),
	}
}
