package configmanager

import (
	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
)

// defaultDistributionConfigPath left empty so distribution-specific defaults are applied later (Kind vs K3d).
const defaultDistributionConfigPath = ""

// FieldSelector defines a field and its metadata for configuration management.
type FieldSelector[T any] struct {
	Selector     func(*T) any // Function that returns a pointer to the field
	Description  string       // Human-readable description for CLI flags
	DefaultValue any          // Default value for the field
}

// DefaultDistributionFieldSelector creates a standard field selector for distribution.
func DefaultDistributionFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.Distribution },
		Description:  "Kubernetes distribution to use",
		DefaultValue: v1alpha1.DistributionKind,
	}
}

// StandardSourceDirectoryFieldSelector creates a standard field selector for source directory.
func StandardSourceDirectoryFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Workload.SourceDirectory },
		Description:  "Directory containing workloads to deploy",
		DefaultValue: "k8s",
	}
}

// DefaultDistributionConfigFieldSelector creates a standard field selector for distribution config.
func DefaultDistributionConfigFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.DistributionConfig },
		Description:  "Configuration file for the distribution",
		DefaultValue: defaultDistributionConfigPath,
	}
}

// DefaultContextFieldSelector creates a standard field selector for kubernetes context.
// No default value is set as the context is distribution-specific and will be
// determined by the scaffolder based on the distribution type.
func DefaultContextFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:    func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.Connection.Context },
		Description: "Kubernetes context of cluster",
	}
}

// DefaultCNIFieldSelector creates a standard field selector for CNI.
func DefaultCNIFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.CNI },
		Description:  "Container Network Interface (CNI) to use",
		DefaultValue: v1alpha1.CNIDefault,
	}
}

// DefaultGitOpsEngineFieldSelector creates a standard field selector for GitOps Engine.
func DefaultGitOpsEngineFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.GitOpsEngine },
		Description:  "GitOps engine to use (None disables GitOps, Flux installs Flux controllers, ArgoCD installs Argo CD)",
		DefaultValue: v1alpha1.GitOpsEngineNone,
	}
}

// DefaultLocalRegistryFieldSelector creates a selector for configuring the local OCI registry lifecycle.
func DefaultLocalRegistryFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector: func(c *v1alpha1.Cluster) any {
			return &c.Spec.Cluster.LocalRegistry
		},
		Description: "Local registry behavior (Enabled provisions a registry; " +
			"Disabled skips provisioning. Defaults to Enabled when " +
			"a GitOps engine is configured)",
		DefaultValue: v1alpha1.LocalRegistryDisabled,
	}
}

// DefaultRegistryPortFieldSelector creates a selector for the registry host port binding.
func DefaultRegistryPortFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector: func(c *v1alpha1.Cluster) any {
			return &c.Spec.Cluster.LocalRegistryOpts.HostPort
		},
		Description:  "Host port to expose the local OCI registry on",
		DefaultValue: v1alpha1.DefaultLocalRegistryPort,
	}
}

// DefaultMetricsServerFieldSelector creates a standard field selector for Metrics Server.
func DefaultMetricsServerFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.MetricsServer },
		Description:  "Metrics Server (Default: use distribution, Enabled: install, Disabled: uninstall)",
		DefaultValue: v1alpha1.MetricsServerDefault,
	}
}

// DefaultCertManagerFieldSelector creates a standard field selector for Cert-Manager.
func DefaultCertManagerFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.CertManager },
		Description:  "Cert-Manager configuration (Enabled: install, Disabled: skip)",
		DefaultValue: v1alpha1.CertManagerDisabled,
	}
}

// DefaultPolicyEngineFieldSelector creates a standard field selector for Policy Engine.
func DefaultPolicyEngineFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.PolicyEngine },
		Description:  "Policy engine (None: skip, Kyverno: install Kyverno, Gatekeeper: install Gatekeeper)",
		DefaultValue: v1alpha1.PolicyEngineNone,
	}
}

// DefaultCSIFieldSelector creates a standard field selector for CSI.
func DefaultCSIFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.CSI },
		Description:  "Container Storage Interface (CSI) to use",
		DefaultValue: v1alpha1.CSIDefault,
	}
}

// DefaultKubeconfigFieldSelector creates a standard field selector for kubeconfig.
func DefaultKubeconfigFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.Connection.Kubeconfig },
		Description:  "Path to kubeconfig file",
		DefaultValue: "~/.kube/config",
	}
}

// DefaultClusterFieldSelectors returns the default field selectors shared by cluster commands.
func DefaultClusterFieldSelectors() []FieldSelector[v1alpha1.Cluster] {
	return []FieldSelector[v1alpha1.Cluster]{
		DefaultDistributionFieldSelector(),
		DefaultDistributionConfigFieldSelector(),
		DefaultContextFieldSelector(),
		DefaultKubeconfigFieldSelector(),
		DefaultGitOpsEngineFieldSelector(),
		DefaultLocalRegistryFieldSelector(),
		DefaultRegistryPortFieldSelector(),
	}
}

// ControlPlanesFieldSelector returns a field selector for control-plane node count.
// This option works for all distributions: Kind, K3d, and Talos.
// For Kind/K3d, the value is applied to their native config (kind.yaml/k3d.yaml).
func ControlPlanesFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector: func(c *v1alpha1.Cluster) any {
			return &c.Spec.Cluster.Talos.ControlPlanes
		},
		Description:  "Number of control-plane nodes",
		DefaultValue: int32(1),
	}
}

// WorkersFieldSelector returns a field selector for worker node count.
// This option works for all distributions: Kind, K3d, and Talos.
// For Kind/K3d, the value is applied to their native config (kind.yaml/k3d.yaml).
func WorkersFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector: func(c *v1alpha1.Cluster) any {
			return &c.Spec.Cluster.Talos.Workers
		},
		Description:  "Number of worker nodes",
		DefaultValue: int32(0),
	}
}
