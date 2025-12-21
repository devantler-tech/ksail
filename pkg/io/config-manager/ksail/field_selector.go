package configmanager

import (
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Distribution },
		Description:  "Kubernetes distribution to use",
		DefaultValue: v1alpha1.DistributionKind,
	}
}

// StandardSourceDirectoryFieldSelector creates a standard field selector for source directory.
func StandardSourceDirectoryFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.SourceDirectory },
		Description:  "Directory containing workloads to deploy",
		DefaultValue: "k8s",
	}
}

// DefaultDistributionConfigFieldSelector creates a standard field selector for distribution config.
func DefaultDistributionConfigFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.DistributionConfig },
		Description:  "Configuration file for the distribution",
		DefaultValue: defaultDistributionConfigPath,
	}
}

// DefaultContextFieldSelector creates a standard field selector for kubernetes context.
// No default value is set as the context is distribution-specific and will be
// determined by the scaffolder based on the distribution type.
func DefaultContextFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:    func(c *v1alpha1.Cluster) any { return &c.Spec.Connection.Context },
		Description: "Kubernetes context of cluster",
	}
}

// DefaultCNIFieldSelector creates a standard field selector for CNI.
func DefaultCNIFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.CNI },
		Description:  "Container Network Interface (CNI) to use",
		DefaultValue: v1alpha1.CNIDefault,
	}
}

// DefaultGitOpsEngineFieldSelector creates a standard field selector for GitOps Engine.
func DefaultGitOpsEngineFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.GitOpsEngine },
		Description:  "GitOps engine to use (None disables GitOps, Flux installs Flux controllers, ArgoCD installs Argo CD)",
		DefaultValue: v1alpha1.GitOpsEngineNone,
	}
}

// DefaultLocalRegistryFieldSelector creates a selector for configuring the local OCI registry lifecycle.
func DefaultLocalRegistryFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector: func(c *v1alpha1.Cluster) any {
			return &c.Spec.LocalRegistry
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
			return &c.Spec.Options.LocalRegistry.HostPort
		},
		Description:  "Host port to expose the local OCI registry on",
		DefaultValue: v1alpha1.DefaultLocalRegistryPort,
	}
}

// DefaultFluxIntervalFieldSelector creates a selector for the Flux reconciliation interval.
func DefaultFluxIntervalFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector: func(c *v1alpha1.Cluster) any {
			return &c.Spec.Options.Flux.Interval
		},
		Description:  "Flux reconciliation interval (e.g. 1m, 30s)",
		DefaultValue: metav1.Duration{Duration: time.Minute},
	}
}

// DefaultMetricsServerFieldSelector creates a standard field selector for Metrics Server.
func DefaultMetricsServerFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.MetricsServer },
		Description:  "Metrics Server configuration (Enabled: install, Disabled: uninstall)",
		DefaultValue: v1alpha1.MetricsServerEnabled,
	}
}

// DefaultCertManagerFieldSelector creates a standard field selector for Cert-Manager.
func DefaultCertManagerFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.CertManager },
		Description:  "Cert-Manager configuration (Enabled: install, Disabled: skip)",
		DefaultValue: v1alpha1.CertManagerDisabled,
	}
}

// DefaultCSIFieldSelector creates a standard field selector for CSI.
func DefaultCSIFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.CSI },
		Description:  "Container Storage Interface (CSI) to use",
		DefaultValue: v1alpha1.CSIDefault,
	}
}

// DefaultKubeconfigFieldSelector creates a standard field selector for kubeconfig.
func DefaultKubeconfigFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Connection.Kubeconfig },
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
		DefaultFluxIntervalFieldSelector(),
	}
}
