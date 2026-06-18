package configmanager

import (
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

// defaultDistributionConfigPath left empty so distribution-specific defaults are applied later (Kind vs K3d).
const defaultDistributionConfigPath = ""

// FieldSelector defines a field and its metadata for configuration management.
type FieldSelector[T any] struct {
	Selector     func(*T) any // Function that returns a pointer to the field
	FlagName     string       // CLI flag name (e.g. "distribution"); required for flag registration
	Shorthand    string       // Optional single-character CLI shorthand (e.g. "d"); "" for none
	Description  string       // Human-readable description for CLI flags
	DefaultValue any          // Default value for the field
	// BareFlagValue, when non-empty, is the value a valueless (bare) flag form
	// resolves to (pflag NoOptDefVal). It restores the bare-flag behavior of a
	// toggle field that was a bool before migrating to a string enum: pflag only
	// auto-applies the no-argument form to bool-typed flags, so without this an
	// enum-typed --foo (registered via Var) would fail with "flag needs an
	// argument". Set it to the enum's "on" value (e.g. "Enabled") so existing
	// scripts invoking the bare flag keep working through the deprecation window.
	BareFlagValue string
}

// DefaultDistributionFieldSelector creates a standard field selector for distribution.
func DefaultDistributionFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.Distribution },
		FlagName:     "distribution",
		Shorthand:    "d",
		Description:  "Kubernetes distribution to use",
		DefaultValue: v1alpha1.DistributionVanilla,
	}
}

// DefaultProviderFieldSelector creates a standard field selector for infrastructure provider.
//
// It intentionally carries no shorthand: the "-p"=--provider shorthand is added
// only by the mutation commands (create/update/init) via
// WithProviderShorthand, matching their lifecycle siblings (delete/list/start/
// stop/info/diagnose) without flipping it on for read-only consumers like
// `workload images` that share this selector.
func DefaultProviderFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.Provider },
		FlagName:     "provider",
		Description:  "Infrastructure provider backend (e.g., Docker)",
		DefaultValue: v1alpha1.ProviderDocker,
	}
}

// WithProviderShorthand returns a copy of the given provider field selector with
// the "-p" shorthand set. The mutation commands use it so create/update/init
// expose -p=--provider like their lifecycle siblings, while shared read-only
// consumers of DefaultProviderFieldSelector keep the long flag only.
func WithProviderShorthand(
	selector FieldSelector[v1alpha1.Cluster],
) FieldSelector[v1alpha1.Cluster] {
	selector.Shorthand = "p"

	return selector
}

// StandardSourceDirectoryFieldSelector creates a standard field selector for source directory.
func StandardSourceDirectoryFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Workload.SourceDirectory },
		FlagName:     "source-directory",
		Shorthand:    "s",
		Description:  "Directory containing workloads to deploy",
		DefaultValue: v1alpha1.DefaultSourceDirectory,
	}
}

// StandardKustomizationFileFieldSelector creates a standard field selector for the kustomization path
// within the source directory (directory used as the kustomize entry point).
func StandardKustomizationFileFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Workload.KustomizationFile },
		FlagName:     "kustomization-file",
		Description:  "Relative directory within sourceDirectory used as the kustomize entry point (e.g., clusters/local)",
		DefaultValue: "",
	}
}

// DefaultDistributionConfigFieldSelector creates a standard field selector for distribution config.
func DefaultDistributionConfigFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.DistributionConfig },
		FlagName:     "distribution-config",
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
		FlagName:    "context",
		Shorthand:   "c",
		Description: "Kubernetes context of cluster",
	}
}

// DefaultCNIFieldSelector creates a standard field selector for CNI.
func DefaultCNIFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.CNI },
		FlagName:     "cni",
		Description:  "Container Network Interface (CNI) to use",
		DefaultValue: v1alpha1.CNIDefault,
	}
}

// DefaultGitOpsEngineFieldSelector creates a standard field selector for GitOps Engine.
func DefaultGitOpsEngineFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.GitOpsEngine },
		FlagName:     "gitops-engine",
		Shorthand:    "g",
		Description:  "GitOps engine to use (None disables GitOps, Flux installs Flux controllers, ArgoCD installs Argo CD)",
		DefaultValue: v1alpha1.GitOpsEngineNone,
	}
}

// DefaultLocalRegistryFieldSelector creates a selector for the local OCI registry specification.
// Format: [user:pass@]host[:port][/path].
func DefaultLocalRegistryFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector: func(c *v1alpha1.Cluster) any {
			return &c.Spec.Cluster.LocalRegistry.Registry
		},
		FlagName: "local-registry",
		Description: "Local registry specification: [user:pass@]host[:port][/path] " +
			"(e.g., localhost:5050, ghcr.io/myorg, ${USER}:${PASS}@ghcr.io:443/org)",
		DefaultValue: "",
	}
}

// DefaultMetricsServerFieldSelector creates a standard field selector for Metrics Server.
func DefaultMetricsServerFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.MetricsServer },
		FlagName:     "metrics-server",
		Description:  "Metrics Server (Default: use distribution, Enabled: install, Disabled: uninstall)",
		DefaultValue: v1alpha1.MetricsServerDefault,
	}
}

// DefaultLoadBalancerFieldSelector creates a standard field selector for LoadBalancer.
func DefaultLoadBalancerFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.LoadBalancer },
		FlagName:     "load-balancer",
		Description:  "LoadBalancer support (Default: use distribution × provider, Enabled: install, Disabled: uninstall)",
		DefaultValue: v1alpha1.LoadBalancerDefault,
	}
}

// DefaultCertManagerFieldSelector creates a standard field selector for Cert-Manager.
func DefaultCertManagerFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.CertManager },
		FlagName:     "cert-manager",
		Description:  "Cert-Manager configuration (Enabled: install, Disabled: skip)",
		DefaultValue: v1alpha1.CertManagerDisabled,
	}
}

// DefaultPolicyEngineFieldSelector creates a standard field selector for Policy Engine.
func DefaultPolicyEngineFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.PolicyEngine },
		FlagName:     "policy-engine",
		Description:  "Policy engine (None: skip, Kyverno: install Kyverno, Gatekeeper: install Gatekeeper)",
		DefaultValue: v1alpha1.PolicyEngineNone,
	}
}

// DefaultCSIFieldSelector creates a standard field selector for CSI.
func DefaultCSIFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.CSI },
		FlagName:     "csi",
		Description:  "Container Storage Interface (Default: use distribution, Enabled: install CSI, Disabled: skip CSI)",
		DefaultValue: v1alpha1.CSIDefault,
	}
}

// DefaultCDIFieldSelector creates a standard field selector for CDI.
func DefaultCDIFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.CDI },
		FlagName:     "cdi",
		Description:  "Container Device Interface (Default: use distribution, Enabled: enable CDI, Disabled: disable CDI)",
		DefaultValue: v1alpha1.CDIDefault,
	}
}

// DefaultKubeconfigFieldSelector creates a standard field selector for kubeconfig.
func DefaultKubeconfigFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.Connection.Kubeconfig },
		FlagName:     "kubeconfig",
		Shorthand:    "k",
		Description:  "Path to kubeconfig file",
		DefaultValue: v1alpha1.DefaultKubeconfigPath,
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
	}
}

// ControlPlanesFieldSelector returns a field selector for control-plane node count.
// This option works for all distributions: Kind, K3d, and Talos.
// For Kind/K3d, the value is applied to their native config (kind.yaml/k3d.yaml).
func ControlPlanesFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector: func(c *v1alpha1.Cluster) any {
			return &c.Spec.Cluster.ControlPlanes
		},
		FlagName:     "control-planes",
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
			return &c.Spec.Cluster.Workers
		},
		FlagName:     "workers",
		Description:  "Number of worker nodes",
		DefaultValue: int32(0),
	}
}

// KubernetesVersionFieldSelector returns a field selector for the Kubernetes
// version (spec.cluster.kubernetesVersion).
//
// DefaultValue is intentionally omitted so an unset version stays at its zero
// value: `cluster create`/`cluster update` then follow the latest supported
// Kubernetes version, while a set value pins it. Writing a default here would
// make "unset" indistinguishable from an explicit pin.
func KubernetesVersionFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector: func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.KubernetesVersion },
		FlagName: "kubernetes-version",
		Description: "Kubernetes version to deploy and reconcile toward. When unset KSail follows " +
			"the latest supported version; set it to pin a specific version. Honored by the Talos " +
			"distribution; Kind/K3d/EKS carry the version in their distribution config instead.",
	}
}

// DistributionVersionFieldSelector returns a field selector for the distribution
// version (spec.cluster.talos.version, the Talos OS version).
//
// DefaultValue is intentionally omitted so an unset version stays at its zero
// value: `cluster create`/`cluster update` then follow the latest supported
// version, while a set value pins it.
func DistributionVersionFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector: func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.Talos.Version },
		FlagName: "distribution-version",
		Description: "Distribution version to deploy and reconcile toward (Talos OS version). When " +
			"unset KSail follows the latest supported version; set it to pin a specific version. " +
			"Other distributions carry their version in the distribution config.",
	}
}

// DrainTimeoutFieldSelector returns a field selector for the per-node drain
// timeout (spec.cluster.talos.drainTimeout) used during `cluster update`.
//
// DefaultValue is intentionally omitted so an unset value stays at its zero
// duration: the Talos provisioner then applies its built-in 10m default. Writing
// a default here would eagerly populate the spec field, which is unnecessary since
// the fallback lives at the provisioner layer.
func DrainTimeoutFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector: func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.Talos.DrainTimeout },
		FlagName: "drain-timeout",
		Description: "Per-node pod-eviction budget for rolling node drains during cluster update " +
			"(default 10m when unset). Increase it for stateful workloads that need longer to evict " +
			"gracefully (e.g. Longhorn rebuilds, database failovers). On timeout the update aborts; " +
			"re-run with --force-drain to delete pods bypassing PodDisruptionBudgets. Talos only.",
	}
}

// DefaultImportImagesFieldSelector creates a standard field selector for import-images.
func DefaultImportImagesFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector: func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.ImportImages },
		FlagName: "import-images",
		Description: "Path to tar archive with container images to import after cluster creation " +
			"but before component installation",
		DefaultValue: "",
	}
}

// ImageVerificationFieldSelector creates a field selector for image verification.
func ImageVerificationFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector: func(c *v1alpha1.Cluster) any {
			return &c.Spec.Cluster.ImageVerification
		},
		FlagName: "image-verification",
		Description: "Image verification (Talos: scaffold ImageVerificationConfig template; " +
			"Vanilla/Kind: inject containerd verifier plugin patch; requires verifier binaries " +
			"and typically policy to be present in the node image bin_dir; " +
			"K3s/K3d: scaffold containerd config template with image verifier plugin and mount into node containers; " +
			"requires verifier binaries and typically policy to be present in the node image bin_dir; " +
			"Disabled: skip)",
		DefaultValue: v1alpha1.ImageVerificationDisabled,
	}
}

// NodeAutoscalingFieldSelector creates a field selector for node autoscaling.
//
// Deprecated: use [NodeAutoscalerEnabledFieldSelector] instead.
//
// DefaultValue is intentionally omitted: setting a default here would eagerly write
// "Disabled" into the Config struct during AddFlagsFromFields (before any config file
// is read). That causes migrateDeprecatedNodeAutoscaling to see old="Disabled" even
// when the user only set the new autoscaler.node.enabled field, resulting in a false
// conflict error. The migration's `if *old == ""` guard correctly skips the field when
// it is left at its zero value.
func NodeAutoscalingFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector: func(c *v1alpha1.Cluster) any {
			return &c.Spec.Cluster.NodeAutoscaling
		},
		FlagName: "node-autoscaling",
		Description: "[Deprecated: use autoscaler.node.enabled instead] Node autoscaling " +
			"(Talos: Enabled defers worker and control-plane scaling to an external autoscaler, " +
			"Disabled lets KSail manage node counts; other distributions currently ignore this setting)",
	}
}

// NodeAutoscalerEnabledFieldSelector creates a field selector for the node autoscaler enabled flag.
//
// The field is the NodeAutoscalerEnabled toggle enum; the --node-autoscaler-enabled
// flag therefore takes an Enabled/Disabled value (a YAML boolean is still accepted
// in ksail.yaml via the toggle bool-alias decode hook). DefaultValue is the enum's
// Disabled value; the field-selector default loop fills it in (post-migration) when
// the field is left at its zero value.
func NodeAutoscalerEnabledFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector: func(c *v1alpha1.Cluster) any {
			return &c.Spec.Cluster.Autoscaler.Node.Enabled
		},
		FlagName: "node-autoscaler-enabled",
		Description: "Node autoscaling " +
			"(Talos: Enabled defers worker and control-plane scaling to an external autoscaler, " +
			"Disabled lets KSail manage node counts; other distributions currently ignore this setting)",
		DefaultValue: v1alpha1.NodeAutoscalerEnabledDisabled,
		// This field was a bool flag before the bool->enum migration; keep the bare
		// `--node-autoscaler-enabled` form (no value) meaning Enabled so existing
		// scripts that relied on the old bool flag's no-argument form keep working.
		BareFlagValue: string(v1alpha1.NodeAutoscalerEnabledEnabled),
	}
}

// OIDCIssuerURLFieldSelector creates a field selector for the OIDC issuer URL.
func OIDCIssuerURLFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.OIDC.IssuerURL },
		FlagName:     "oidc-issuer-url",
		Description:  "OIDC provider issuer URL (e.g. https://dex.example.com)",
		DefaultValue: "",
	}
}

// OIDCClientIDFieldSelector creates a field selector for the OIDC client ID.
func OIDCClientIDFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.OIDC.ClientID },
		FlagName:     "oidc-client-id",
		Description:  "OIDC client ID for kubectl authentication",
		DefaultValue: "",
	}
}

// OIDCUsernameClaimFieldSelector creates a field selector for the OIDC username claim.
func OIDCUsernameClaimFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.OIDC.UsernameClaim },
		FlagName:     "oidc-username-claim",
		Description:  "JWT claim for Kubernetes username",
		DefaultValue: v1alpha1.DefaultOIDCUsernameClaim,
	}
}

// OIDCGroupsClaimFieldSelector creates a field selector for the OIDC groups claim.
func OIDCGroupsClaimFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.OIDC.GroupsClaim },
		FlagName:     "oidc-groups-claim",
		Description:  "JWT claim for Kubernetes groups",
		DefaultValue: v1alpha1.DefaultOIDCGroupsClaim,
	}
}

// OIDCCAFileFieldSelector creates a field selector for the OIDC CA file.
func OIDCCAFileFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.OIDC.CAFile },
		FlagName:     "oidc-ca-file",
		Description:  "Path to CA certificate for self-signed OIDC providers",
		DefaultValue: "",
	}
}

// OIDCUsernamePrefixFieldSelector creates a field selector for the OIDC username prefix.
func OIDCUsernamePrefixFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.OIDC.UsernamePrefix },
		FlagName:     "oidc-username-prefix",
		Description:  "Prefix for OIDC usernames in Kubernetes",
		DefaultValue: v1alpha1.DefaultOIDCUsernamePrefix,
	}
}

// OIDCGroupsPrefixFieldSelector creates a field selector for the OIDC groups prefix.
func OIDCGroupsPrefixFieldSelector() FieldSelector[v1alpha1.Cluster] {
	return FieldSelector[v1alpha1.Cluster]{
		Selector:     func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.OIDC.GroupsPrefix },
		FlagName:     "oidc-groups-prefix",
		Description:  "Prefix for OIDC groups in Kubernetes",
		DefaultValue: v1alpha1.DefaultOIDCGroupsPrefix,
	}
}
