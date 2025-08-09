package cluster

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	Group      = "ksail.dev"
	Version    = "v1alpha1"
	Kind       = "Cluster"
	APIVersion = Group + "/" + Version
)

// Cluster represents a KSail cluster desired state + metadata.
type Cluster struct {
	metav1.TypeMeta `json:",inline"`
	Metadata        metav1.ObjectMeta `json:"metadata,omitzero"`
	Spec            Spec              `json:"spec,omitzero"`
}

// Spec defines the desired state of a KSail cluster.
type Spec struct {
	SourceDirectory        string `json:"sourceDirectory,omitzero"`
	Connection             `json:"connection,omitzero"`
	Distribution           Distribution      `json:"distribution,omitzero"`
	CNI                    CNI               `json:"cni,omitzero"`
	CSI                    CSI               `json:"csi,omitzero"`
	IngressController      IngressController `json:"ingressController,omitzero"`
	GatewayController      GatewayController `json:"gatewayController,omitzero"`
	DeploymentTool         DeploymentTool    `json:"deploymentTool,omitzero"`
	Options                Options           `json:"options,omitzero"`
}

// Connection defines connection options for a KSail cluster.
type Connection struct {
	Kubeconfig string          `json:"kubeconfig,omitzero"`
	Context    string          `json:"context,omitzero"`
	Timeout    metav1.Duration `json:"timeout,omitzero"`
}

// Distribution defines the distribution options for a KSail cluster.
type Distribution string

// Set implements pflag.Value.
func (d *Distribution) Set(value string) error {
	// Check against constant values with case-insensitive comparison
	for _, dist := range []Distribution{DistributionKind, DistributionK3d, DistributionTalosInDocker} {
		if strings.EqualFold(value, string(dist)) {
			*d = dist
			return nil
		}
	}

	return fmt.Errorf("invalid distribution: %s (valid options: %s, %s, %s)",
		value, DistributionKind, DistributionK3d, DistributionTalosInDocker)
}

// String implements pflag.Value.
func (d *Distribution) String() string {
	switch *d {
	case DistributionKind:
		return "Kind"
	case DistributionK3d:
		return "K3d"
	case DistributionTalosInDocker:
		return "TalosInDocker"
	default:
		return "Unknown"
	}
}

// Type implements pflag.Value.
func (d *Distribution) Type() string {
	switch *d {
	case DistributionKind:
		return "Kind"
	case DistributionK3d:
		return "K3d"
	case DistributionTalosInDocker:
		return "TalosInDocker"
	default:
		return "Unknown"
	}
}

const (
	DistributionKind          Distribution = "Kind"
	DistributionK3d           Distribution = "K3d"
	DistributionTalosInDocker Distribution = "TalosInDocker"
)

// CNI defines the CNI options for a KSail cluster.
type CNI string

const (
	CNIDefault CNI = "Default"
	CNICilium  CNI = "Cilium"
)

// CSI defines the CSI options for a KSail cluster.
type CSI string

const (
	CSIDefault          CSI = "Default"
	CSILocalPathStorage CSI = "LocalPathStorage"
)

// IngressController defines the Ingress Controller options for a KSail cluster.
type IngressController string

const (
	IngressControllerDefault IngressController = "Default"
	IngressControllerTraefik IngressController = "Traefik"
	IngressControllerNone    IngressController = "None"
)

// GatewayController defines the Gateway Controller options for a KSail cluster.
type GatewayController string

const (
	GatewayControllerDefault GatewayController = "Default"
	GatewayControllerTraefik GatewayController = "Traefik"
	GatewayControllerCilium  GatewayController = "Cilium"
	GatewayControllerNone    GatewayController = "None"
)

// DeploymentTool defines the Deployment Tool options for a KSail cluster.
type DeploymentTool string

const (
	DeploymentToolKubectl DeploymentTool = "Kubectl"
	DeploymentToolFlux    DeploymentTool = "Flux"
	DeploymentToolArgoCD  DeploymentTool = "ArgoCD"
)

// ClusterSpecFluxDeploymentTool defines the Flux deployment tool options for a KSail cluster.
type Options struct {
	Kind          OptionsKind          `json:"kind,omitzero"`
	K3d           OptionsK3d           `json:"k3d,omitzero"`
	TalosInDocker OptionsTalosInDocker `json:"talosInDocker,omitzero"`

	Cilium OptionsCilium `json:"cilium,omitzero"`

	Kubectl OptionsKubectl `json:"kubectl,omitzero"`
	Flux    OptionsFlux    `json:"flux,omitzero"`
	ArgoCD  OptionsArgoCD  `json:"argoCD,omitzero"`

	Helm      OptionsHelm      `json:"helm,omitzero"`
	Kustomize OptionsKustomize `json:"kustomize,omitzero"`
}

// OptionsKind defines the options for the Kind distribution.
type OptionsKind struct {
	// Add any specific fields for the Kind distribution here.
}

// OptionsK3d defines the options for the K3d distribution.
type OptionsK3d struct {
	// Add any specific fields for the K3d distribution here.
}

// OptionsTalosInDocker defines the options for the TalosInDocker distribution.
type OptionsTalosInDocker struct {
	// Add any specific fields for the TalosInDocker distribution here.
}

// OptionsCilium defines the options for the Cilium CNI.
type OptionsCilium struct {
	// Add any specific fields for the Cilium CNI here.
}

// OptionsKubectl defines the options for the Kubectl distribution.
type OptionsKubectl struct {
	// Add any specific fields for the Kubectl distribution here.
}

// OptionsFlux defines the options for the Flux distribution.
type OptionsFlux struct {
	// Add any specific fields for the Flux distribution here.
}

// OptionsArgoCD defines the options for the ArgoCD distribution.
type OptionsArgoCD struct {
	// Add any specific fields for the ArgoCD distribution here.
}

// OptionsHelm defines the options for the Helm distribution.
type OptionsHelm struct {
	// Add any specific fields for the Helm distribution here.
}

// OptionsKustomize defines the options for the Kustomize distribution.
type OptionsKustomize struct {
	// Add any specific fields for the Kustomize distribution here.
}

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
	SetDefaults(c)
	return c
}

func WithMetadataName(name string) func(*Cluster) {
	return func(c *Cluster) {
		c.Metadata.Name = name
	}
}

func WithSpecDistribution(distribution Distribution) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.Distribution = distribution
	}
}

func WithSpecConnectionKubeconfig(kubeconfig string) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.Connection.Kubeconfig = kubeconfig
	}
}

func WithSpecConnectionContext(context string) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.Connection.Context = context
	}
}

func WithSpecConnectionTimeout(timeout metav1.Duration) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.Connection.Timeout = timeout
	}
}

func WithSpecCNI(cni CNI) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.CNI = cni
	}
}

func WithSpecCSI(csi CSI) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.CSI = csi
	}
}

func WithSpecIngressController(ingressController IngressController) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.IngressController = ingressController
	}
}

func WithSpecGatewayController(gatewayController GatewayController) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.GatewayController = gatewayController
	}
}

func WithSpecDeploymentTool(deploymentTool DeploymentTool) func(*Cluster) {
	return func(c *Cluster) {
		c.Spec.DeploymentTool = deploymentTool
	}
}
