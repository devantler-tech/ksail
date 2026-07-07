package cluster

import (
	"context"
	"errors"
	"fmt"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
)

// ErrUnmanagedCluster indicates a ksail-only lifecycle action (start/stop/…) was attempted against a
// kubeconfig context ksail did not provision — an unmanaged cluster (a managed cloud cluster, a
// kubeadm cluster, a colleague's cluster). Read-only operations still work; only ksail-only
// lifecycle actions are refused (ksail#5885, part of the unmanaged-cluster surface epic #5654).
var ErrUnmanagedCluster = errors.New("cluster is not managed by ksail")

// managedClusterLister enumerates the names of clusters ksail manages across every provider and
// reports whether discovery was complete (no provider failed). The guard fails open when discovery
// is incomplete so a transient provider error (e.g. a stopped Docker daemon) never wrongly refuses a
// genuine managed cluster. Overridable in tests.
type managedClusterLister func(ctx context.Context) (managed map[string]struct{}, complete bool)

// discoverManagedClusters is the production managedClusterLister: it queries every provider via the
// shared clusterdiscovery.Discoverer — the same enumeration `ksail cluster list` uses — and keys the
// result by cluster name. complete is false when any provider failed to list, so the guard can fail
// open rather than refuse a cluster the failed provider might actually manage.
func discoverManagedClusters(ctx context.Context) (map[string]struct{}, bool) {
	clusters, failures := (&clusterdiscovery.Discoverer{}).
		Discover(ctx, clusterdiscovery.DefaultProviders())

	managed := make(map[string]struct{}, len(clusters))
	for _, cluster := range clusters {
		managed[cluster.Name] = struct{}{}
	}

	return managed, len(failures) == 0
}

// ensureClusterManaged rejects a ksail-only lifecycle action when the target cluster is NOT among
// ksail's managed clusters but a matching kubeconfig context DOES exist — i.e. the user is pointing
// ksail at a cluster it did not provision. It is best-effort and FAILS OPEN so a genuine managed
// cluster is never wrongly refused: it returns nil when discovery could not fully enumerate every
// provider (complete=false), or when the kubeconfig has no matching context (a nonexistent cluster,
// left to the normal not-found path). Only a resolved cluster that is unmanaged AND present in the
// kubeconfig is refused.
func ensureClusterManaged(
	ctx context.Context,
	resolved *lifecycle.ResolvedClusterInfo,
	lister managedClusterLister,
) error {
	managed, complete := lister(ctx)
	if !complete {
		return nil
	}

	isManaged := func(name string) bool {
		_, ok := managed[name]

		return ok
	}

	if clusterdiscovery.ContextIsManaged(resolved.ClusterName, isManaged) {
		return nil
	}

	if !kubeconfigHasClusterContext(resolved.KubeconfigPath, resolved.ClusterName) {
		return nil
	}

	return fmt.Errorf(
		"%q is an unmanaged cluster: %w; read-only operations (list, resource browsing, logs, exec) still work",
		resolved.ClusterName,
		ErrUnmanagedCluster,
	)
}

// kubeconfigHasClusterContext reports whether the kubeconfig at kubeconfigPath contains a context
// that maps to clusterName — directly, or via ksail's context→name detection so a Docker cluster's
// "kind-dev" context matches the ksail name "dev". It reuses clusterdiscovery.ContextIsManaged so the
// context↔name mapping stays defined in exactly one place. Best-effort: a missing/unreadable
// kubeconfig yields false (treated as "not an unmanaged cluster, just absent").
func kubeconfigHasClusterContext(kubeconfigPath, clusterName string) bool {
	config := clusterdiscovery.LoadKubeconfig(kubeconfigPath)
	if config == nil {
		return false
	}

	matchesClusterName := func(name string) bool { return name == clusterName }
	for contextName := range config.Contexts {
		if clusterdiscovery.ContextIsManaged(contextName, matchesClusterName) {
			return true
		}
	}

	return false
}

// unmanagedClusterGuard is the SimpleLifecycleConfig.Guard shared by the start and stop commands: it
// rejects an action on a cluster ksail did not provision, using the real cross-provider discoverer.
func unmanagedClusterGuard(ctx context.Context, resolved *lifecycle.ResolvedClusterInfo) error {
	return ensureClusterManaged(ctx, resolved, discoverManagedClusters)
}

// updateUnmanagedGuardFunc is the unmanaged-cluster guard `cluster update` applies before it
// reconciles any configuration. It defaults to the real cross-provider guard; tests override it
// (via ExportSetUpdateUnmanagedGuard) so the refusal path is exercised without a live provider.
//
//nolint:gochecknoglobals // dependency injection for tests
var updateUnmanagedGuardFunc = unmanagedClusterGuard

// guardUpdateTargetManaged refuses `cluster update` when its target is a cluster ksail did not
// provision. Unlike delete/start/stop, `cluster update` has no --kubeconfig flag — it resolves its
// target from ksail.yaml — so the kubeconfig path is read from the loaded ClusterCfg and resolved
// the same way ResolveClusterInfo does, then handed to the shared unmanaged-cluster guard. The guard
// fails open on incomplete discovery, so a genuine managed cluster is never wrongly refused.
// (ksail#5885, epic #5654.)
func guardUpdateTargetManaged(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) error {
	kubeconfigPath, err := clusterdetector.ResolveKubeconfigPath(
		clusterCfg.Spec.Cluster.Connection.Kubeconfig,
	)
	if err != nil {
		return fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	// Canonicalize the user-supplied path before the guard reads the kubeconfig (repo path-safety
	// guideline; EvalCanonicalPath tolerates a not-yet-existing file by resolving its parent, so a
	// missing kubeconfig still falls through to the guard's fail-open path).
	canonical, err := fsutil.EvalCanonicalPath(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("canonicalize kubeconfig path %q: %w", kubeconfigPath, err)
	}

	return updateUnmanagedGuardFunc(ctx, &lifecycle.ResolvedClusterInfo{
		ClusterName:    clusterName,
		KubeconfigPath: canonical,
	})
}
