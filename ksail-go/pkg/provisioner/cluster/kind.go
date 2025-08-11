package clusterprovisioner

import (
	"bytes"
	"fmt"
	"os/exec"
	"slices"

	"github.com/devantler-tech/ksail/internal/utils"
	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
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
func (k *KindClusterProvisioner) Create(name string) error {
	target := name
	if target == "" {
		target = k.ksailConfig.Metadata.Name
	}
	return k.dockerProvider.Create(
		target,
		cluster.CreateWithV1Alpha4Config(k.kindConfig),
		cluster.CreateWithDisplayUsage(true),
		cluster.CreateWithDisplaySalutation(true),
	)
}

// Delete deletes a kind cluster.
func (k *KindClusterProvisioner) Delete(name string) error {
	target := name
	if target == "" {
		target = k.ksailConfig.Metadata.Name
	}
	kubeconfigPath, err := utils.ExpandPath(k.ksailConfig.Spec.Connection.Kubeconfig)
	if err != nil {
		return err
	}
	return k.dockerProvider.Delete(target, kubeconfigPath)
}

// Starts a kind cluster.
func (k *KindClusterProvisioner) Start(name string) error {
	target := name
	if target == "" {
		target = k.ksailConfig.Metadata.Name
	}
	nodes, err := k.dockerProvider.ListNodes(target)
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		return fmt.Errorf("cluster '%s' not found", target)
	}
	for _, n := range nodes {
		// Start each node container by name
		cmd := exec.Command("docker", "start", n.String())
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("docker start failed: %v: %s", err, stderr.String())
		}
	}
	return nil
}

// Stops a kind cluster.
func (k *KindClusterProvisioner) Stop(name string) error {
	target := name
	if target == "" {
		target = k.ksailConfig.Metadata.Name
	}
	nodes, err := k.dockerProvider.ListNodes(target)
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		return fmt.Errorf("cluster '%s' not found", target)
	}
	for _, n := range nodes {
		// Stop each node container by name
		cmd := exec.Command("docker", "stop", n.String())
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("docker stop failed: %v: %s", err, stderr.String())
		}
	}
	return nil
}

// Lists all kind clusters.
func (k *KindClusterProvisioner) List() ([]string, error) {
	return k.dockerProvider.List()
}

// Checks if a kind cluster exists.
func (k *KindClusterProvisioner) Exists(name string) (bool, error) {
	clusters, err := k.dockerProvider.List()
	if err != nil {
		return false, err
	}
	target := name
	if target == "" {
		target = k.ksailConfig.Metadata.Name
	}
	if slices.Contains(clusters, target) {
		return true, nil
	}
	return false, nil
}

// NewKindClusterProvisioner creates a new KindClusterProvisioner.
func NewKindClusterProvisioner(ksailConfig *ksailcluster.Cluster, kindConfig *v1alpha4.Cluster) *KindClusterProvisioner {
	return &KindClusterProvisioner{
		ksailConfig: ksailConfig,
		kindConfig:  kindConfig,
		dockerProvider: cluster.NewProvider(
			cluster.ProviderWithLogger(kindcmd.NewLogger()),
		),
	}
}
