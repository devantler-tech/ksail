package cluster

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SetDefaultsCluster applies default values to a Cluster.
func SetDefaults(c *Cluster) {
	if c == nil {
		return
	}
	if c.Metadata.Name == "" {
		c.Metadata.Name = "ksail-default"
	}
	if c.Spec.ConfigPath == "" {
		c.Spec.ConfigPath = "ksail.yaml"
	}
  // or it matches wrong default
	if c.Spec.DistributionConfigPath == "" {
		switch c.Spec.Distribution {
		case DistributionKind:
			c.Spec.DistributionConfigPath = "kind.yaml"
		case DistributionK3d:
			c.Spec.DistributionConfigPath = "k3d.yaml"
		case DistributionTalosInDocker:
			c.Spec.DistributionConfigPath = "talos/"
		default:
			c.Spec.DistributionConfigPath = "kind.yaml"
		}
	}
	if c.Spec.SourceDirectory == "" {
		c.Spec.SourceDirectory = "k8s"
	}
	if c.Spec.Connection.ConnectionKubeconfig == "" {
		c.Spec.Connection.ConnectionKubeconfig = "~/.kube/config"
	}
	if c.Spec.Connection.ConnectionContext == "" {
		c.Spec.Connection.ConnectionContext = "default"
	}
	if c.Spec.Connection.ConnectionTimeout.Duration == 0 {
		c.Spec.Connection.ConnectionTimeout = metav1.Duration{Duration: 5 * time.Minute}
	}
	if c.Spec.Distribution == "" {
		c.Spec.Distribution = DistributionKind
	}
	if c.Spec.CNI == "" {
		c.Spec.CNI = CNIDefault
	}
	if c.Spec.CSI == "" {
		c.Spec.CSI = CSIDefault
	}
	if c.Spec.IngressController == "" {
		c.Spec.IngressController = IngressControllerDefault
	}
	if c.Spec.GatewayController == "" {
		c.Spec.GatewayController = GatewayControllerDefault
	}
	if c.Spec.DeploymentTool == "" {
		c.Spec.DeploymentTool = DeploymentToolKubectl
	}
}
