package ksailcluster

import (
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// --- Constructors ---

// NewCluster creates a new KSail cluster with the given options.
func NewCluster(options ...func(*Cluster)) *Cluster {
	c := &Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       Kind,
			APIVersion: APIVersion,
		},
	}
	for _, opt := range options {
		opt(c)
	}
	c.SetDefaults()
	return c
}

// WithMetadataName sets the name of the cluster.
func WithMetadataName(name string) func(*Cluster) {
	return func(c *Cluster) {
		c.Metadata.Name = name
	}
}

// WithSpecDistribution sets the distribution of the cluster.
func WithSpecDistribution(distribution Distribution) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.Distribution = distribution
	}
}

// WithSpecConnectionKubeconfig sets the kubeconfig for the cluster.
func WithSpecConnectionKubeconfig(kubeconfig string) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.Connection.Kubeconfig = kubeconfig
	}
}

// WithSpecConnectionContext sets the context for the cluster.
func WithSpecConnectionContext(context string) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.Connection.Context = context
	}
}

// WithSpecConnectionTimeout sets the timeout for the cluster.
func WithSpecConnectionTimeout(timeout metav1.Duration) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.Connection.Timeout = timeout
	}
}

// WithSpecCNI sets the CNI for the cluster.
func WithSpecCNI(cni CNI) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.CNI = cni
	}
}

// WithSpecCSI sets the CSI implementation on the cluster spec.
func WithSpecCSI(csi CSI) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.CSI = csi
	}
}

// WithSpecIngressController sets the ingress controller on the cluster spec.
func WithSpecIngressController(ingressController IngressController) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.IngressController = ingressController
	}
}

// WithSpecGatewayController sets the gateway controller on the cluster spec.
func WithSpecGatewayController(gatewayController GatewayController) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.GatewayController = gatewayController
	}
}

// WithSpecReconciliationTool sets the deployment tool on the cluster spec.
func WithSpecReconciliationTool(reconciliationTool ReconciliationTool) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.ReconciliationTool = reconciliationTool
	}
}

// WithDistribution sets the Distribution on the cluster spec.
func WithDistribution(distribution Distribution) func(*Cluster) {
	return func(c *Cluster) { c.Spec.Distribution = distribution }
}

// WithConnectionKubeconfig sets the kubeconfig path.
func WithConnectionKubeconfig(kubeconfig string) func(*Cluster) {
	return func(c *Cluster) { c.Spec.Connection.Kubeconfig = kubeconfig }
}

// WithConnectionContext sets the kubeconfig context.
func WithConnectionContext(context string) func(*Cluster) {
	return func(c *Cluster) { c.Spec.Connection.Context = context }
}

// WithConnectionTimeout sets the connection timeout.
func WithConnectionTimeout(timeout metav1.Duration) func(*Cluster) {
	return func(c *Cluster) { c.Spec.Connection.Timeout = timeout }
}

// WithCNI sets the cluster CNI.
func WithCNI(cni CNI) func(*Cluster) { return func(c *Cluster) { c.Spec.CNI = cni } }

// WithCSI sets the cluster CSI.
func WithCSI(csi CSI) func(*Cluster) { return func(c *Cluster) { c.Spec.CSI = csi } }

// WithIngressController sets the ingress controller.
func WithIngressController(ic IngressController) func(*Cluster) {
	return func(c *Cluster) { c.Spec.IngressController = ic }
}

// WithGatewayController sets the gateway controller.
func WithGatewayController(gc GatewayController) func(*Cluster) {
	return func(c *Cluster) { c.Spec.GatewayController = gc }
}

// WithReconciliationTool sets the deployment tool.
func WithReconciliationTool(dt ReconciliationTool) func(*Cluster) {
	return func(c *Cluster) { c.Spec.ReconciliationTool = dt }
}

// --- Defaults ---

func (c *Cluster) SetDefaults() {
	if c.Metadata.Name == "" {
		c.Metadata.Name = "ksail-default"
	}
	if c.Spec.DistributionConfig == "" {
		c.Spec.DistributionConfig = "kind.yaml"
	}
	if c.Spec.SourceDirectory == "" {
		c.Spec.SourceDirectory = "k8s"
	}
	if c.Spec.Connection.Kubeconfig == "" {
		c.Spec.Connection.Kubeconfig = "~/.kube/config"
	}
	if c.Spec.Connection.Context == "" {
		c.Spec.Connection.Context = "kind-ksail-default"
	}
	if c.Spec.Connection.Timeout.Duration == 0 {
		c.Spec.Connection.Timeout = metav1.Duration{Duration: 30 * time.Second}
	}
	if c.Spec.Distribution == "" {
		c.Spec.Distribution = DistributionKind
	}
	if c.Spec.ReconciliationTool == "" {
		c.Spec.ReconciliationTool = ReconciliationToolKubectl
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
}

// --- Getters and Setters ---

// Set for Distribution
func (d *Distribution) Set(value string) error {
	// Check against constant values with case-insensitive comparison
	for _, dist := range validDistributions {
		if strings.EqualFold(value, string(dist)) {
			*d = dist
			return nil
		}
	}

	return fmt.Errorf("invalid distribution: %s (valid options: %s, %s, %s)",
		value, DistributionKind, DistributionK3d, DistributionTind)
}

// Set for ReconciliationTool
func (d *ReconciliationTool) Set(value string) error {
	// Check against constant values with case-insensitive comparison
	for _, tool := range validReconciliationTools {
		if strings.EqualFold(value, string(tool)) {
			*d = tool
			return nil
		}
	}

	return fmt.Errorf("invalid reconciliation tool: %s (valid options: %s, %s, %s)",
		value, ReconciliationToolKubectl, ReconciliationToolFlux, ReconciliationToolArgoCD)
}

// --- pflag Values ---

// String returns the string representation of the Distribution.
func (d *Distribution) String() string {
	return string(*d)
}

// Type returns the type of the Distribution.
func (d *Distribution) Type() string {
	return "Distribution"
}

// String returns the string representation of the ReconciliationTool
func (d *ReconciliationTool) String() string {
	return string(*d)
}

// Type returns the type of the ReconciliationTool.
func (d *ReconciliationTool) Type() string {
	return "ReconciliationTool"
}