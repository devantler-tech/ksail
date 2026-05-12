package talosprovisioner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
	"github.com/siderolabs/talos/pkg/cluster/check"
	"github.com/siderolabs/talos/pkg/conditions"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"github.com/siderolabs/talos/pkg/machinery/config/bundle"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"k8s.io/client-go/tools/clientcmd"
)

// writeKubeconfig merges the raw kubeconfig bytes into the existing kubeconfig file
// at the configured path. It expands tilde in the path and delegates to MergeKubeconfig
// which handles directory creation, path canonicalization, and merging.
func (p *Provisioner) writeKubeconfig(kubeconfig []byte) error {
	// Expand tilde in kubeconfig path (e.g., ~/.kube/config -> /home/user/.kube/config)
	kubeconfigPath, err := fsutil.ExpandHomePath(p.options.KubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to expand kubeconfig path: %w", err)
	}

	// Merge into existing kubeconfig to preserve other cluster entries.
	// MergeKubeconfig handles directory creation and path canonicalization.
	err = k8s.MergeKubeconfig(kubeconfigPath, kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to merge kubeconfig: %w", err)
	}

	// Log the canonical path for accuracy (MergeKubeconfig resolves symlinks internally).
	canonicalPath, _ := fsutil.EvalCanonicalPath(kubeconfigPath)
	if canonicalPath == "" {
		canonicalPath = kubeconfigPath
	}

	_, _ = fmt.Fprintf(p.logWriter, "Kubeconfig saved to %s\n", canonicalPath)

	return nil
}

// saveTalosconfig merges the new talosconfig context into the existing talosconfig file.
// If the file does not exist, it creates it with the new context.
func (p *Provisioner) saveTalosconfig(configBundle *bundle.Bundle) error {
	// Expand tilde in talosconfig path
	talosconfigPath, err := fsutil.ExpandHomePath(p.options.TalosconfigPath)
	if err != nil {
		return fmt.Errorf("failed to expand talosconfig path: %w", err)
	}

	talosconfigPath, err = prepareTalosconfigPath(talosconfigPath)
	if err != nil {
		return err
	}

	newConfig := configBundle.TalosConfig()

	// Try to load existing talosconfig; if it doesn't exist, save directly
	existing, openErr := clientconfig.Open(talosconfigPath)
	if openErr != nil {
		if os.IsNotExist(openErr) {
			saveErr := newConfig.Save(talosconfigPath)
			if saveErr != nil {
				return fmt.Errorf("failed to save talosconfig: %w", saveErr)
			}

			_, _ = fmt.Fprintf(p.logWriter, "Talosconfig saved to %s\n", talosconfigPath)

			return nil
		}

		return fmt.Errorf("failed to open existing talosconfig: %w", openErr)
	}

	// Merge new contexts into existing config (renames colliding contexts with -N suffix)
	existing.Merge(newConfig)

	saveErr := existing.Save(talosconfigPath)
	if saveErr != nil {
		return fmt.Errorf("failed to save merged talosconfig: %w", saveErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Talosconfig saved to %s\n", talosconfigPath)

	return nil
}

// mergeTalosconfigBytes merges raw talosconfig bytes into an existing talosconfig file.
// If the file does not exist, it creates it with the new content.
// It creates the parent directory and canonicalizes the path before reading or writing
// to prevent symlink-escape attacks.
// This is used by Omni paths that receive talosconfig as raw bytes.
func mergeTalosconfigBytes(talosconfigPath string, newData []byte) error {
	// Parse the new talosconfig data
	newConfig, err := clientconfig.FromBytes(newData)
	if err != nil {
		return fmt.Errorf("failed to parse new talosconfig: %w", err)
	}

	talosconfigPath, err = prepareTalosconfigPath(talosconfigPath)
	if err != nil {
		return err
	}

	// Try to load existing talosconfig
	existing, openErr := clientconfig.Open(talosconfigPath)
	if openErr != nil {
		if os.IsNotExist(openErr) {
			// No existing file; save the new config directly
			saveErr := newConfig.Save(talosconfigPath)
			if saveErr != nil {
				return fmt.Errorf("failed to save new talosconfig: %w", saveErr)
			}

			return nil
		}

		return fmt.Errorf("failed to open existing talosconfig: %w", openErr)
	}

	// Merge new contexts into existing config
	existing.Merge(newConfig)

	saveErr := existing.Save(talosconfigPath)
	if saveErr != nil {
		return fmt.Errorf("failed to save merged talosconfig: %w", saveErr)
	}

	return nil
}

// prepareTalosconfigPath ensures the parent directory exists and canonicalizes the path.
// Shared by saveTalosconfig and mergeTalosconfigBytes to avoid duplication.
func prepareTalosconfigPath(talosconfigPath string) (string, error) {
	talosconfigDir := filepath.Dir(talosconfigPath)
	if talosconfigDir != "" && talosconfigDir != "." {
		mkdirErr := os.MkdirAll(talosconfigDir, stateDirectoryPermissions)
		if mkdirErr != nil {
			return "", fmt.Errorf("failed to create talosconfig directory: %w", mkdirErr)
		}
	}

	canonicalPath, err := fsutil.EvalCanonicalPath(talosconfigPath)
	if err != nil {
		return "", fmt.Errorf("failed to canonicalize talosconfig path: %w", err)
	}

	return canonicalPath, nil
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
func (p *Provisioner) cleanupKubeconfig(clusterName string) error {
	// Expand tilde in kubeconfig path
	kubeconfigPath, err := fsutil.ExpandHomePath(p.options.KubeconfigPath)
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
func (p *Provisioner) cleanupTalosconfig(clusterName string) error {
	// Expand tilde in talosconfig path
	talosconfigPath, err := fsutil.ExpandHomePath(p.options.TalosconfigPath)
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

// isDockerProvider returns true if the provisioner is using the Docker provider.
// This is the case when infraProvider is a *dockerprovider.Provider, or when
// infraProvider is nil (legacy fallback to dockerClient).
func (p *Provisioner) isDockerProvider() bool {
	if p.infraProvider == nil {
		return true
	}

	_, ok := p.infraProvider.(*dockerprovider.Provider)

	return ok
}

// etcdChecks returns the etcd-related pre-boot readiness checks shared across
// all provider-specific pre-boot sequences.
func etcdChecks() []check.ClusterCheck {
	cpTypes := check.WithNodeTypes(machine.TypeInit, machine.TypeControlPlane)

	return []check.ClusterCheck{
		func(cluster check.ClusterInfo) conditions.Condition {
			return conditions.PollingCondition(
				"etcd to be healthy",
				func(ctx context.Context) error {
					return check.ServiceHealthAssertion(ctx, cluster, "etcd", cpTypes)
				},
				preBootPollInterval,
			)
		},
		func(cluster check.ClusterInfo) conditions.Condition {
			return conditions.PollingCondition(
				"etcd members to be consistent across nodes",
				func(ctx context.Context) error {
					return check.EtcdConsistentAssertion(ctx, cluster)
				},
				preBootPollInterval,
			)
		},
		func(cluster check.ClusterInfo) conditions.Condition {
			return conditions.PollingCondition(
				"etcd members to be control plane nodes",
				func(ctx context.Context) error {
					return check.EtcdControlPlaneNodesAssertion(ctx, cluster)
				},
				preBootPollInterval,
			)
		},
	}
}

// kubeletAndBootChecks returns the kubelet health and boot sequence completion
// checks shared across all provider-specific pre-boot sequences.
func kubeletAndBootChecks() []check.ClusterCheck {
	allNodeTypes := check.WithNodeTypes(
		machine.TypeInit,
		machine.TypeControlPlane,
		machine.TypeWorker,
	)

	return []check.ClusterCheck{
		func(cluster check.ClusterInfo) conditions.Condition {
			return conditions.PollingCondition(
				"kubelet to be healthy",
				func(ctx context.Context) error {
					return check.ServiceHealthAssertion(ctx, cluster, "kubelet", allNodeTypes)
				},
				preBootPollInterval,
			)
		},
		func(cluster check.ClusterInfo) conditions.Condition {
			return conditions.PollingCondition(
				"all nodes to finish boot sequence",
				func(ctx context.Context) error {
					return check.AllNodesBootedAssertion(ctx, cluster)
				},
				preBootPollInterval,
			)
		},
	}
}

// dockerPreBootSequenceChecks returns a trimmed subset of the upstream
// check.PreBootSequenceChecks() (github.com/siderolabs/talos v1.13.0-beta.1,
// pkg/cluster/check/check.go) optimized for Docker environments. It omits
// purely diagnostic checks (AllNodesMemorySizes, AllNodesDiskSizes, NoDiagnostics)
// that add polling overhead without catching real issues in Docker containers.
//
// When upgrading the Talos dependency, verify this list against the upstream
// check.PreBootSequenceChecks() to pick up any new essential readiness gates.
func dockerPreBootSequenceChecks() []check.ClusterCheck {
	// AllNodesMemorySizes — skipped: diagnostic-only, Docker containers have consistent resources
	// AllNodesDiskSizes — skipped: diagnostic-only, Docker containers have consistent resources
	// NoDiagnostics — skipped: informational-only, not a readiness gate
	return slices.Concat(
		etcdChecks(),
		[]check.ClusterCheck{
			func(cluster check.ClusterInfo) conditions.Condition {
				return conditions.PollingCondition(
					"apid to be ready",
					func(ctx context.Context) error {
						return check.ApidReadyAssertion(ctx, cluster)
					},
					preBootPollInterval,
				)
			},
		},
		kubeletAndBootChecks(),
	)
}

// preBootSequenceChecksSkipDiagnostics returns the upstream PreBootSequenceChecks
// (github.com/siderolabs/talos, pkg/cluster/check/default.go) with the
// NoDiagnostics check omitted. Used for non-Docker providers (Hetzner, Omni) when
// kubelet cert rotation is enabled and CNI is not yet installed (CNI disabled in
// config or skipped via SkipCNIChecks because it is installed post-creation).
//
// Without CNI, the kubelet-serving-cert-approver pod cannot schedule (nodes have
// not-ready:NoSchedule taint), so kubelet serving CSRs remain pending. Talos
// reports this as a diagnostic ("CSR is not approved"), causing NoDiagnostics to
// never pass — which would deadlock check.Wait since CNI installation only
// happens after the provisioner returns.
//
// Unlike dockerPreBootSequenceChecks, this retains AllNodesMemorySizes and
// AllNodesDiskSizes checks because cloud servers have meaningful resource
// constraints worth validating.
//
// When upgrading the Talos dependency, verify this list against the upstream
// check.PreBootSequenceChecks() to pick up any new essential readiness gates.
func preBootSequenceChecksSkipDiagnostics() []check.ClusterCheck {
	// NoDiagnostics — skipped: creates deadlock when kubelet cert rotation is enabled
	// without CNI (cert-approver can't schedule, "CSR is not approved" diagnostic never clears)
	return slices.Concat(
		etcdChecks(),
		[]check.ClusterCheck{
			func(cluster check.ClusterInfo) conditions.Condition {
				return conditions.PollingCondition(
					"apid to be ready",
					func(ctx context.Context) error {
						return check.ApidReadyAssertion(ctx, cluster)
					},
					preBootPollInterval,
				)
			},
			func(cluster check.ClusterInfo) conditions.Condition {
				return conditions.PollingCondition(
					"all nodes memory sizes",
					func(ctx context.Context) error {
						return check.AllNodesMemorySizes(ctx, cluster)
					},
					preBootPollInterval,
				)
			},
			func(cluster check.ClusterInfo) conditions.Condition {
				return conditions.PollingCondition(
					"all nodes disk sizes",
					func(ctx context.Context) error {
						return check.AllNodesDiskSizes(ctx, cluster)
					},
					preBootPollInterval,
				)
			},
		},
		kubeletAndBootChecks(),
	)
}

// the K8sControlPlaneStaticPods check is skipped because it depends on Talos
// StaticPodStatus resources which require a kubelet serving certificate. Without CNI,
// the kubelet-serving-cert-approver pod cannot schedule, leaving CSRs unapproved and
// the kubelet without a serving certificate. The K8sFullControlPlaneAssertion check
// validates the same control plane readiness via the K8s API instead.
//
// See: https://pkg.go.dev/github.com/siderolabs/talos/pkg/cluster/check
func (p *Provisioner) clusterReadinessChecks() []check.ClusterCheck {
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
	skipStaticPodStatusCheck := p.talosConfigs != nil &&
		p.talosConfigs.IsKubeletCertRotationEnabled()

	var preBootChecks []check.ClusterCheck

	switch {
	case p.isDockerProvider():
		preBootChecks = dockerPreBootSequenceChecks()
	case skipStaticPodStatusCheck:
		// Non-Docker providers with kubelet cert rotation: skip NoDiagnostics to
		// avoid deadlock (cert-approver can't schedule without CNI, so the "CSR
		// is not approved" diagnostic never clears).
		preBootChecks = preBootSequenceChecksSkipDiagnostics()
	default:
		preBootChecks = check.PreBootSequenceChecks()
	}

	if skipStaticPodStatusCheck {
		return slices.Concat(
			preBootChecks,
			p.k8sComponentsReadinessChecksWithoutStaticPodStatus(),
		)
	}

	return slices.Concat(
		preBootChecks,
		check.K8sComponentsReadinessChecks(),
	)
}

// k8sComponentsReadinessChecksWithoutStaticPodStatus returns K8s component readiness checks
// that skip the Talos-based K8sControlPlaneStaticPods check. This is used when kubelet
// serving certificate rotation is enabled but the CSR approver cannot run (e.g., no CNI),
// making StaticPodStatus resources unavailable. The K8sFullControlPlaneAssertion check
// validates control plane readiness via the K8s API instead.
func (p *Provisioner) k8sComponentsReadinessChecksWithoutStaticPodStatus() []check.ClusterCheck {
	return []check.ClusterCheck{
		// wait for all the nodes to report in at k8s level
		func(cluster check.ClusterInfo) conditions.Condition {
			return conditions.PollingCondition(
				"all k8s nodes to report",
				func(ctx context.Context) error {
					return check.K8sAllNodesReportedAssertion(ctx, cluster)
				},
				preBootPollInterval,
			)
		},

		// skip K8sControlPlaneStaticPods — Talos can't connect to kubelet without serving cert

		// wait for HA k8s control plane
		func(cluster check.ClusterInfo) conditions.Condition {
			return conditions.PollingCondition(
				"all control plane components to be ready",
				func(ctx context.Context) error {
					return check.K8sFullControlPlaneAssertion(ctx, cluster)
				},
				preBootPollInterval,
			)
		},
	}
}
