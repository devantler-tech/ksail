package v1alpha1

import (
	"encoding/json"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MarshalYAML trims default values before emitting YAML.
func (c Cluster) MarshalYAML() (any, error) {
	pruned := pruneClusterDefaults(c)
	out := buildClusterOutput(pruned)

	return out, nil
}

// MarshalJSON trims default values before emitting JSON (used by YAML library).
func (c Cluster) MarshalJSON() ([]byte, error) {
	pruned := pruneClusterDefaults(c)

	out := buildClusterOutput(pruned)

	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal cluster to JSON: %w", err)
	}

	return b, nil
}

// buildClusterOutput converts a Cluster into a YAML/JSON-friendly projection with omitempty tags.
type clusterOutput struct {
	APIVersion string             `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	Kind       string             `json:"kind,omitempty"       yaml:"kind,omitempty"`
	Spec       *clusterSpecOutput `json:"spec,omitempty"       yaml:"spec,omitempty"`
}

type clusterSpecOutput struct {
	Distribution       string                   `json:"distribution,omitempty"       yaml:"distribution,omitempty"`
	DistributionConfig string                   `json:"distributionConfig,omitempty" yaml:"distributionConfig,omitempty"`
	SourceDirectory    string                   `json:"sourceDirectory,omitempty"    yaml:"sourceDirectory,omitempty"`
	Connection         *clusterConnectionOutput `json:"connection,omitempty"         yaml:"connection,omitempty"`
	CNI                string                   `json:"cni,omitempty"                yaml:"cni,omitempty"`
	CSI                string                   `json:"csi,omitempty"                yaml:"csi,omitempty"`
	MetricsServer      string                   `json:"metricsServer,omitempty"      yaml:"metricsServer,omitempty"`
	CertManager        string                   `json:"certManager,omitempty"        yaml:"certManager,omitempty"`
	LocalRegistry      string                   `json:"localRegistry,omitempty"      yaml:"localRegistry,omitempty"`
	GitOpsEngine       string                   `json:"gitOpsEngine,omitempty"       yaml:"gitOpsEngine,omitempty"`
	Options            *clusterOptionsOutput    `json:"options,omitempty"            yaml:"options,omitempty"`
}

type clusterConnectionOutput struct {
	Kubeconfig string `json:"kubeconfig,omitempty" yaml:"kubeconfig,omitempty"`
	Context    string `json:"context,omitempty"    yaml:"context,omitempty"`
	Timeout    string `json:"timeout,omitempty"    yaml:"timeout,omitempty"`
}

type clusterOptionsOutput struct {
	Flux          *fluxOptionsOutput          `json:"flux,omitempty"          yaml:"flux,omitempty"`
	LocalRegistry *localRegistryOptionsOutput `json:"localRegistry,omitempty" yaml:"localRegistry,omitempty"`
}

type fluxOptionsOutput struct {
	Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`
}

type localRegistryOptionsOutput struct {
	HostPort int32 `json:"hostPort,omitempty" yaml:"hostPort,omitempty"`
}

//nolint:cyclop,funlen // marshalling logic requires checking multiple optional fields
func buildClusterOutput(cluster Cluster) clusterOutput {
	var spec clusterSpecOutput

	hasSpec := false

	if cluster.Spec.Distribution != "" {
		spec.Distribution = string(cluster.Spec.Distribution)
		hasSpec = true
	}

	if trimmed := strings.TrimSpace(cluster.Spec.DistributionConfig); trimmed != "" {
		spec.DistributionConfig = trimmed
		hasSpec = true
	}

	if cluster.Spec.SourceDirectory != "" {
		spec.SourceDirectory = cluster.Spec.SourceDirectory
		hasSpec = true
	}

	var conn clusterConnectionOutput
	if cluster.Spec.Connection.Kubeconfig != "" {
		conn.Kubeconfig = cluster.Spec.Connection.Kubeconfig
	}

	if cluster.Spec.Connection.Context != "" {
		conn.Context = cluster.Spec.Connection.Context
	}

	if cluster.Spec.Connection.Timeout.Duration != 0 {
		conn.Timeout = cluster.Spec.Connection.Timeout.Duration.String()
	}

	if conn.Kubeconfig != "" || conn.Context != "" || conn.Timeout != "" {
		spec.Connection = &conn
		hasSpec = true
	}

	if cluster.Spec.CNI != "" {
		spec.CNI = string(cluster.Spec.CNI)
		hasSpec = true
	}

	if cluster.Spec.CSI != "" {
		spec.CSI = string(cluster.Spec.CSI)
		hasSpec = true
	}

	if cluster.Spec.MetricsServer != "" {
		spec.MetricsServer = string(cluster.Spec.MetricsServer)
		hasSpec = true
	}

	if cluster.Spec.CertManager != "" {
		spec.CertManager = string(cluster.Spec.CertManager)
		hasSpec = true
	}

	if cluster.Spec.LocalRegistry != "" {
		spec.LocalRegistry = string(cluster.Spec.LocalRegistry)
		hasSpec = true
	}

	if cluster.Spec.GitOpsEngine != "" {
		spec.GitOpsEngine = string(cluster.Spec.GitOpsEngine)
		hasSpec = true
	}

	var opts clusterOptionsOutput

	hasOpts := false

	if cluster.Spec.Options.Flux.Interval.Duration != 0 {
		opts.Flux = &fluxOptionsOutput{
			Interval: cluster.Spec.Options.Flux.Interval.Duration.String(),
		}
		hasOpts = true
	}

	if cluster.Spec.Options.LocalRegistry.HostPort != 0 {
		opts.LocalRegistry = &localRegistryOptionsOutput{
			HostPort: cluster.Spec.Options.LocalRegistry.HostPort,
		}

		hasOpts = true
	}

	if hasOpts {
		spec.Options = &opts
		hasSpec = true
	}

	var specPtr *clusterSpecOutput
	if hasSpec {
		specPtr = &spec
	}

	return clusterOutput{
		APIVersion: cluster.APIVersion,
		Kind:       cluster.Kind,
		Spec:       specPtr,
	}
}

// pruneClusterDefaults zeroes fields that match default values so they are omitted when marshalled.
//
//nolint:cyclop,funlen // default pruning requires checking multiple fields
func pruneClusterDefaults(cluster Cluster) Cluster {
	// Distribution defaults
	distribution := cluster.Spec.Distribution
	if distribution == "" {
		distribution = DistributionKind
	}

	if cluster.Spec.Distribution == DistributionKind {
		cluster.Spec.Distribution = ""
	}

	expectedDistConfig := ExpectedDistributionConfigName(distribution)

	trimmedConfig := strings.TrimSpace(cluster.Spec.DistributionConfig)
	if trimmedConfig == "" || trimmedConfig == expectedDistConfig {
		cluster.Spec.DistributionConfig = ""
	}

	if cluster.Spec.SourceDirectory == DefaultSourceDirectory ||
		cluster.Spec.SourceDirectory == "" {
		cluster.Spec.SourceDirectory = ""
	}

	if cluster.Spec.Connection.Kubeconfig == DefaultKubeconfigPath ||
		cluster.Spec.Connection.Kubeconfig == "" {
		cluster.Spec.Connection.Kubeconfig = ""
	}

	if defaultCtx := ExpectedContextName(distribution); cluster.Spec.Connection.Context == defaultCtx {
		cluster.Spec.Connection.Context = ""
	}

	if cluster.Spec.Connection.Timeout.Duration == 0 {
		cluster.Spec.Connection.Timeout = metav1.Duration{}
	}

	if cluster.Spec.CNI == CNIDefault {
		cluster.Spec.CNI = ""
	}

	if cluster.Spec.CSI == CSIDefault {
		cluster.Spec.CSI = ""
	}

	if cluster.Spec.MetricsServer == MetricsServerEnabled || cluster.Spec.MetricsServer == "" {
		cluster.Spec.MetricsServer = ""
	}

	if cluster.Spec.CertManager == CertManagerDisabled || cluster.Spec.CertManager == "" {
		cluster.Spec.CertManager = ""
	}

	if cluster.Spec.LocalRegistry == LocalRegistryDisabled || cluster.Spec.LocalRegistry == "" {
		cluster.Spec.LocalRegistry = ""
	}

	if cluster.Spec.GitOpsEngine == GitOpsEngineNone || cluster.Spec.GitOpsEngine == "" {
		cluster.Spec.GitOpsEngine = ""
	}

	if cluster.Spec.Options.Flux.Interval == DefaultFluxInterval ||
		cluster.Spec.Options.Flux.Interval.Duration == 0 {
		cluster.Spec.Options.Flux.Interval = metav1.Duration{}
	}

	if cluster.Spec.Options.LocalRegistry.HostPort == DefaultLocalRegistryPort ||
		cluster.Spec.Options.LocalRegistry.HostPort == 0 {
		cluster.Spec.Options.LocalRegistry.HostPort = 0
	}

	return cluster
}
