package api

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const groupApps = "apps"

// Workload kind names referenced in both the allowlist and the scalable/restartable predicates.
const (
	kindDeployment  = "Deployment"
	kindStatefulSet = "StatefulSet"
	kindDaemonSet   = "DaemonSet"
	kindReplicaSet  = "ReplicaSet"
)

// ResourceQuery selects a set of resources to list from a target cluster.
type ResourceQuery struct {
	// Kind is one of the curated browsable kinds (see ResourceKindFor).
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

// namespacedKindVersion builds a namespaced ResourceKind at an explicit API version — used for the
// GitOps CRDs (Flux/ArgoCD) whose served versions are not v1 (e.g. HelmRelease v2, Application
// v1alpha1).
func namespacedKindVersion(group, version, resource string) ResourceKind {
	return ResourceKind{
		GVR:        schema.GroupVersionResource{Group: group, Version: version, Resource: resource},
		Namespaced: true,
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
		kindDeployment:          namespacedKind(groupApps, "deployments"),
		kindStatefulSet:         namespacedKind(groupApps, "statefulsets"),
		kindDaemonSet:           namespacedKind(groupApps, "daemonsets"),
		kindReplicaSet:          namespacedKind(groupApps, "replicasets"),
		"Job":                   namespacedKind("batch", "jobs"),
		"CronJob":               namespacedKind("batch", "cronjobs"),
		"Ingress":               namespacedKind("networking.k8s.io", "ingresses"),
		// GitOps CRs (Flux + ArgoCD), browsable read-only so the reconciliation status (status
		// conditions) is visible. A kind whose CRD is not installed lists with an error, surfaced as a
		// normal error in the browser. Versions are the cluster-served ones, not all v1.
		"Kustomization": namespacedKindVersion(
			"kustomize.toolkit.fluxcd.io",
			"v1",
			"kustomizations",
		),
		"HelmRelease":   namespacedKindVersion("helm.toolkit.fluxcd.io", "v2", "helmreleases"),
		"GitRepository": namespacedKindVersion("source.toolkit.fluxcd.io", "v1", "gitrepositories"),
		"OCIRepository": namespacedKindVersion("source.toolkit.fluxcd.io", "v1", "ocirepositories"),
		"Application":   namespacedKindVersion("argoproj.io", "v1alpha1", "applications"),
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

// ListResourcesWith lists a resolved kind from a dynamic client, scoped to query.Namespace for a
// namespaced kind (empty Namespace lists across all namespaces). It is shared by the local and
// operator backends so the only per-backend difference is how the dynamic client is obtained.
func ListResourcesWith(
	ctx context.Context,
	dyn dynamic.Interface,
	kind ResourceKind,
	query ResourceQuery,
) (*unstructured.UnstructuredList, error) {
	lister := dynamic.ResourceInterface(dyn.Resource(kind.GVR))
	if kind.Namespaced && query.Namespace != "" {
		lister = dyn.Resource(kind.GVR).Namespace(query.Namespace)
	}

	list, err := lister.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", query.Kind, err)
	}

	return list, nil
}

// GetResourceWith fetches a single resolved resource from a dynamic client. Shared by both backends.
func GetResourceWith(
	ctx context.Context,
	dyn dynamic.Interface,
	kind ResourceKind,
	ref ResourceRef,
) (*unstructured.Unstructured, error) {
	getter := dynamic.ResourceInterface(dyn.Resource(kind.GVR))
	if kind.Namespaced {
		getter = dyn.Resource(kind.GVR).Namespace(ref.Namespace)
	}

	obj, err := getter.Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s %q: %w", ref.Kind, ref.Name, err)
	}

	return obj, nil
}

// ScaleRequest is the JSON body of a scale request.
type ScaleRequest struct {
	Replicas int32 `json:"replicas"`
}

// ResourceWriter is an optional interface a ClusterService may implement to expose a SMALL, safe set
// of write actions on browsable resources: scale, rollout restart, and delete. When the serving
// ClusterService implements it, the server registers the mutating resource routes (still subject to
// the read-only guard) and advertises capabilities.workloadWrite=true; otherwise the SPA hides the
// action affordances. Apply/exec/secrets are separate, later features — not part of this surface.
type ResourceWriter interface {
	// ScaleResource sets the replica count of a scalable workload (see ResourceKindScalable).
	ScaleResource(
		ctx context.Context,
		namespace, name string,
		ref ResourceRef,
		replicas int32,
	) error
	// RestartResource triggers a rolling restart of a workload (see ResourceKindRestartable).
	RestartResource(ctx context.Context, namespace, name string, ref ResourceRef) error
	// DeleteResource deletes any allowlisted resource.
	DeleteResource(ctx context.Context, namespace, name string, ref ResourceRef) error
	// ReconcileResource triggers an immediate GitOps reconcile of a Flux/ArgoCD resource (see
	// ResourceKindReconcilable).
	ReconcileResource(ctx context.Context, namespace, name string, ref ResourceRef) error
}

// ResourceKindScalable reports whether a kind supports `scale` (and is in the allowlist). Used to
// validate requests and to drive the SPA's scale affordance.
func ResourceKindScalable(kind string) bool {
	switch kind {
	case kindDeployment, kindStatefulSet, kindReplicaSet:
		return true
	default:
		return false
	}
}

// ResourceKindRestartable reports whether a kind supports rollout restart (a pod-template annotation
// bump). Delete applies to any allowlisted kind, so it has no dedicated predicate.
func ResourceKindRestartable(kind string) bool {
	switch kind {
	case kindDeployment, kindStatefulSet, kindDaemonSet:
		return true
	default:
		return false
	}
}

// ResourceKindReconcilable reports whether a kind supports an immediate GitOps reconcile — the Flux
// CRs (annotated reconcile.fluxcd.io/requestedAt) and the ArgoCD Application (annotated
// argocd.argoproj.io/refresh). Drives the SPA's Reconcile affordance and validates requests.
func ResourceKindReconcilable(kind string) bool {
	switch kind {
	case "Kustomization", "HelmRelease", "GitRepository", "OCIRepository", "Application":
		return true
	default:
		return false
	}
}
