package kindprovisioner

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// resolveKubeconfigPath resolves the effective kubeconfig write target for
// kind. An explicit configured path wins (expanded). Otherwise it mirrors
// kind's own KUBECONFIG handling for the write side (kind v0.32.0: with a
// path list, write to the first EXISTING file, or the last entry when none
// exist) — the resolved path is later passed as --kubeconfig, which disables
// kind's own selection, so this must not diverge from what kind would have
// chosen. With no KUBECONFIG either, it falls back to ~/.kube/config.
func resolveKubeconfigPath(kubeconfigPath string) (string, error) {
	if kubeconfigPath != "" {
		resolved, err := k8s.ResolveKubeconfigPath(kubeconfigPath)
		if err != nil {
			return "", fmt.Errorf("resolve kubeconfig path: %w", err)
		}

		return resolved, nil
	}

	if target := kindKubeconfigEnvWriteTarget(); target != "" {
		return target, nil
	}

	return k8s.DefaultKubeconfigPath(), nil
}

// kindKubeconfigEnvWriteTarget applies kind's kubeconfig write-target rule to
// the KUBECONFIG environment variable. Empty when KUBECONFIG is unset or
// holds no usable entry.
func kindKubeconfigEnvWriteTarget() string {
	return kindKubeconfigWriteTarget(os.Getenv("KUBECONFIG"))
}

// kindKubeconfigWriteTarget picks kind's kubeconfig write target from a
// KUBECONFIG-style path list: entries are kept literal (kind does not expand
// them), empty and duplicate entries are dropped, the first entry naming an
// existing regular file wins, and when none exist the last entry does.
func kindKubeconfigWriteTarget(envValue string) string {
	if envValue == "" {
		return ""
	}

	entries := make([]string, 0)
	seen := make(map[string]struct{})

	for _, entry := range filepath.SplitList(envValue) {
		if entry == "" {
			continue
		}

		if _, duplicate := seen[entry]; duplicate {
			continue
		}

		seen[entry] = struct{}{}
		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		return ""
	}

	for _, entry := range entries {
		// Stat-only existence probe of a path the user controls by design
		// (KUBECONFIG carries the same trust kubectl gives it).
		info, err := os.Stat(entry) //nolint:gosec // KUBECONFIG is a user-controlled path by design.
		if err == nil && !info.IsDir() {
			return entry
		}
	}

	return entries[len(entries)-1]
}

// CreateProvisioner creates a Provisioner from a pre-loaded configuration.
// The Kind config should be loaded via the configmanager before calling this function,
// allowing any in-memory modifications (e.g., mirror registries) to be preserved.
//
// Parameters:
//   - kindConfig: Pre-loaded Kind cluster configuration
//   - kubeconfigPath: Path where the kubeconfig should be written (defaults to
//     the first existing KUBECONFIG entry, or its last entry when none exist;
//     with no usable KUBECONFIG entry, defaults to ~/.kube/config)
func CreateProvisioner(
	kindConfig *v1alpha4.Cluster,
	kubeconfigPath string,
) (*Provisioner, error) {
	kubeconfigPath, err := resolveKubeconfigPath(kubeconfigPath)
	if err != nil {
		return nil, err
	}

	kindSDKProvider := NewDefaultProviderAdapter()

	dockerClient, err := NewDefaultDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	// Create Docker infrastructure provider for Kind clusters
	infraProvider := dockerprovider.NewProvider(dockerClient, dockerprovider.LabelSchemeKind)

	provisioner := NewProvisioner(
		kindConfig,
		kubeconfigPath,
		kindSDKProvider,
		infraProvider,
	)

	return provisioner, nil
}

// CreateProvisionerWithProvider creates a Provisioner with a custom infrastructure provider.
// This is useful for testing or when a specific provider implementation is needed.
func CreateProvisionerWithProvider(
	kindConfig *v1alpha4.Cluster,
	kubeconfigPath string,
	infraProvider provider.Provider,
) (*Provisioner, error) {
	kubeconfigPath, err := resolveKubeconfigPath(kubeconfigPath)
	if err != nil {
		return nil, err
	}

	kindSDKProvider := NewDefaultProviderAdapter()

	provisioner := NewProvisioner(
		kindConfig,
		kubeconfigPath,
		kindSDKProvider,
		infraProvider,
	)

	return provisioner, nil
}
