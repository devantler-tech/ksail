package cluster

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/confirm"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/picker"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ErrContextNotFound is returned when the specified cluster does not have a matching context in the kubeconfig.
var ErrContextNotFound = errors.New("no matching context found for cluster")

// ErrAmbiguousCluster is returned when multiple distribution contexts match the cluster name.
var ErrAmbiguousCluster = errors.New("ambiguous cluster name")

// ErrNoClusters is returned when the kubeconfig holds no context to switch to.
var ErrNoClusters = errors.New("no clusters found in kubeconfig")

// unmanagedMarker labels a kubeconfig context KSail does not manage in the interactive picker.
// It matches the "Unmanaged" status `ksail cluster list` shows for the same context, and a
// marker is display text; picker targets retain their context identity separately.
const unmanagedMarker = " (unmanaged)"

// unmanagedCompletionDescription is the shell-completion description of an unmanaged context.
const unmanagedCompletionDescription = "unmanaged"

// ErrInteractivePickerNoTTY is returned when the interactive picker is requested
// (switch invoked without a cluster-name argument) but stdin is not a terminal —
// e.g. an AI tool client, CI pipeline, or piped input. Pass the cluster name as
// an argument instead of relying on the picker.
var ErrInteractivePickerNoTTY = errors.New(
	"cluster switch requires a cluster name when stdin is not a terminal " +
		"(the interactive picker needs a TTY); run: ksail cluster switch <name>",
)

// switchHistoryMaxItems is the maximum number of recently-switched clusters to remember.
const switchHistoryMaxItems = 5

// switchHistoryFileName is the file name used to persist switch history under ~/.ksail/.
const switchHistoryFileName = "switch-history.json"

// switchHistoryDirPerms is the permission mode for the ~/.ksail/ directory.
const switchHistoryDirPerms = 0o700

// switchHistoryFilePerms is the permission mode for the switch-history.json file.
const switchHistoryFilePerms = 0o600

// switchHistory is the JSON representation persisted to ~/.ksail/switch-history.json.
type switchHistory struct {
	Recent []string `json:"recent"`
}

// switchHistoryPath returns the path to the switch history file (~/.ksail/switch-history.json).
func switchHistoryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get user home directory: %w", err)
	}

	return filepath.Join(home, ".ksail", switchHistoryFileName), nil
}

// loadSwitchHistory reads the recent-cluster list from disk.
// Returns nil if the file does not exist or cannot be parsed.
//
//nolint:gosec // G304: path is derived from os.UserHomeDir() with a fixed suffix
func loadSwitchHistory() []string {
	path, err := switchHistoryPath()
	if err != nil {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var history switchHistory

	err = json.Unmarshal(data, &history)
	if err != nil {
		return nil
	}

	return history.Recent
}

// saveToSwitchHistory prepends name to the recent list, deduplicates, caps at
// switchHistoryMaxItems, and writes to disk. Errors are silently discarded so that
// a failed history write never blocks the user's context switch.
func saveToSwitchHistory(name string) {
	path, err := switchHistoryPath()
	if err != nil {
		return
	}

	existing := loadSwitchHistory()

	seen := map[string]struct{}{name: {}}
	updated := []string{name}

	for _, n := range existing {
		if _, dup := seen[n]; !dup {
			seen[n] = struct{}{}
			updated = append(updated, n)

			if len(updated) >= switchHistoryMaxItems {
				break
			}
		}
	}

	h := switchHistory{Recent: updated}

	data, err := json.Marshal(h)
	if err != nil {
		return
	}

	mkErr := os.MkdirAll(filepath.Dir(path), switchHistoryDirPerms)
	if mkErr != nil {
		return
	}

	_ = fsutil.AtomicWriteFile(path, data, switchHistoryFilePerms)
}

// switchKubeconfigFileMode is the file mode for kubeconfig files.
const switchKubeconfigFileMode = 0o600

const switchLongDesc = `Switch the active kubeconfig context to the named cluster.

This command accepts a cluster name and automatically resolves it to the
correct kubeconfig context by checking all supported distribution prefixes
(kind-, k3d-, k3k-, admin@, vcluster-docker_, kwok-). The k3k- prefix matches
nested K3s clusters on the Kubernetes provider, and kwok- matches KWOK clusters.

If multiple distributions have contexts for the same cluster name, the
command returns an error listing the matching contexts.

Contexts the kubeconfig does not show as clusters KSail manages, such as a
cloud cluster or a colleague's cluster, can be switched to by their exact
context name. The picker and shell completion list them too, marked
unmanaged as in ksail cluster list, and switching to one prints a notice.
Use --context to select an exact kubeconfig context without managed-name matching.
Unmanaged shell completions use this flag so colliding names remain selectable.

The kubeconfig is resolved in the following priority order:
  1. From KUBECONFIG environment variable
  2. From ksail.yaml config file (if present)
  3. Defaults to ~/.kube/config

When called without arguments, an interactive picker is shown
to select from available clusters. Recently-switched clusters
appear at the top of the list (up to the last 5), followed by
the remaining clusters in alphabetical order. The history is
persisted to ~/.ksail/switch-history.json.

In the picker, press '/' to enter filter mode and type to narrow
the list by keyword (case-insensitive). Press Esc to exit filter mode.

Examples:
  # Switch to a Vanilla (Kind) cluster named "dev"
  ksail cluster switch dev

  # Switch to a cluster named "staging"
  ksail cluster switch staging

  # Select an exact kubeconfig context, even if its name matches a managed cluster
  ksail cluster switch --context dev

  # Select a cluster interactively (recent clusters shown first)
  ksail cluster switch`

// NewSwitchCmd creates the switch command for clusters.
func NewSwitchCmd() *cobra.Command {
	var contextName string

	cmd := &cobra.Command{
		Use:   "switch [cluster-name]",
		Short: "Switch active cluster context",
		Long:  switchLongDesc,
		Args:  cobra.MaximumNArgs(1),
		// switch without an argument opens an interactive TTY picker (bubbletea),
		// which an AI tool client cannot drive — keep it out of the generated
		// MCP/chat tool surface. The picker path also returns a clean non-TTY
		// error (pickCluster) for any caller that reaches it without a terminal.
		Annotations: map[string]string{
			annotations.AnnotationInteractive: annotations.AnnotationValueTrue,
		},
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

			if cmd.Flags().Changed("context") {
				if len(args) != 0 || contextName == "" {
					return errors.New("--context requires a non-empty context name and no cluster-name argument")
				}

				return handleSwitchTarget(cmd, switchTarget{name: contextName, mode: exactContextTarget}, deps)
			}

			if len(args) > 0 {
				return HandleSwitchRunE(cmd, args[0], deps)
			}

			return HandleSwitchRunE(cmd, "", deps)
		},
	}

	cmd.Flags().StringVar(&contextName, "context", "", "Switch to this exact kubeconfig context")

	return cmd
}

// switchTarget keeps context identity separate from a picker label or managed cluster name.
type switchTarget struct {
	name string
	mode switchTargetMode
}

type switchTargetMode uint8

const (
	clusterNameTarget switchTargetMode = iota
	exactContextTarget
	prefixedContextTarget
)

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

	// IsTTY overrides the terminal detection used to gate the interactive picker.
	// If nil, confirm.IsTTY is used. Tests inject it because the go-test stdin
	// may report as a TTY even though bubbletea cannot open /dev/tty.
	IsTTY func() bool
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
	target := switchTarget{name: clusterName}
	if clusterName == "" {
		selected, err := pickCluster(cmd, deps)
		if err != nil {
			return err
		}

		target = selected
	}

	return handleSwitchTarget(cmd, target, deps)
}

// handleSwitchTarget applies a resolved selection without interpreting its display label.
func handleSwitchTarget(cmd *cobra.Command, target switchTarget, deps SwitchDeps) error {
	kubeconfigPath, err := resolveSwitchKubeconfig(cmd, deps)
	if err != nil {
		return err
	}

	contextName, managed, err := switchContext(kubeconfigPath, target)
	if err != nil {
		return err
	}

	displayName := target.name
	if !managed {
		displayName = contextName
	}

	notify.Successf(
		cmd.OutOrStdout(),
		"Switched to cluster '%s' (context: %s)",
		displayName,
		contextName,
	)

	if !managed {
		notify.Infof(
			cmd.OutOrStdout(),
			"context '%s' is unmanaged: the kubeconfig does not show it as a cluster "+
				"KSail manages, so cluster lifecycle commands may refuse it",
			contextName,
		)
	}

	// Persist the cluster name to switch history (errors silently ignored). Unmanaged contexts
	// are left out: the picker orders only managed clusters by recency, and an unmanaged entry
	// would evict managed ones or promote a managed cluster that shares its name.
	if managed {
		save := deps.SaveToSwitchHistory
		if save == nil {
			save = saveToSwitchHistory
		}

		save(stripParenthetical(target.name))
	}

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
// interactive picker for the user to select one. Contexts without a distribution
// prefix follow those clusters by their raw names, and the ones KSail does not
// manage are labelled with unmanagedMarker.
func pickCluster(cmd *cobra.Command, deps SwitchDeps) (switchTarget, error) {
	kubeconfigPath, err := resolveSwitchKubeconfig(cmd, deps)
	if err != nil {
		return switchTarget{}, err
	}

	clusterNames, otherContexts := kubeconfigClusterNames(kubeconfigPath)
	if len(clusterNames) == 0 && len(otherContexts) == 0 {
		return switchTarget{}, fmt.Errorf("%w", ErrNoClusters)
	}

	loadHistory := deps.LoadSwitchHistory
	if loadHistory == nil {
		loadHistory = loadSwitchHistory
	}

	names := buildOrderedClusterNames(loadHistory(), clusterNames)
	targets := make(map[string]switchTarget, len(otherContexts))
	for _, other := range otherContexts {
		label := other.pickerLabel()
		for slices.Contains(names, label) {
			label += " (context)"
		}

		names = append(names, label)
		targets[label] = switchTarget{name: other.name, mode: exactContextTarget}
	}

	pick := deps.PickCluster
	if pick == nil {
		pick = picker.Run

		// The default bubbletea picker needs a TTY. Fail fast with an actionable
		// error in non-interactive environments (AI tool clients, CI, piped
		// stdin) instead of hanging or crashing the alt-screen renderer. Tests
		// inject deps.PickCluster (bypassing this guard) or deps.IsTTY.
		isTTY := deps.IsTTY
		if isTTY == nil {
			isTTY = confirm.IsTTY
		}

		if !isTTY() {
			return switchTarget{}, ErrInteractivePickerNoTTY
		}
	}

	selected, err := pick("Select a cluster:", names)
	if err != nil {
		return switchTarget{}, fmt.Errorf("cluster selection: %w", err)
	}

	if !slices.Contains(names, selected) {
		return switchTarget{}, fmt.Errorf("%w: %s", ErrContextNotFound, selected)
	}

	if target, ok := targets[selected]; ok {
		return target, nil
	}

	return switchTarget{name: selected, mode: prefixedContextTarget}, nil
}

// resolveContextName finds the matching kubeconfig context for a cluster name
// by checking all known distribution context-name prefixes.
// Parenthetical suffixes (e.g., " (Vanilla)") are stripped defensively so that
// cluster names containing distribution hints still resolve correctly.
func resolveContextName(
	config *clientcmdapi.Config,
	clusterName string,
) (string, error) {
	// Preserve typed labels from earlier picker/completion output. Current picker
	// and --context selections carry their exact identity separately.
	if contextName, labelled := strings.CutSuffix(clusterName, unmanagedMarker); labelled {
		// A literal context may itself end in the marker. Typed full names retain
		// that identity; picker selections carry their exact identity separately.
		if _, exists := config.Contexts[clusterName]; exists {
			return clusterName, nil
		}

		if _, exists := config.Contexts[contextName]; exists {
			return contextName, nil
		}

		// Never fall back to another context that merely contains the name.
		return "", fmt.Errorf("%w: %s", ErrContextNotFound, contextName)
	}

	// Strip trailing parenthetical suffix (e.g., " (Vanilla)") that may be
	// present if the name was copied from enriched list output.
	cleanName := stripParenthetical(clusterName)

	matches := clusterdetector.MatchContexts(config, cleanName)

	return resolveContextMatches(config, clusterName, matches)
}

// resolveContextMatches reports missing and ambiguous targets consistently for each input mode.
func resolveContextMatches(
	config *clientcmdapi.Config,
	clusterName string,
	matches []string,
) (string, error) {
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
	before, _, ok := strings.CutLast(input, " (")
	if !ok {
		return input
	}

	if strings.HasSuffix(input, ")") {
		return before
	}

	return input
}

// switchContext loads the kubeconfig, resolves the cluster name to a context, and sets
// current-context. It returns the context switched to and whether KSail manages it.
func switchContext(kubeconfigPath string, target switchTarget) (string, bool, error) {
	canonicalPath, err := fsutil.EvalCanonicalPath(kubeconfigPath)
	if err != nil {
		return "", false, fmt.Errorf("failed to read kubeconfig: %w", err)
	}

	configBytes, err := os.ReadFile(canonicalPath) //nolint:gosec // canonicalized above
	if err != nil {
		return "", false, fmt.Errorf("failed to read kubeconfig: %w", err)
	}

	config, err := clientcmd.Load(configBytes)
	if err != nil {
		return "", false, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	contextName, err := target.resolve(config)
	if err != nil {
		return "", false, err
	}

	managed := isManagedContext(config, contextName)

	config.CurrentContext = contextName

	result, err := clientcmd.Write(*config)
	if err != nil {
		return "", false, fmt.Errorf("failed to serialize kubeconfig: %w", err)
	}

	err = fsutil.AtomicWriteFile(canonicalPath, result, switchKubeconfigFileMode)
	if err != nil {
		return "", false, fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	return contextName, managed, nil
}

// resolve preserves typed managed-name matching and requires exact picker/flag targets to exist.
func (target switchTarget) resolve(config *clientcmdapi.Config) (string, error) {
	if target.mode == clusterNameTarget {
		return resolveContextName(config, target.name)
	}

	if target.mode == prefixedContextTarget {
		prefixed := clientcmdapi.NewConfig()
		for name, context := range config.Contexts {
			if stripDistributionPrefix(name) != "" {
				prefixed.Contexts[name] = context
			}
		}

		return resolveContextMatches(prefixed, target.name, clusterdetector.MatchContexts(prefixed, target.name))
	}

	if _, exists := config.Contexts[target.name]; !exists {
		return "", fmt.Errorf("%w: %s", ErrContextNotFound, target.name)
	}

	return target.name, nil
}

// listClusterNames returns the shell-completion candidates from the kubeconfig: the
// deduplicated cluster names of its distribution-prefixed contexts (prefix stripped),
// then exact --context targets for unmanaged contexts, described as unmanaged. A managed
// raw name equal to one of those cluster names is left out, because it is already offered.
// When cmd is non-nil, the --config persistent flag is honored for config loading.
func listClusterNames(cmd *cobra.Command) []string {
	kubeconfigPath, err := resolveKubeconfigForSwitch(cmd)
	if err != nil {
		return nil
	}

	clusterNames, otherContexts := kubeconfigClusterNames(kubeconfigPath)

	candidates := make([]string, 0, len(clusterNames)+len(otherContexts))
	candidates = append(candidates, clusterNames...)

	for _, other := range otherContexts {
		if other.managed && slices.Contains(clusterNames, other.name) {
			continue
		}

		if other.managed {
			candidates = append(candidates, other.name)

			continue
		}

		candidates = append(candidates, "--context="+other.name+"\t"+unmanagedCompletionDescription)
	}

	return candidates
}

// kubeconfigContext is a kubeconfig context without a distribution prefix, and whether
// KSail manages the cluster it points at.
type kubeconfigContext struct {
	name    string
	managed bool
}

// pickerLabel returns the picker entry for the context: its raw name, followed by
// unmanagedMarker when KSail does not manage it.
func (c kubeconfigContext) pickerLabel() string {
	if c.managed {
		return c.name
	}

	return c.name + unmanagedMarker
}

// kubeconfigClusterNames reads the given kubeconfig and returns the sorted, deduplicated
// cluster names encoded in its distribution-prefixed contexts (prefix stripped), and
// every other context sorted by name. The other contexts are those KSail does not
// manage, plus managed ones whose names carry no distribution prefix, such as eksctl's
// or a Sidero Omni service-account context. A missing or unreadable kubeconfig yields
// neither.
//
//nolint:gosec // G304: kubeconfigPath is resolved from trusted config or default
func kubeconfigClusterNames(kubeconfigPath string) ([]string, []kubeconfigContext) {
	configBytes, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return nil, nil
	}

	config, err := clientcmd.Load(configBytes)
	if err != nil {
		return nil, nil
	}

	seen := make(map[string]struct{})
	otherContexts := make([]kubeconfigContext, 0)

	for contextName := range config.Contexts {
		if name := stripDistributionPrefix(contextName); name != "" {
			seen[name] = struct{}{}

			continue
		}

		otherContexts = append(otherContexts, kubeconfigContext{
			name:    contextName,
			managed: isManagedContext(config, contextName),
		})
	}

	clusterNames := make([]string, 0, len(seen))
	for name := range seen {
		clusterNames = append(clusterNames, name)
	}

	slices.Sort(clusterNames)
	slices.SortFunc(otherContexts, func(a, b kubeconfigContext) int {
		return strings.Compare(a.name, b.name)
	})

	return clusterNames, otherContexts
}

// isManagedContext reports whether a kubeconfig context points at a cluster KSail
// manages, judged offline from the kubeconfig alone: a distribution prefix (including
// the nested-K3s "k3k-" alias), the eksctl context shape, or a Sidero Omni server URL.
// Every other context belongs to a cluster KSail did not create, which
// `ksail cluster list` shows as Unmanaged.
func isManagedContext(config *clientcmdapi.Config, contextName string) bool {
	if stripDistributionPrefix(contextName) != "" {
		return true
	}

	_, _, err := clusterdetector.DetectDistributionFromConfigContext(config, contextName)

	return err == nil
}

// stripDistributionPrefix removes the distribution-specific prefix from a context name,
// returning the underlying cluster name. Returns empty string if the context name
// does not match any known distribution prefix. The prefix conventions (including
// the nested-on-Kubernetes "k3k-" alias) are owned by clusterdetector.
func stripDistributionPrefix(contextName string) string {
	_, clusterName, _ := clusterdetector.StripContextPrefix(contextName)

	return clusterName
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
