package workload

import (
	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/fsnotify/fsnotify"
)

// ExportIsRelevantEvent exposes isRelevantEvent for testing.
func ExportIsRelevantEvent(event fsnotify.Event) bool {
	return isRelevantEvent(event)
}

// ExportResolveWatchDir exposes resolveWatchDir for testing.
func ExportResolveWatchDir(cfg *v1alpha1.Cluster, pathFlag string) string {
	return resolveWatchDir(cfg, pathFlag)
}

// ExportAddRecursive exposes addRecursive for testing.
func ExportAddRecursive(watcher *fsnotify.Watcher, root string) error {
	return addRecursive(watcher, root)
}
