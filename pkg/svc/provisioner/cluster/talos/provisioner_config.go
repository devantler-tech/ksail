package talosprovisioner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	iopath "github.com/devantler-tech/ksail/v5/pkg/io"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/siderolabs/talos/pkg/cluster/check"
	"github.com/siderolabs/talos/pkg/conditions"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"github.com/siderolabs/talos/pkg/machinery/config/bundle"
	"k8s.io/client-go/tools/clientcmd"
)

// writeKubeconfig writes the raw kubeconfig bytes to the configured kubeconfig path.
// It expands tilde in the path, ensures the directory exists, and writes the file.
func (p *TalosProvisioner) writeKubeconfig(kubeconfig []byte) error {
	// Expand tilde in kubeconfig path (e.g., ~/.kube/config -> /home/user/.kube/config)
	kubeconfigPath, err := iopath.ExpandHomePath(p.options.KubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to expand kubeconfig path: %w", err)
	}

	// Ensure kubeconfig directory exists
	kubeconfigDir := filepath.Dir(kubeconfigPath)
	if kubeconfigDir != "" && kubeconfigDir != "." {
		mkdirErr := os.MkdirAll(kubeconfigDir, stateDirectoryPermissions)
		if mkdirErr != nil {
			return fmt.Errorf("failed to create kubeconfig directory: %w", mkdirErr)
		}
	}

	// Write kubeconfig to file
	err = os.WriteFile(kubeconfigPath, kubeconfig, kubeconfigFileMode)
	if err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Kubeconfig saved to %s\n", kubeconfigPath)

	return nil
}

// saveTalosconfig saves the talosconfig for any cluster type.
func (p *TalosProvisioner) saveTalosconfig(configBundle *bundle.Bundle) error {
	// Expand tilde in talosconfig path
	talosconfigPath, err := iopath.ExpandHomePath(p.options.TalosconfigPath)
	if err != nil {
		return fmt.Errorf("failed to expand talosconfig path: %w", err)
	}

	// Ensure talosconfig directory exists
	talosconfigDir := filepath.Dir(talosconfigPath)
	if talosconfigDir != "" && talosconfigDir != "." {
		mkdirErr := os.MkdirAll(talosconfigDir, stateDirectoryPermissions)
		if mkdirErr != nil {
			return fmt.Errorf("failed to create talosconfig directory: %w", mkdirErr)
		}
	}

	// Save the talosconfig
	saveErr := configBundle.TalosConfig().Save(talosconfigPath)
	if saveErr != nil {
		return fmt.Errorf("failed to save talosconfig: %w", saveErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Talosconfig saved to %s\n", talosconfigPath)

	return nil
}

// rewriteKubeconfigEndpoint rewrites all cluster server endpoints in the kubeconfig
// to use the specified endpoint. This is used for Docker-in-VM environments where
// the internal container IPs are not accessible from the host.
func rewriteKubeconfigEndpoint(kubeconfigBytes []byte, endpoint string) ([]byte, error) {
	if endpoint == "" {
		return kubeconfigBytes, nil
	}

	kubeConfig, err := clientcmd.Load(kubeconfigBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	// Rewrite server endpoint for all clusters
	for name := range kubeConfig.Clusters {
		kubeConfig.Clusters[name].Server = endpoint
	}

	// Serialize back to YAML
	result, err := clientcmd.Write(*kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize kubeconfig: %w", err)
	}

	return result, nil
}

// cleanupKubeconfig removes the cluster, context, and user entries for the deleted cluster
// from the kubeconfig file. This only removes entries matching the cluster name,
// leaving other cluster configurations intact.
func (p *TalosProvisioner) cleanupKubeconfig(clusterName string) error {
	// Expand tilde in kubeconfig path
	kubeconfigPath, err := iopath.ExpandHomePath(p.options.KubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to expand kubeconfig path: %w", err)
	}

	// Talos uses "admin@<cluster-name>" format for context and user names
	contextName := "admin@" + clusterName
	userName := contextName

	err = k8s.CleanupKubeconfig(
		kubeconfigPath,
		clusterName,
		contextName,
		userName,
		p.logWriter,
	)
	if err != nil {
		return fmt.Errorf("failed to cleanup kubeconfig: %w", err)
	}

	return nil
}

// cleanupTalosconfig removes the context entry for the deleted cluster from the talosconfig file.
// This cleans up stale configuration that would point to IPs that no longer exist.
// If the current context is the deleted cluster, it sets the context to the first
// remaining context, or leaves it empty if no contexts remain.
func (p *TalosProvisioner) cleanupTalosconfig(clusterName string) error {
	// Expand tilde in talosconfig path
	talosconfigPath, err := iopath.ExpandHomePath(p.options.TalosconfigPath)
	if err != nil {
		return fmt.Errorf("failed to expand talosconfig path: %w", err)
	}

	// Open the talosconfig file
	cfg, err := clientconfig.Open(talosconfigPath)
	if err != nil {
		// If the file doesn't exist, nothing to clean up
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("failed to open talosconfig: %w", err)
	}

	// Check if the context exists
	if _, exists := cfg.Contexts[clusterName]; !exists {
		// Context doesn't exist, nothing to clean up
		return nil
	}

	// Remove the context
	delete(cfg.Contexts, clusterName)

	// If the current context was the deleted cluster, update it
	if cfg.Context == clusterName {
		// Set to first remaining context, or empty if none
		cfg.Context = ""

		for name := range cfg.Contexts {
			cfg.Context = name

			break
		}
	}

	// Save the modified config
	saveErr := cfg.Save(talosconfigPath)
	if saveErr != nil {
		return fmt.Errorf("failed to save talosconfig: %w", saveErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Cleaned up talosconfig entries for cluster %q\n", clusterName)

	return nil
}

// clusterReadinessChecks returns the appropriate set of cluster readiness checks
// based on CNI and kubelet certificate configuration.
//
// When CNI is disabled (either by Talos config or SkipCNIChecks option), returns
// lighter checks that skip node Ready status, since nodes will remain NotReady
// until the CNI is installed post-creation.
//
// Additionally, when kubelet serving certificate rotation is enabled with CNI disabled,
// the K8sControlPlaneStaticPods check is skipped because it depends on Talos
// StaticPodStatus resources which require a kubelet serving certificate. Without CNI,
// the kubelet-serving-cert-approver pod cannot schedule, leaving CSRs unapproved and
// the kubelet without a serving certificate. The K8sFullControlPlaneAssertion check
// validates the same control plane readiness via the K8s API instead.
//
// See: https://pkg.go.dev/github.com/siderolabs/talos/pkg/cluster/check
func (p *TalosProvisioner) clusterReadinessChecks() []check.ClusterCheck {
	skipNodeReadiness := (p.talosConfigs != nil && p.talosConfigs.IsCNIDisabled()) ||
		p.options.SkipCNIChecks

	if !skipNodeReadiness {
		return check.DefaultClusterChecks()
	}

	// When kubelet cert rotation is enabled with CNI disabled, the kubelet-serving-cert-approver
	// pod cannot schedule (node has not-ready taint), so kubelet serving CSRs remain pending.
	// Without a serving certificate, Talos cannot connect to kubelet, and StaticPodStatus
	// resources are never populated. Skip the Talos-based static pod check and rely on
	// K8sFullControlPlaneAssertion which validates the same thing via the K8s API.
	skipStaticPodStatusCheck := p.talosConfigs != nil && p.talosConfigs.IsKubeletCertRotationEnabled()

	if skipStaticPodStatusCheck {
		return slices.Concat(
			check.PreBootSequenceChecks(),
			p.k8sComponentsReadinessChecksWithoutStaticPodStatus(),
		)
	}

	return slices.Concat(
		check.PreBootSequenceChecks(),
		check.K8sComponentsReadinessChecks(),
	)
}

// k8sComponentsReadinessChecksWithoutStaticPodStatus returns K8s component readiness checks
// that skip the Talos-based K8sControlPlaneStaticPods check. This is used when kubelet
// serving certificate rotation is enabled but the CSR approver cannot run (e.g., no CNI),
// making StaticPodStatus resources unavailable. The K8sFullControlPlaneAssertion check
// validates control plane readiness via the K8s API instead.
func (p *TalosProvisioner) k8sComponentsReadinessChecksWithoutStaticPodStatus() []check.ClusterCheck {
	return []check.ClusterCheck{
		// wait for all the nodes to report in at k8s level
		func(cluster check.ClusterInfo) conditions.Condition {
			return conditions.PollingCondition("all k8s nodes to report", func(ctx context.Context) error {
				return check.K8sAllNodesReportedAssertion(ctx, cluster)
			}, 5*time.Minute, 30*time.Second)
		},

		// skip K8sControlPlaneStaticPods — Talos can't connect to kubelet without serving cert

		// wait for HA k8s control plane
		func(cluster check.ClusterInfo) conditions.Condition {
			return conditions.PollingCondition("all control plane components to be ready", func(ctx context.Context) error {
				return check.K8sFullControlPlaneAssertion(ctx, cluster)
			}, 5*time.Minute, 5*time.Second)
		},
	}
}
