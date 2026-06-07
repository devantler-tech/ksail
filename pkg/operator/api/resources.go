package api

import (
	"context"
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const groupApps = "apps"

// ResourceQuery selects a set of resources to list from a target cluster.
type ResourceQuery struct {
	// Kind is one of the curated browsable kinds (see ResourceKindFor / ResourceKindNames).
	Kind string
	// Namespace restricts a namespaced kind to one namespace; empty lists across all namespaces.
	Namespace string
}

// ResourceRef identifies a single resource to fetch.
type ResourceRef struct {
	Kind      string
	Namespace string
	Name      string
}

// ResourceService is an optional interface a ClusterService may implement to expose READ-ONLY
// Kubernetes resource access for a target cluster (the workload browser). When the serving
// ClusterService implements it, the server registers the resource endpoints and advertises
// capabilities.workloadRead=true; otherwise the endpoints 404 and the SPA hides the Resources view.
//
// Implementations resolve a client for the named cluster — the local backend from the matching
// kubeconfig context, the operator from the cluster's kubeconfig secret — and list/get the requested
// kind as unstructured objects (which JSON-encode as the resource's native shape for the SPA).
type ResourceService interface {
	ListResources(
		ctx context.Context,
		namespace, name string,
		query ResourceQuery,
	) (*unstructured.UnstructuredList, error)
	GetResource(
		ctx context.Context,
		namespace, name string,
		ref ResourceRef,
	) (*unstructured.Unstructured, error)
}

// ResourceKind describes a browsable resource type and its mapping to a GroupVersionResource.
type ResourceKind struct {
	GVR        schema.GroupVersionResource
	Namespaced bool
}

// namespacedKind / clusterScopedKind build a ResourceKind for a v1 GroupVersionResource. All
// browsable kinds are served at version v1.
func namespacedKind(group, resource string) ResourceKind {
	return ResourceKind{
		GVR:        schema.GroupVersionResource{Group: group, Version: "v1", Resource: resource},
		Namespaced: true,
	}
}

func clusterScopedKind(group, resource string) ResourceKind {
	return ResourceKind{
		GVR:        schema.GroupVersionResource{Group: group, Version: "v1", Resource: resource},
		Namespaced: false,
	}
}

// resourceKindTable is the curated allowlist of resource types the read-only workload browser
// exposes. It deliberately EXCLUDES Secrets: their values are sensitive and a redaction-aware secrets
// view is a separate feature. New browsable kinds are added here. It is a function (not a package
// global) to keep the allowlist in one place while satisfying the no-globals lint.
func resourceKindTable() map[string]ResourceKind {
	return map[string]ResourceKind{
		"Pod":                   namespacedKind("", "pods"),
		"Service":               namespacedKind("", "services"),
		"ConfigMap":             namespacedKind("", "configmaps"),
		"PersistentVolumeClaim": namespacedKind("", "persistentvolumeclaims"),
		"Event":                 namespacedKind("", "events"),
		"Node":                  clusterScopedKind("", "nodes"),
		"Namespace":             clusterScopedKind("", "namespaces"),
		"Deployment":            namespacedKind(groupApps, "deployments"),
		"StatefulSet":           namespacedKind(groupApps, "statefulsets"),
		"DaemonSet":             namespacedKind(groupApps, "daemonsets"),
		"ReplicaSet":            namespacedKind(groupApps, "replicasets"),
		"Job":                   namespacedKind("batch", "jobs"),
		"CronJob":               namespacedKind("batch", "cronjobs"),
		"Ingress":               namespacedKind("networking.k8s.io", "ingresses"),
	}
}

// ResourceKindFor returns the GVR mapping for a browsable kind, or an ErrInvalid-wrapped error (→ 422)
// for any kind outside the curated allowlist, so unknown/forbidden kinds cannot be queried.
func ResourceKindFor(kind string) (ResourceKind, error) {
	resourceKind, ok := resourceKindTable()[kind]
	if !ok {
		return ResourceKind{}, fmt.Errorf("%w: unsupported resource kind %q", ErrInvalid, kind)
	}

	return resourceKind, nil
}

// ResourceKindNames returns the curated browsable kinds, sorted, for the SPA's kind selector.
func ResourceKindNames() []string {
	table := resourceKindTable()
	names := make([]string, 0, len(table))

	for name := range table {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}
