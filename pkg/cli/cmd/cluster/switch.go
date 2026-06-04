package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/picker"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// NewSwitchCmd creates the switch command for clusters.
func NewSwitchCmd(_ *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "switch [cluster-name]",
		Short: "Switch active cluster context",
		Long:  switchLongDesc,
		Args:  cobra.MaximumNArgs(1),
		ValidArgsFunction: func(
			cmd *cobra.Command,
			_ []string,
			_ string,
		) ([]string, cobra.ShellCompDirective) {
			return listClusterNames(cmd), cobra.ShellCompDirectiveNoFileComp
		},
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			deps := SwitchDeps{}

			if len(args) > 0 {
				return HandleSwitchRunE(cmd, args[0], deps)
			}

			clusterName, err := pickCluster(cmd, deps)
			if err != nil {
				return err
			}

			return HandleSwitchRunE(cmd, clusterName, deps)
		},
	}

	return cmd
}

// SwitchDeps captures injectable dependencies for the switch command.
type SwitchDeps struct {
	// KubeconfigPath overrides the kubeconfig path resolution.
	// If empty, the path is resolved from KUBECONFIG env, ksail.yaml, or the default.
	KubeconfigPath string

	// PickCluster overrides the interactive picker for testing.
	// If nil, the default bubbletea picker is used.
	PickCluster func(title string, items []string) (string, error)

	// LoadSwitchHistory overrides history loading for testing.
	// If nil, the default loadSwitchHistory is used.
	LoadSwitchHistory func() []string

	// SaveToSwitchHistory overrides history saving for testing.
	// If nil, the default saveToSwitchHistory is used.
	SaveToSwitchHistory func(name string)
}

// resolveSwitchKubeconfig returns the kubeconfig path for switch operations.
// It uses the injected path from deps when provided, otherwise delegates to
// resolveKubeconfigForSwitch (which checks KUBECONFIG env, ksail.yaml, and the default).
func resolveSwitchKubeconfig(cmd *cobra.Command, deps SwitchDeps) (string, error) {
	if deps.KubeconfigPath != "" {
		return deps.KubeconfigPath, nil
	}

	path, err := resolveKubeconfigForSwitch(cmd)
	if err != nil {
		return "", fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	return path, nil
}

// HandleSwitchRunE handles the switch command.
// Exported for testing purposes.
func HandleSwitchRunE(
	cmd *cobra.Command,
	clusterName string,
	deps SwitchDeps,
) error {
	kubeconfigPath, err := resolveSwitchKubeconfig(cmd, deps)
	if err != nil {
		return err
	}

	contextName, err := switchContext(kubeconfigPath, clusterName)
	if err != nil {
		return err
	}

	notify.Successf(
		cmd.OutOrStdout(),
		"Switched to cluster '%s' (context: %s)",
		clusterName,
		contextName,
	)

	// Persist the cluster name to switch history (errors silently ignored).
	save := deps.SaveToSwitchHistory
	if save == nil {
		save = saveToSwitchHistory
	}

	save(stripParenthetical(clusterName))

	return nil
}

// buildOrderedClusterNames merges recent switch history with all known cluster
// names so that recently-switched clusters appear first in the list.
// Names in recent that are no longer present in allNames are silently skipped.
func buildOrderedClusterNames(recent, allNames []string) []string {
	recentSet := make(map[string]struct{}, len(recent))
	names := make([]string, 0, len(allNames))

	for _, name := range recent {
		if len(names) >= switchHistoryMaxItems {
			break
		}

		if _, already := recentSet[name]; !already && slices.Contains(allNames, name) {
			names = append(names, name)
			recentSet[name] = struct{}{}
		}
	}

	for _, name := range allNames {
		if _, ok := recentSet[name]; !ok {
			names = append(names, name)
		}
	}

	return names
}

// pickCluster resolves the kubeconfig, lists available cluster names ordered
// by recency (recently switched clusters appear first), and presents an
// interactive picker for the user to select one.
func pickCluster(cmd *cobra.Command, deps SwitchDeps) (string, error) {
	kubeconfigPath, err := resolveSwitchKubeconfig(cmd, deps)
	if err != nil {
		return "", err
	}

	allNames := clusterNamesFromPath(kubeconfigPath)
	if len(allNames) == 0 {
		return "", fmt.Errorf("%w", ErrNoClusters)
	}

	loadHistory := deps.LoadSwitchHistory
	if loadHistory == nil {
		loadHistory = loadSwitchHistory
	}

	names := buildOrderedClusterNames(loadHistory(), allNames)

	pick := deps.PickCluster
	if pick == nil {
		pick = picker.Run
	}

	selected, err := pick("Select a cluster:", names)
	if err != nil {
		return "", fmt.Errorf("cluster selection: %w", err)
	}

	return selected, nil
}

// resolveContextName finds the matching kubeconfig context for a cluster name
// by checking all known distribution context-name prefixes.
// Parenthetical suffixes (e.g., " (Vanilla)") are stripped defensively so that
// cluster names containing distribution hints still resolve correctly.
func resolveContextName(
	config *clientcmdapi.Config,
	clusterName string,
) (string, error) {
	// Strip trailing parenthetical suffix (e.g., " (Vanilla)") that may be
	// present if the name was copied from enriched list output.
	cleanName := stripParenthetical(clusterName)

	var matches []string

	for _, dist := range v1alpha1.ValidDistributions() {
		candidate := dist.ContextName(cleanName)

		if _, exists := config.Contexts[candidate]; exists {
			matches = append(matches, candidate)
		}
	}

	// Fallback: if no distribution-prefix match was found, look for contexts
	// that contain the cluster name as a substring. This handles providers like
	// Omni whose kubeconfig context format (<org>-<cluster>-<sa>) doesn't
	// follow the standard distribution prefix conventions.
	if len(matches) == 0 {
		for ctxName := range config.Contexts {
			if strings.Contains(ctxName, cleanName) {
				matches = append(matches, ctxName)
			}
		}
	}

	switch len(matches) {
	case 0:
		available := make([]string, 0, len(config.Contexts))
		for name := range config.Contexts {
			available = append(available, name)
		}

		slices.Sort(available)

		return "", fmt.Errorf(
			"%w: %s (available contexts: %s)",
			ErrContextNotFound,
			clusterName,
			strings.Join(available, ", "),
		)
	case 1:
		return matches[0], nil
	default:
		slices.Sort(matches)

		return "", fmt.Errorf(
			"%w: '%s' matches multiple contexts: %s",
			ErrAmbiguousCluster,
			clusterName,
			strings.Join(matches, ", "),
		)
	}
}

// stripParenthetical removes a trailing " (<text>)" suffix from input.
// Returns input unchanged if no such suffix is present.
func stripParenthetical(input string) string {
	idx := strings.LastIndex(input, " (")
	if idx < 0 {
		return input
	}

	if strings.HasSuffix(input, ")") {
		return input[:idx]
	}

	return input
}

// switchContext loads the kubeconfig, resolves the cluster name to a context, and sets current-context.
//
//nolint:gosec // G304: kubeconfigPath is resolved from trusted config or default
func switchContext(kubeconfigPath, clusterName string) (string, error) {
	configBytes, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("failed to read kubeconfig: %w", err)
	}

	config, err := clientcmd.Load(configBytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	contextName, err := resolveContextName(config, clusterName)
	if err != nil {
		return "", err
	}

	config.CurrentContext = contextName

	result, err := clientcmd.Write(*config)
	if err != nil {
		return "", fmt.Errorf("failed to serialize kubeconfig: %w", err)
	}

	err = os.WriteFile(kubeconfigPath, result, switchKubeconfigFileMode)
	if err != nil {
		return "", fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	return contextName, nil
}

// listClusterNames returns deduplicated cluster names from the kubeconfig for shell completion.
// It strips known distribution prefixes from context names to produce cluster names.
// When cmd is non-nil, the --config persistent flag is honored for config loading.
func listClusterNames(cmd *cobra.Command) []string {
	kubeconfigPath, err := resolveKubeconfigForSwitch(cmd)
	if err != nil {
		return nil
	}

	return clusterNamesFromPath(kubeconfigPath)
}

// clusterNamesFromPath reads the given kubeconfig and returns sorted, deduplicated
// cluster names by stripping distribution prefixes from context names.
//
//nolint:gosec // G304: kubeconfigPath is resolved from trusted config or default
func clusterNamesFromPath(kubeconfigPath string) []string {
	configBytes, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return nil
	}

	config, err := clientcmd.Load(configBytes)
	if err != nil {
		return nil
	}

	seen := make(map[string]struct{})

	for contextName := range config.Contexts {
		if name := stripDistributionPrefix(contextName); name != "" {
			seen[name] = struct{}{}
		}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

// stripDistributionPrefix removes the distribution-specific prefix from a context name,
// returning the underlying cluster name. Returns empty string if the context name
// does not match any known distribution prefix.
func stripDistributionPrefix(contextName string) string {
	const sentinel = "\x00"

	for _, dist := range v1alpha1.ValidDistributions() {
		prefix := strings.TrimSuffix(dist.ContextName(sentinel), sentinel)

		if after, found := strings.CutPrefix(contextName, prefix); found {
			return after
		}
	}

	return ""
}

// resolveKubeconfigForSwitch resolves the kubeconfig path using the same priority
// order as other cluster commands: KUBECONFIG env > ksail.yaml > default (~/.kube/config).
// When KUBECONFIG contains multiple paths separated by the OS path list separator,
// only the first path is used.
// When cmd is non-nil, the --config persistent flag is honored for config loading.
func resolveKubeconfigForSwitch(cmd *cobra.Command) (string, error) {
	// 1. Check KUBECONFIG environment variable
	if os.Getenv("KUBECONFIG") != "" {
		// ResolveKubeconfigPath("") checks KUBECONFIG env, splits on path separator,
		// expands ~, and returns the first path.
		resolved, err := clusterdetector.ResolveKubeconfigPath("")
		if err != nil {
			return "", fmt.Errorf("resolve kubeconfig from KUBECONFIG env: %w", err)
		}

		return resolved, nil
	}

	// 2. Try ksail.yaml config file, falls back to default (~/.kube/config)
	path := kubeconfig.GetKubeconfigPathSilently(cmd)

	resolved, err := clusterdetector.ResolveKubeconfigPath(path)
	if err != nil {
		return "", fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	return resolved, nil
}

// overrideInstallerFactory is a helper that applies a factory override and returns a restore function.
func overrideInstallerFactory(apply func(*setup.InstallerFactories)) func() {
	installerFactoriesOverrideMu.Lock()

	previous := installerFactoriesOverride
	override := setup.DefaultInstallerFactories()

	if previous != nil {
		*override = *previous
	}

	apply(override)
	installerFactoriesOverride = override

	installerFactoriesOverrideMu.Unlock()

	return func() {
		installerFactoriesOverrideMu.Lock()

		installerFactoriesOverride = previous

		installerFactoriesOverrideMu.Unlock()
	}
}

// SetCertManagerInstallerFactoryForTests overrides the cert-manager installer factory.
func SetCertManagerInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.CertManager = factory
	})
}

// SetCSIInstallerFactoryForTests overrides the CSI installer factory.
func SetCSIInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.CSI = factory
	})
}

// SetArgoCDInstallerFactoryForTests overrides the Argo CD installer factory.
func SetArgoCDInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.ArgoCD = factory
	})
}

// SetPolicyEngineInstallerFactoryForTests overrides the policy engine installer factory.
func SetPolicyEngineInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.PolicyEngine = factory
	})
}

// SetClusterAutoscalerInstallerFactoryForTests overrides the cluster-autoscaler installer factory.
func SetClusterAutoscalerInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.ClusterAutoscaler = factory
	})
}

// SetEnsureArgoCDResourcesForTests overrides the Argo CD resource ensure function.
func SetEnsureArgoCDResourcesForTests(
	fn func(context.Context, string, *v1alpha1.Cluster, string) error,
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.EnsureArgoCDResources = fn
	})
}

// SetFluxInstallerFactoryForTests overrides the Flux installer factory.
func SetFluxInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		// Wrap the simplified test factory to match the Flux factory signature
		f.Flux = func(_ helm.Interface, _ time.Duration, _ string) installer.Installer {
			inst, _ := factory(nil) // clusterCfg not used in test factory

			return inst
		}
	})
}

// SetDockerClientInvokerForTests overrides the Docker client invoker for testing.
func SetDockerClientInvokerForTests(
	invoker func(*cobra.Command, func(client.APIClient) error) error,
) func() {
	dockerClientInvokerMu.Lock()

	previous := dockerClientInvoker
	dockerClientInvoker = invoker

	dockerClientInvokerMu.Unlock()

	return func() {
		dockerClientInvokerMu.Lock()

		dockerClientInvoker = previous

		dockerClientInvokerMu.Unlock()
	}
}

// SetProvisionerFactoryForTests overrides the cluster provisioner factory for testing.
func SetProvisionerFactoryForTests(factory clusterprovisioner.Factory) func() {
	clusterProvisionerFactoryMu.Lock()

	previous := clusterProvisionerFactoryOverride
	clusterProvisionerFactoryOverride = factory

	clusterProvisionerFactoryMu.Unlock()

	return func() {
		clusterProvisionerFactoryMu.Lock()

		clusterProvisionerFactoryOverride = previous

		clusterProvisionerFactoryMu.Unlock()
	}
}

// SetLocalRegistryServiceFactoryForTests overrides the local registry service factory for testing.
func SetLocalRegistryServiceFactoryForTests(factory localregistry.ServiceFactoryFunc) func() {
	localRegistryServiceFactoryMu.Lock()

	previous := localRegistryServiceFactory
	localRegistryServiceFactory = factory

	localRegistryServiceFactoryMu.Unlock()

	return func() {
		localRegistryServiceFactoryMu.Lock()

		localRegistryServiceFactory = previous

		localRegistryServiceFactoryMu.Unlock()
	}
}

// SetSetupFluxInstanceForTests overrides the FluxInstance setup function.
func SetSetupFluxInstanceForTests(
	fn func(context.Context, string, *v1alpha1.Cluster, string, string) error,
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.SetupFluxInstance = fn
	})
}

// SetWaitForFluxReadyForTests overrides the Flux readiness wait function.
func SetWaitForFluxReadyForTests(fn func(context.Context, string) error) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.WaitForFluxReady = fn
	})
}

// SetEnsureOCIArtifactForTests overrides the OCI artifact ensure function.
func SetEnsureOCIArtifactForTests(
	fn func(context.Context, *cobra.Command, *v1alpha1.Cluster, string, io.Writer) (bool, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.EnsureOCIArtifact = fn
	})
}

// deleteTimeout is the maximum duration for the auto-delete operation.
const deleteTimeout = 10 * time.Minute

// waitForTTLAndDelete blocks until the TTL duration elapses and then auto-deletes the cluster.
// The wait can be cancelled with SIGINT/SIGTERM, in which case the cluster is left running.
// This implements the ephemeral cluster pattern: after creation, the process stays alive
// and automatically tears down the cluster when the TTL expires.
func waitForTTLAndDelete(
	cmd *cobra.Command,
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
	ttl time.Duration,
) error {
	notify.Infof(cmd.OutOrStdout(),
		"cluster will auto-destroy in %s (press Ctrl+C to cancel)", ttl)

	// Create a context that is cancelled on SIGINT/SIGTERM and also respects cmd.Context().
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	timer := time.NewTimer(ttl)
	defer timer.Stop()

	select {
	case <-timer.C:
		return autoDeleteCluster(cmd, clusterName, clusterCfg)
	case <-ctx.Done():
		notify.Infof(cmd.OutOrStdout(),
			"TTL wait cancelled; cluster %q will remain running", clusterName)

		return nil
	}
}

// autoDeleteCluster performs an automatic cluster deletion after TTL expiry.
// It creates a minimal provisioner based on distribution and provider info
// from the original cluster config and deletes the cluster.
func autoDeleteCluster(
	cmd *cobra.Command,
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
) error {
	notify.Infof(cmd.OutOrStdout(),
		"TTL expired; auto-destroying cluster %q...", clusterName)

	info := &clusterdetector.Info{
		ClusterName:  clusterName,
		Distribution: clusterCfg.Spec.Cluster.Distribution,
		Provider:     clusterCfg.Spec.Cluster.Provider,
	}

	provisioner, err := createDeleteProvisioner(
		info, clusterCfg.Spec.Provider.Omni, clusterCfg.Spec.Provider.Kubernetes, false,
	)
	if err != nil {
		return fmt.Errorf("TTL auto-delete: failed to create provisioner: %w", err)
	}

	deleteCtx, cancel := context.WithTimeout(cmd.Context(), deleteTimeout)
	defer cancel()

	err = provisioner.Delete(deleteCtx, clusterName)
	if err != nil {
		return fmt.Errorf("TTL auto-delete failed: %w", err)
	}

	// Clean up persisted state (spec + TTL).
	// Best-effort: warn on failure rather than blocking success.
	stateErr := state.DeleteClusterState(clusterName)
	if stateErr != nil {
		notify.Warningf(cmd.OutOrStdout(),
			"failed to clean up cluster state: %v", stateErr)
	}

	notify.Successf(cmd.OutOrStdout(),
		"cluster %q auto-destroyed after TTL expiry", clusterName)

	return nil
}

// ErrUnsupportedOutputFormat is returned when the --output flag is set to an unsupported value.
var ErrUnsupportedOutputFormat = errors.New("unsupported --output format")

// outputFormatText is the default human-readable output format.
const outputFormatText = "text"

// outputFormatJSON is the machine-readable JSON output format.
const outputFormatJSON = "json"

// ChangeJSON is the JSON representation of a single configuration change.
// It is used by DiffJSONOutput for --output json mode.
type ChangeJSON struct {
	Field    string `json:"field"`
	OldValue string `json:"oldValue"`
	NewValue string `json:"newValue"`
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

// DiffJSONOutput is the JSON representation of the diff result, emitted when
// --output json is set. It is suitable for CI/MCP consumption.
type DiffJSONOutput struct {
	TotalChanges         int          `json:"totalChanges"`
	InPlaceChanges       []ChangeJSON `json:"inPlaceChanges"`
	RebootRequired       []ChangeJSON `json:"rebootRequired"`
	RecreateRequired     []ChangeJSON `json:"recreateRequired"`
	RollingRecreate      []ChangeJSON `json:"rollingRecreate"`
	WipeRequired         []ChangeJSON `json:"wipeRequired"`
	UnknownBaseline      []ChangeJSON `json:"unknownBaseline"`
	RequiresConfirmation bool         `json:"requiresConfirmation"`
}

// getOutputFormat returns the --output flag value from the command, defaulting to "text".
// The value is normalised to lower-case so that "--output JSON" is accepted.
// Safe to call even when the flag is not registered on cmd.
func getOutputFormat(cmd *cobra.Command) string {
	if cmd == nil {
		return outputFormatText
	}

	flag := cmd.Flags().Lookup("output")
	if flag == nil {
		return outputFormatText
	}

	return strings.ToLower(flag.Value.String())
}

// validateOutputFormat returns an error when the --output flag value is
// neither "text" nor "json".
func validateOutputFormat(cmd *cobra.Command) error {
	format := getOutputFormat(cmd)
	if format != outputFormatText && format != outputFormatJSON {
		return fmt.Errorf(
			"%w: %q (expected %q or %q)",
			ErrUnsupportedOutputFormat,
			format,
			outputFormatText,
			outputFormatJSON,
		)
	}

	return nil
}

// diffToJSON converts an UpdateResult to a DiffJSONOutput struct.
func diffToJSON(diff *clusterupdate.UpdateResult) DiffJSONOutput {
	convertChanges := func(changes []clusterupdate.Change) []ChangeJSON {
		result := make([]ChangeJSON, len(changes))

		for i, change := range changes {
			result[i] = ChangeJSON{
				Field:    change.Field,
				OldValue: change.OldValue,
				NewValue: change.NewValue,
				Category: change.Category.String(),
				Reason:   change.Reason,
			}
		}

		return result
	}

	return DiffJSONOutput{
		TotalChanges:         diff.TotalChanges(),
		InPlaceChanges:       convertChanges(diff.InPlaceChanges),
		RebootRequired:       convertChanges(diff.RebootRequired),
		RecreateRequired:     convertChanges(diff.RecreateRequired),
		RollingRecreate:      convertChanges(diff.RollingRecreate),
		WipeRequired:         convertChanges(diff.WipeRequired),
		UnknownBaseline:      convertChanges(diff.UnknownBaseline),
		RequiresConfirmation: diff.NeedsUserConfirmation(),
	}
}

// emitDiffJSON serialises diff as indented JSON and writes it to cmd's stdout.
func emitDiffJSON(cmd *cobra.Command, diff *clusterupdate.UpdateResult) {
	out := diffToJSON(diff)

	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// Keep '<', '>', '&' literal instead of \u-escaping them; this is CLI
	// output, not HTML.
	enc.SetEscapeHTML(false)

	err := enc.Encode(out)
	if err != nil {
		// Encoding a plain struct with only basic types never fails.
		notify.Errorf(cmd.OutOrStderr(), "failed to marshal diff to JSON: %v", err)

		return
	}

	// enc.Encode already appends a trailing newline.
	_, _ = fmt.Fprint(cmd.OutOrStdout(), buf.String())
}
