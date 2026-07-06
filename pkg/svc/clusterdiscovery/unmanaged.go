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
	config := loadKubeconfigForUnmanaged(kubeconfigPath)
	if config == nil {
		return nil
	}

	contextNames := make([]string, 0, len(config.Contexts))
	for contextName := range config.Contexts {
		contextNames = append(contextNames, contextName)
	}

	sort.Strings(contextNames)

	unmanaged := make([]Cluster, 0, len(contextNames))

	for _, contextName := range contextNames {
		if contextIsManaged(contextName, managed) {
			continue
		}

		unmanaged = append(unmanaged, Cluster{
			Name:     contextName,
			RunState: RunStateUnmanaged,
		})
	}

	return unmanaged
}

// contextIsManaged reports whether a kubeconfig context corresponds to a cluster already discovered,
// so DiscoverUnmanaged does not re-surface it. A context maps to a managed cluster when its
// ksail-detected name — or, when detection fails, the raw context name — is a key in managed. Both
// keys are checked because a Docker cluster's context ("kind-dev") detects to its ksail name ("dev"),
// which is what discovery keys the cluster by.
func contextIsManaged(contextName string, managed map[string]struct{}) bool {
	if _, ok := managed[contextName]; ok {
		return true
	}

	_, name, err := clusterdetector.DetectDistributionFromContext(contextName)
	if err == nil {
		if _, ok := managed[name]; ok {
			return true
		}
	}

	return false
}

// loadKubeconfigForUnmanaged loads the kubeconfig best-effort: a missing kubeconfig is normal and a
// malformed one must not turn discovery into a failure, so both yield nil (logged at debug so the
// cause stays discoverable). Mirrors pkg/cli/clusterapi's loader.
func loadKubeconfigForUnmanaged(kubeconfigPath string) *clientcmdapi.Config {
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		slog.Debug("load kubeconfig for unmanaged cluster discovery",
			"path", kubeconfigPath, "error", err)

		return nil
	}

	return config
}
