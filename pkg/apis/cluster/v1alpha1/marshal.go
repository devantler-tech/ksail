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
	Editor   string                 `json:"editor,omitempty"   yaml:"editor,omitempty"`
	Cluster  *clusterSubSpecOutput  `json:"cluster,omitempty"  yaml:"cluster,omitempty"`
	Workload *workloadSubSpecOutput `json:"workload,omitempty" yaml:"workload,omitempty"`
}

type clusterSubSpecOutput struct {
	DistributionConfig string                   `json:"distributionConfig,omitempty" yaml:"distributionConfig,omitempty"`
	Connection         *clusterConnectionOutput `json:"connection,omitempty"         yaml:"connection,omitempty"`
	Distribution       string                   `json:"distribution,omitempty"       yaml:"distribution,omitempty"`
	CNI                string                   `json:"cni,omitempty"                yaml:"cni,omitempty"`
	CSI                string                   `json:"csi,omitempty"                yaml:"csi,omitempty"`
	MetricsServer      string                   `json:"metricsServer,omitempty"      yaml:"metricsServer,omitempty"`
	CertManager        string                   `json:"certManager,omitempty"        yaml:"certManager,omitempty"`
	LocalRegistry      string                   `json:"localRegistry,omitempty"      yaml:"localRegistry,omitempty"`
	GitOpsEngine       string                   `json:"gitOpsEngine,omitempty"       yaml:"gitOpsEngine,omitempty"`
	Options            *clusterOptionsOutput    `json:"options,omitempty"            yaml:"options,omitempty"`
}

type workloadSubSpecOutput struct {
	SourceDirectory string `json:"sourceDirectory,omitempty" yaml:"sourceDirectory,omitempty"`
}

type clusterConnectionOutput struct {
	Kubeconfig string `json:"kubeconfig,omitempty" yaml:"kubeconfig,omitempty"`
	Context    string `json:"context,omitempty"    yaml:"context,omitempty"`
	Timeout    string `json:"timeout,omitempty"    yaml:"timeout,omitempty"`
}

type clusterOptionsOutput struct {
	LocalRegistry *localRegistryOptionsOutput `json:"localRegistry,omitempty" yaml:"localRegistry,omitempty"`
}

type localRegistryOptionsOutput struct {
	HostPort int32 `json:"hostPort,omitempty" yaml:"hostPort,omitempty"`
}

//nolint:cyclop,funlen // marshalling logic requires checking multiple optional fields
func buildClusterOutput(cluster Cluster) clusterOutput {
	var spec clusterSpecOutput

	hasSpec := false

	// Editor field (top-level)
	if cluster.Spec.Editor != "" {
		spec.Editor = cluster.Spec.Editor
		hasSpec = true
	}

	// Build cluster sub-spec
	var clusterSubSpec clusterSubSpecOutput

	hasClusterSubSpec := false

	if cluster.Spec.Cluster.Distribution != "" {
		clusterSubSpec.Distribution = string(cluster.Spec.Cluster.Distribution)
		hasClusterSubSpec = true
	}

	if trimmed := strings.TrimSpace(cluster.Spec.Cluster.DistributionConfig); trimmed != "" {
		clusterSubSpec.DistributionConfig = trimmed
		hasClusterSubSpec = true
	}

	var conn clusterConnectionOutput
	if cluster.Spec.Cluster.Connection.Kubeconfig != "" {
		conn.Kubeconfig = cluster.Spec.Cluster.Connection.Kubeconfig
	}

	if cluster.Spec.Cluster.Connection.Context != "" {
		conn.Context = cluster.Spec.Cluster.Connection.Context
	}

	if cluster.Spec.Cluster.Connection.Timeout.Duration != 0 {
		conn.Timeout = cluster.Spec.Cluster.Connection.Timeout.Duration.String()
	}

	if conn.Kubeconfig != "" || conn.Context != "" || conn.Timeout != "" {
		clusterSubSpec.Connection = &conn
		hasClusterSubSpec = true
	}

	if cluster.Spec.Cluster.CNI != "" {
		clusterSubSpec.CNI = string(cluster.Spec.Cluster.CNI)
		hasClusterSubSpec = true
	}

	if cluster.Spec.Cluster.CSI != "" {
		clusterSubSpec.CSI = string(cluster.Spec.Cluster.CSI)
		hasClusterSubSpec = true
	}

	if cluster.Spec.Cluster.MetricsServer != "" {
		clusterSubSpec.MetricsServer = string(cluster.Spec.Cluster.MetricsServer)
		hasClusterSubSpec = true
	}

	if cluster.Spec.Cluster.CertManager != "" {
		clusterSubSpec.CertManager = string(cluster.Spec.Cluster.CertManager)
		hasClusterSubSpec = true
	}

	if cluster.Spec.Cluster.LocalRegistry != "" {
		clusterSubSpec.LocalRegistry = string(cluster.Spec.Cluster.LocalRegistry)
		hasClusterSubSpec = true
	}

	if cluster.Spec.Cluster.GitOpsEngine != "" {
		clusterSubSpec.GitOpsEngine = string(cluster.Spec.Cluster.GitOpsEngine)
		hasClusterSubSpec = true
	}

	var opts clusterOptionsOutput

	hasOpts := false

	if cluster.Spec.Cluster.LocalRegistryOpts.HostPort != 0 {
		opts.LocalRegistry = &localRegistryOptionsOutput{
			HostPort: cluster.Spec.Cluster.LocalRegistryOpts.HostPort,
		}

		hasOpts = true
	}

	if hasOpts {
		clusterSubSpec.Options = &opts
		hasClusterSubSpec = true
	}

	if hasClusterSubSpec {
		spec.Cluster = &clusterSubSpec
		hasSpec = true
	}

	// Build workload sub-spec
	var workloadSubSpec workloadSubSpecOutput

	hasWorkloadSubSpec := false

	if cluster.Spec.Workload.SourceDirectory != "" {
		workloadSubSpec.SourceDirectory = cluster.Spec.Workload.SourceDirectory
		hasWorkloadSubSpec = true
	}

	if hasWorkloadSubSpec {
		spec.Workload = &workloadSubSpec
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
	distribution := cluster.Spec.Cluster.Distribution
	if distribution == "" {
		distribution = DistributionKind
	}

	if cluster.Spec.Cluster.Distribution == DistributionKind {
		cluster.Spec.Cluster.Distribution = ""
	}

	expectedDistConfig := ExpectedDistributionConfigName(distribution)

	trimmedConfig := strings.TrimSpace(cluster.Spec.Cluster.DistributionConfig)
	if trimmedConfig == "" || trimmedConfig == expectedDistConfig {
		cluster.Spec.Cluster.DistributionConfig = ""
	}

	if cluster.Spec.Workload.SourceDirectory == DefaultSourceDirectory ||
		cluster.Spec.Workload.SourceDirectory == "" {
		cluster.Spec.Workload.SourceDirectory = ""
	}

	if cluster.Spec.Cluster.Connection.Kubeconfig == DefaultKubeconfigPath ||
		cluster.Spec.Cluster.Connection.Kubeconfig == "" {
		cluster.Spec.Cluster.Connection.Kubeconfig = ""
	}

	if defaultCtx := ExpectedContextName(distribution); cluster.Spec.Cluster.Connection.Context == defaultCtx {
		cluster.Spec.Cluster.Connection.Context = ""
	}

	if cluster.Spec.Cluster.Connection.Timeout.Duration == 0 {
		cluster.Spec.Cluster.Connection.Timeout = metav1.Duration{}
	}

	if cluster.Spec.Cluster.CNI == CNIDefault {
		cluster.Spec.Cluster.CNI = ""
	}

	if cluster.Spec.Cluster.CSI == CSIDefault {
		cluster.Spec.Cluster.CSI = ""
	}

	if cluster.Spec.Cluster.MetricsServer == MetricsServerEnabled ||
		cluster.Spec.Cluster.MetricsServer == "" {
		cluster.Spec.Cluster.MetricsServer = ""
	}

	if cluster.Spec.Cluster.CertManager == CertManagerDisabled ||
		cluster.Spec.Cluster.CertManager == "" {
		cluster.Spec.Cluster.CertManager = ""
	}

	if cluster.Spec.Cluster.LocalRegistry == LocalRegistryDisabled ||
		cluster.Spec.Cluster.LocalRegistry == "" {
		cluster.Spec.Cluster.LocalRegistry = ""
	}

	if cluster.Spec.Cluster.GitOpsEngine == GitOpsEngineNone ||
		cluster.Spec.Cluster.GitOpsEngine == "" {
		cluster.Spec.Cluster.GitOpsEngine = ""
	}

	if cluster.Spec.Cluster.LocalRegistryOpts.HostPort == DefaultLocalRegistryPort ||
		cluster.Spec.Cluster.LocalRegistryOpts.HostPort == 0 {
		cluster.Spec.Cluster.LocalRegistryOpts.HostPort = 0
	}

	return cluster
}
