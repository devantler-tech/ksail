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
	if c.Spec.SourceDirectory == "" {
		c.Spec.SourceDirectory = "k8s"
	}
	if c.Spec.Connection.Kubeconfig == "" {
		c.Spec.Connection.Kubeconfig = "~/.kube/config"
	}
	if c.Spec.Connection.Context == "" {
		c.Spec.Connection.Context = "default"
	}
	if c.Spec.Connection.Timeout.Duration == 0 {
		c.Spec.Connection.Timeout = metav1.Duration{Duration: 5 * time.Minute}
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
