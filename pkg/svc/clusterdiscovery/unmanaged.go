package clusterdiscovery

import (
	"log/slog"
	"sort"

	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// DiscoverUnmanaged synthesizes a Cluster for every kubeconfig context at kubeconfigPath that does
// NOT correspond to a cluster ksail already discovered (managed) — flagged RunStateUnmanaged, with
// empty Distribution/Provider (both unknown without a ksail spec). It lets a surface see clusters
// that exist in the user's kubeconfig but were not provisioned by ksail (a managed cloud cluster, a
// kubeadm cluster, a colleague's cluster), so `ksail cluster list` matches the web-UI model
// (pkg/cli/clusterapi) where an unmanaged cluster is visible — clearly marked — rather than invisible.
//
// managed is the set of names already discovered via infrastructure providers; a context is skipped
// when its ksail-detected name (or, when detection fails, its raw context name) is in managed, so a
// managed cluster never appears twice. Best-effort and offline (no cluster round-trips): a missing or
// unreadable kubeconfig yields none. Results are sorted by context name so the list stays stable.
func DiscoverUnmanaged(kubeconfigPath string, managed map[string]struct{}) []Cluster {
	config := LoadKubeconfig(kubeconfigPath)

	isManaged := func(name string) bool {
		_, ok := managed[name]

		return ok
	}

	contextNames := UnmanagedContextNames(config, isManaged)

	unmanaged := make([]Cluster, 0, len(contextNames))
	for _, contextName := range contextNames {
		unmanaged = append(unmanaged, Cluster{
			Name:     contextName,
			RunState: RunStateUnmanaged,
		})
	}

	return unmanaged
}

// UnmanagedContextNames returns, sorted and deduped, the names of every kubeconfig context in config
// that does NOT correspond to an already-managed cluster — the enumerate-and-dedup rule shared by the
// CLI's DiscoverUnmanaged and the web-UI model (pkg/cli/clusterapi), so the skeleton lives in exactly
// one place and cannot drift. isManaged reports whether a name is in the caller's managed set (each
// surface keys its own map); a context is skipped when ContextIsManaged accepts it under isManaged. A
// nil config (missing or unreadable kubeconfig) yields none. Results are sorted by context name so the
// list stays stable across surfaces.
func UnmanagedContextNames(config *clientcmdapi.Config, isManaged func(name string) bool) []string {
	if config == nil {
		return nil
	}

	contextNames := make([]string, 0, len(config.Contexts))
	for contextName := range config.Contexts {
		contextNames = append(contextNames, contextName)
	}

	sort.Strings(contextNames)

	unmanaged := make([]string, 0, len(contextNames))

	for _, contextName := range contextNames {
		if ContextIsManaged(contextName, isManaged) {
			continue
		}

		unmanaged = append(unmanaged, contextName)
	}

	return unmanaged
}

// ContextIsManaged reports whether a kubeconfig context corresponds to a cluster already discovered,
// so an unmanaged-cluster synthesizer does not re-surface it. A context maps to a managed cluster when
// its ksail-detected name — or, when detection fails, the raw context name — satisfies isManaged. Both
// keys are checked because a Docker cluster's context ("kind-dev") detects to its ksail name ("dev"),
// which is what discovery keys the cluster by. Shared by the CLI's DiscoverUnmanaged and the web-UI
// model (pkg/cli/clusterapi) so the dedup rule lives in exactly one place.
func ContextIsManaged(contextName string, isManaged func(name string) bool) bool {
	if isManaged(contextName) {
		return true
	}

	_, name, err := clusterdetector.DetectDistributionFromContext(contextName)
	if err == nil && isManaged(name) {
		return true
	}

	return false
}

// LoadKubeconfig loads a kubeconfig best-effort: a missing kubeconfig is normal and a malformed one
// must not turn a caller into a failure, so both yield nil (logged at debug so the cause stays
// discoverable). Shared by DiscoverUnmanaged and the web-UI model (pkg/cli/clusterapi) so the
// nil-on-error load semantics live in one place and cannot drift.
func LoadKubeconfig(kubeconfigPath string) *clientcmdapi.Config {
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		slog.Debug("load kubeconfig", "path", kubeconfigPath, "error", err)

		return nil
	}

	return config
}
