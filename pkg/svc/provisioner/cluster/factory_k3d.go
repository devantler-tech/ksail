package clusterprovisioner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	k3dconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/k3d"
	k3dprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/k3d"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"sigs.k8s.io/yaml"
)

// wrapK3kServerArgs works around a k3k operator bug: it joins Cluster.Spec.ServerArgs
// with spaces into an unquoted shell assignment (EXTRA_ARGS={{.EXTRA_ARGS}}), which
// breaks for multiple space-separated args (the shell treats only the first token as the
// assignment and the rest as a command). Wrapping the args in a single single-quoted
// element makes the assignment see one token, while the operator's later unquoted
// `$EXTRA_ARGS` expansion word-splits it back into separate k3s flags. Returns nil for
// empty input.
func wrapK3kServerArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}

	return []string{"'" + strings.Join(args, " ") + "'"}
}

// createK3dKubernetesProvisioner creates a K3s provisioner that runs inside
// a host Kubernetes cluster using the k3k operator.
func (f DefaultFactory) createK3dKubernetesProvisioner(
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
	opts := cluster.Spec.Provider.Kubernetes

	// resolveClusterNameFromContext normally extracts from k3dConfig.Name, but k3d
	// config is skipped for the Kubernetes provider path. Derive from the connection
	// context (set by applyClusterNameOverride). The Kubernetes provider writes a
	// "k3k-<name>" context (via the k3k operator); standalone paths use "k3d-<name>".
	// Strip whichever prefix is present so the k3k cluster/namespace name is correct.
	clusterName := strings.TrimPrefix(cluster.Spec.Cluster.Connection.Context, "k3k-")
	clusterName = strings.TrimPrefix(clusterName, "k3d-")

	if clusterName == "" {
		clusterName = cluster.Name
	}

	hostClient, restConfig, dynClient, k8sProvider, err := buildKubernetesInfra(opts)
	if err != nil {
		return nil, nil, err
	}

	controlPlanes := cluster.Spec.Cluster.ControlPlanes
	if controlPlanes <= 0 {
		controlPlanes = 1
	}

	workers := cluster.Spec.Cluster.Workers

	// Calico's v3.30+ CRD chart ships MutatingAdmissionPolicy resources that require the
	// MutatingAdmissionPolicy feature gate / v1beta1 admissionregistration API. These flags
	// flow through the k3k Cluster spec's serverArgs to the embedded k3s kube-apiserver.
	//
	// The k3k operator joins serverArgs with spaces into an UNQUOTED shell assignment
	// (EXTRA_ARGS={{.EXTRA_ARGS}}), which breaks for multiple space-separated args (the
	// shell treats only the first token as the assignment and the rest as a command), so
	// the feature-gate flags never reach `k3s server $EXTRA_ARGS`. Wrap them in a single
	// single-quoted element: the assignment then sees one token (EXTRA_ARGS='a b' → "a b")
	// and the later unquoted $EXTRA_ARGS expansion word-splits it back into separate flags.
	serverArgs := wrapK3kServerArgs(
		k3dconfigmanager.APIServerFeatureGatesArgsForCNI(cluster.Spec.Cluster.CNI),
	)

	provisioner, err := k3dprovisioner.NewK3kProvisioner(
		k3dprovisioner.K3kProvisionerConfig{
			HostClientset:    hostClient,
			RestConfig:       restConfig,
			K8sProvider:      k8sProvider,
			DynamicClient:    dynClient,
			ClusterName:      clusterName,
			KubeconfigPath:   cluster.Spec.Cluster.Connection.Kubeconfig,
			HostContext:      resolveKubernetesOption(opts.Context, opts.ContextEnvVar),
			GatewayClassName: opts.GatewayClassName,
			ControlPlanes:    controlPlanes,
			Workers:          workers,
			PodCIDR:          opts.PodCIDR,
			ServiceCIDR:      opts.ServiceCIDR,
			ServerArgs:       serverArgs,
			// Pin the nested K3s version to the standalone K3d version so the nested
			// apiserver matches the proven standalone config (serves the
			// admissionregistration.k8s.io/v1beta1 API that Calico's CRD chart needs),
			// instead of inheriting the host cluster's possibly-older version.
			K3sVersion: k3dconfigmanager.DefaultK3sVersion(),
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create K3k provisioner: %w", err)
	}

	if f.ComponentDetector != nil {
		provisioner.WithComponentDetector(f.ComponentDetector)
	}

	return provisioner, nil, nil
}

func (f DefaultFactory) createK3dProvisioner( //nolint:funlen // sequential setup steps
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
	// Kubernetes provider: use k3k operator instead of Docker-based K3d
	if cluster.Spec.Cluster.Provider == v1alpha1.ProviderKubernetes {
		return f.createK3dKubernetesProvisioner(cluster)
	}

	if f.DistributionConfig.K3d == nil {
		return nil, nil, fmt.Errorf(
			"k3d config is required for K3d distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	k3dConfig := f.DistributionConfig.K3d

	// Apply node count overrides from CLI flags / cluster-level config.
	applyK3dNodeCounts(k3dConfig, cluster.Spec.Cluster.ControlPlanes, cluster.Spec.Cluster.Workers)

	// Enable the MutatingAdmissionPolicy feature gate / v1beta1 admissionregistration
	// API on the server nodes only for Calico, whose v3.30+ CRD chart ships
	// MutatingAdmissionPolicy resources. Enabling it elsewhere makes other components
	// (e.g. Kyverno) attempt to use the API and fail.
	if cluster.Spec.Cluster.CNI == v1alpha1.CNICalico {
		k3dconfigmanager.ApplyAPIServerFeatureGatesArgs(k3dConfig)
	}

	// Apply containerd image verifier plugin volume mount when image verification is enabled.
	// This mounts the generated config.toml.tmpl into K3d node containers so K3s uses it
	// to generate the final containerd config with the image verifier plugin enabled.
	if cluster.Spec.Cluster.Talos.ImageVerification == v1alpha1.ImageVerificationEnabled {
		templatePath := filepath.Join(k3dconfigmanager.DefaultImageVerifierDir, "config.toml.tmpl")

		absTemplatePath, err := filepath.Abs(templatePath)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"failed to resolve k3d image verification template path %q: %w",
				templatePath,
				err,
			)
		}

		fileInfo, err := os.Stat(absTemplatePath)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"k3d image verification template not found at %q; run 'ksail cluster init' to generate it: %w",
				absTemplatePath,
				err,
			)
		}

		if !fileInfo.Mode().IsRegular() {
			return nil, nil, fmt.Errorf(
				"%w: %s; remove it and re-run 'ksail cluster init'",
				ErrImageVerificationTemplateNotRegularFile,
				absTemplatePath,
			)
		}

		k3dconfigmanager.ApplyImageVerificationVolumes(k3dConfig, absTemplatePath)
	}

	// Write the in-memory config to a temp file so k3d picks up any modifications
	// (e.g., registry mirrors configured via --mirror-registry, node counts).
	// We always use a temp file to avoid modifying the user's k3d.yaml.
	// The k3d CLI reads configuration from file, not from our in-memory config.
	tempConfigPath, err := writeK3dConfigToTempFile(k3dConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to write k3d config to temp file: %w", err)
	}

	provisioner := k3dprovisioner.CreateProvisioner(
		k3dConfig,
		tempConfigPath,
	).WithKubeconfig(cluster.Spec.Cluster.Connection.Kubeconfig)

	if f.ComponentDetector != nil {
		provisioner.WithComponentDetector(f.ComponentDetector)
	}

	return provisioner, k3dConfig, nil
}

// applyK3dNodeCounts applies node count overrides from CLI flags / cluster-level
// config to the K3d config. Enables --control-planes and --workers CLI flags to
// override k3d.yaml at runtime.
func applyK3dNodeCounts(k3dConfig *k3dv1alpha5.SimpleConfig, controlPlanes, workers int32) {
	if controlPlanes <= 0 && workers <= 0 {
		return
	}

	if controlPlanes > 0 {
		k3dConfig.Servers = int(controlPlanes)
	}

	k3dConfig.Agents = int(workers)
}

// writeK3dConfigToTempFile writes the in-memory k3d config to a temporary file.
// This approach avoids modifying the user's k3d.yaml while ensuring k3d picks up
// all in-memory modifications (registry mirrors, node counts, etc.).
// The temp file persists until system cleanup - this is intentional since k3d
// may reference the config path during cluster operations.
func writeK3dConfigToTempFile(config *k3dv1alpha5.SimpleConfig) (string, error) {
	data, err := yaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("marshal k3d config: %w", err)
	}

	// Create temp file with k3d prefix for easy identification
	tempFile, err := os.CreateTemp("", "ksail-k3d-*.yaml")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	filePath := tempFile.Name()

	_, writeErr := tempFile.Write(data)

	closeErr := tempFile.Close()

	if writeErr != nil {
		return "", fmt.Errorf("write to temp file: %w", writeErr)
	}

	if closeErr != nil {
		return "", fmt.Errorf("close temp file: %w", closeErr)
	}

	return filePath, nil
}
