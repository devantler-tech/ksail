package clusterprovisioner

import (
	"context"
	"slices"

	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
	k3dclient "github.com/k3d-io/k3d/v5/pkg/client"
	k3dconfig "github.com/k3d-io/k3d/v5/pkg/config"
	conf "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	k3drt "github.com/k3d-io/k3d/v5/pkg/runtimes"
	k3dtypes "github.com/k3d-io/k3d/v5/pkg/types"
)

// K3dClusterProvisioner implements provisioning for k3d clusters.
type K3dClusterProvisioner struct {
	cfg *ksailcluster.Cluster
	simpleCfg   *conf.SimpleConfig
}

var _ ClusterProvisioner = (*K3dClusterProvisioner)(nil)

// Create provisions a k3d cluster using the loaded SimpleConfig.
func (k *K3dClusterProvisioner) Create(name string) error {
	ctx := context.Background()
	rt := k3drt.SelectedRuntime

	// Ensure name in SimpleConfig; default to ksail name
	target := name
	if target == "" {
		target = k.cfg.Metadata.Name
	}
	k.simpleCfg.Name = target

	// Transform SimpleConfig -> ClusterConfig
	clusterCfg, err := k3dconfig.TransformSimpleToClusterConfig(ctx, rt, *k.simpleCfg, "k3d.yaml")
	if err != nil {
		return err
	}

	// Default kubeconfig options similar to CLI
	clusterCfg.KubeconfigOpts.UpdateDefaultKubeconfig = true
	clusterCfg.KubeconfigOpts.SwitchCurrentContext = true

	// Run full create sequence
	if err := k3dclient.ClusterRun(ctx, rt, clusterCfg); err != nil {
		return err
	}
	return nil
}

// Delete tears down a k3d cluster.
func (k *K3dClusterProvisioner) Delete(name string) error {
	ctx := context.Background()
	rt := k3drt.SelectedRuntime
	target := name
	if target == "" {
		target = k.cfg.Metadata.Name
	}
	cluster := &k3dtypes.Cluster{Name: target}
	return k3dclient.ClusterDelete(ctx, rt, cluster, k3dtypes.ClusterDeleteOpts{})
}

// Start starts an existing k3d cluster.
func (k *K3dClusterProvisioner) Start(name string) error {
	ctx := context.Background()
	rt := k3drt.SelectedRuntime
	target := name
	if target == "" {
		target = k.cfg.Metadata.Name
	}
	c, err := k3dclient.ClusterGet(ctx, rt, &k3dtypes.Cluster{Name: target})
	if err != nil {
		return err
	}
	return k3dclient.ClusterStart(ctx, rt, c, k3dtypes.ClusterStartOpts{})
}

// Stop stops a running k3d cluster.
func (k *K3dClusterProvisioner) Stop(name string) error {
	ctx := context.Background()
	rt := k3drt.SelectedRuntime
	target := name
	if target == "" {
		target = k.cfg.Metadata.Name
	}
	c, err := k3dclient.ClusterGet(ctx, rt, &k3dtypes.Cluster{Name: target})
	if err != nil {
		return err
	}
	return k3dclient.ClusterStop(ctx, rt, c)
}

// List returns cluster names managed by k3d.
func (k *K3dClusterProvisioner) List() ([]string, error) {
	ctx := context.Background()
	rt := k3drt.SelectedRuntime
	clusters, err := k3dclient.ClusterList(ctx, rt)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(clusters))
	for _, c := range clusters {
		out = append(out, c.Name)
	}
	return out, nil
}

// Exists returns whether the ksail cluster name exists in k3d.
func (k *K3dClusterProvisioner) Exists(name string) (bool, error) {
	clusters, err := k.List()
	if err != nil {
		return false, err
	}
	target := name
	if target == "" {
		target = k.cfg.Metadata.Name
	}
	return slices.Contains(clusters, target), nil
}

// NewK3dClusterProvisioner constructs a k3d provisioner instance.
func NewK3dClusterProvisioner(cfg *ksailcluster.Cluster, simpleCfg *conf.SimpleConfig) *K3dClusterProvisioner {
	return &K3dClusterProvisioner{cfg: cfg, simpleCfg: simpleCfg}
}
