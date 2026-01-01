package cluster

import (
	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// CommandContext groups all configuration objects needed by cluster commands.
// This reduces coupling by consolidating related parameters into a single cohesive structure.
type CommandContext struct {
	ClusterCfg  *v1alpha1.Cluster
	KindConfig  *v1alpha4.Cluster
	K3dConfig   *v1alpha5.SimpleConfig
	TalosConfig *talosconfigmanager.Configs
}

// NewClusterCommandContext creates a new cluster command context from a config manager.
func NewClusterCommandContext(cfgManager *ksailconfigmanager.ConfigManager) *CommandContext {
	distConfig := cfgManager.DistributionConfig

	return &CommandContext{
		ClusterCfg:  cfgManager.Config,
		KindConfig:  distConfig.Kind,
		K3dConfig:   distConfig.K3d,
		TalosConfig: distConfig.Talos,
	}
}
