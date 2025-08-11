package ksailcluster

import (
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
	DistributionConfig string            `json:"distributionConfig,omitzero"`
	SourceDirectory    string            `json:"sourceDirectory,omitzero"`
	Connection         Connection        `json:"connection,omitzero"`
	Distribution       Distribution      `json:"distribution,omitzero"`
	ContainerEngine    ContainerEngine   `json:"containerEngine,omitzero"`
	CNI                CNI               `json:"cni,omitzero"`
	CSI                CSI               `json:"csi,omitzero"`
	IngressController  IngressController `json:"ingressController,omitzero"`
	GatewayController  GatewayController `json:"gatewayController,omitzero"`
	ReconciliationTool     ReconciliationTool    `json:"reconciliationTool,omitzero"`
	Options            Options           `json:"options,omitzero"`
}

// Connection defines connection options for a KSail cluster.
type Connection struct {
	Kubeconfig string          `json:"kubeconfig,omitzero"`
	Context    string          `json:"context,omitzero"`
	Timeout    metav1.Duration `json:"timeout,omitzero"`
}

// Distribution defines the distribution options for a KSail cluster.
type Distribution string

const (
	DistributionKind Distribution = "Kind"
	DistributionK3d  Distribution = "K3d"
	DistributionTind Distribution = "Tind"
)

// validDistributions enumerates supported distribution values.
var validDistributions = []Distribution{DistributionKind, DistributionK3d, DistributionTind}

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

// ReconciliationTool defines the Deployment Tool options for a KSail cluster.
type ReconciliationTool string

const (
	ReconciliationToolKubectl ReconciliationTool = "Kubectl"
	ReconciliationToolFlux    ReconciliationTool = "Flux"
	ReconciliationToolArgoCD  ReconciliationTool = "ArgoCD"
)

// validReconciliationTools enumerates supported reconciliation tool values.
var validReconciliationTools = []ReconciliationTool{ReconciliationToolKubectl, ReconciliationToolFlux, ReconciliationToolArgoCD}

// ContainerEngine defines the container engine used for local cluster lifecycle.
type ContainerEngine string

const (
	ContainerEngineDocker ContainerEngine = "Docker"
	ContainerEnginePodman ContainerEngine = "Podman"
)

// validContainerEngines enumerates supported container engines.
var validContainerEngines = []ContainerEngine{ContainerEngineDocker, ContainerEnginePodman}

// Options holds optional settings for distributions, networking, and deployment tools.
type Options struct {
	Kind OptionsKind `json:"kind,omitzero"`
	K3d  OptionsK3d  `json:"k3d,omitzero"`
	Tind OptionsTind `json:"talosInDocker,omitzero"`

	Cilium OptionsCilium `json:"cilium,omitzero"`

	Kubectl OptionsKubectl `json:"kubectl,omitzero"`
	Flux    OptionsFlux    `json:"flux,omitzero"`
	ArgoCD  OptionsArgoCD  `json:"argoCD,omitzero"`

	Helm      OptionsHelm      `json:"helm,omitzero"`
	Kustomize OptionsKustomize `json:"kustomize,omitzero"`
}

// OptionsKind defines options specific to the Kind distribution.
type OptionsKind struct {
	// Add any specific fields for the Kind distribution here.
}

// OptionsK3d defines options specific to the K3d distribution.
type OptionsK3d struct {
	// Add any specific fields for the K3d distribution here.
}

// OptionsTind defines options specific to the Tind distribution.
type OptionsTind struct {
	// Add any specific fields for the Tind distribution here.
}

// OptionsCilium defines options for the Cilium CNI.
type OptionsCilium struct {
	// Add any specific fields for the Cilium CNI here.
}

// OptionsKubectl defines options for the kubectl deployment tool.
type OptionsKubectl struct {
	// Add any specific fields for the Kubectl distribution here.
}

// OptionsFlux defines options for the Flux deployment tool.
type OptionsFlux struct {
	// Add any specific fields for the Flux distribution here.
}

// OptionsArgoCD defines options for the ArgoCD deployment tool.
type OptionsArgoCD struct {
	// Add any specific fields for the ArgoCD distribution here.
}

// OptionsHelm defines options for the Helm tool.
type OptionsHelm struct {
	// Add any specific fields for the Helm distribution here.
}

// OptionsKustomize defines options for the Kustomize tool.
type OptionsKustomize struct {
	// Add any specific fields for the Kustomize distribution here.
}
