package api

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

const groupApps = "apps"

// groupMetrics is the metrics API group (metrics.k8s.io); its kinds are allowlisted for reads but
// not browsable (see resourceKindBrowsable).
const groupMetrics = "metrics.k8s.io"

// Workload kind names referenced in both the allowlist and the scalable/restartable predicates.
const (
	kindDeployment  = "Deployment"
	kindStatefulSet = "StatefulSet"
	kindDaemonSet   = "DaemonSet"
	kindReplicaSet  = "ReplicaSet"
)

// kindApplication is the ArgoCD Application kind, referenced in the allowlist, the reconcilable
// predicate, and the reconcile-annotation selection.
const kindApplication = "Application"

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

// clusterScopedKindVersion builds a cluster-scoped ResourceKind at an explicit API version — used
// for the metrics API (metrics.k8s.io v1beta1), which is not served at v1.
func clusterScopedKindVersion(group, version, resource string) ResourceKind {
	return ResourceKind{
		GVR:        schema.GroupVersionResource{Group: group, Version: version, Resource: resource},
		Namespaced: false,
	}
}

// resourceKindEntry pairs an allowlisted kind name with its GVR mapping. The allowlist is ordered:
// the /api/v1/meta resourceKinds payload preserves entry order, which is the order the SPA's kind
// selector presents (workloads first, then config/network/cluster scope, then the GitOps CRs, then
// the non-browsable metrics kinds).
type resourceKindEntry struct {
	name string
	kind ResourceKind
}

// resourceKindEntries is the curated allowlist of resource types the read-only workload browser
// exposes. It deliberately EXCLUDES Secrets: their values are sensitive and a redaction-aware secrets
// view is a separate feature. New browsable kinds are added here (and only here: the lookup table and
// the /api/v1/meta resourceKinds payload both derive from this list). It is a function (not a package
// global) to keep the allowlist in one place while satisfying the no-globals lint.
func resourceKindEntries() []resourceKindEntry {
	entries := builtinKindEntries()
	entries = append(entries, gitOpsKindEntries()...)
	entries = append(entries, metricsKindEntries()...)

	return entries
}

// builtinKindEntries lists the built-in API kinds (core, apps, batch, networking), all served at v1.
func builtinKindEntries() []resourceKindEntry {
	return []resourceKindEntry{
		{name: "Pod", kind: namespacedKind("", "pods")},
		{name: kindDeployment, kind: namespacedKind(groupApps, "deployments")},
		{name: kindStatefulSet, kind: namespacedKind(groupApps, "statefulsets")},
		{name: kindDaemonSet, kind: namespacedKind(groupApps, "daemonsets")},
		{name: kindReplicaSet, kind: namespacedKind(groupApps, "replicasets")},
		{name: "Job", kind: namespacedKind("batch", "jobs")},
		{name: "CronJob", kind: namespacedKind("batch", "cronjobs")},
		{name: "Service", kind: namespacedKind("", "services")},
		{name: "Ingress", kind: namespacedKind("networking.k8s.io", "ingresses")},
		{name: "ConfigMap", kind: namespacedKind("", "configmaps")},
		{name: "PersistentVolumeClaim", kind: namespacedKind("", "persistentvolumeclaims")},
		{name: "Event", kind: namespacedKind("", "events")},
		{name: "Node", kind: clusterScopedKind("", "nodes")},
		{name: "Namespace", kind: clusterScopedKind("", "namespaces")},
	}
}

// gitOpsKindEntries lists the GitOps CRs (Flux + ArgoCD), browsable read-only so the reconciliation
// status (status conditions) is visible. A kind whose CRD is not installed lists with an error,
// surfaced as a normal error in the browser. Versions are the cluster-served ones, not all v1.
func gitOpsKindEntries() []resourceKindEntry {
	return []resourceKindEntry{
		{
			name: "Kustomization",
			kind: namespacedKindVersion("kustomize.toolkit.fluxcd.io", "v1", "kustomizations"),
		},
		{
			name: "HelmRelease",
			kind: namespacedKindVersion("helm.toolkit.fluxcd.io", "v2", "helmreleases"),
		},
		{
			name: "GitRepository",
			kind: namespacedKindVersion("source.toolkit.fluxcd.io", "v1", "gitrepositories"),
		},
		{
			name: "OCIRepository",
			kind: namespacedKindVersion("source.toolkit.fluxcd.io", "v1", "ocirepositories"),
		},
		{
			name: kindApplication,
			kind: namespacedKindVersion("argoproj.io", "v1alpha1", "applications"),
		},
	}
}

// metricsKindEntries lists the live-usage kinds from the metrics API (metrics.k8s.io, served by a
// metrics-server). These power the Overview's resource-usage monitoring; on a cluster without a
// metrics-server the list fails like any other absent API and the UI degrades to capacity/requests
// only. They are allowlisted for reads but not browsable (see resourceKindBrowsable).
func metricsKindEntries() []resourceKindEntry {
	return []resourceKindEntry{
		{
			name: "NodeMetrics",
			kind: clusterScopedKindVersion(groupMetrics, "v1beta1", "nodes"),
		},
		{
			name: "PodMetrics",
			kind: namespacedKindVersion(groupMetrics, "v1beta1", "pods"),
		},
	}
}

// resourceKindTable indexes the allowlist by kind name for validation lookups (see ResourceKindFor).
func resourceKindTable() map[string]ResourceKind {
	entries := resourceKindEntries()
	table := make(map[string]ResourceKind, len(entries))

	for _, entry := range entries {
		table[entry.name] = entry.kind
	}

	return table
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
	case "Kustomization", "HelmRelease", "GitRepository", "OCIRepository", kindApplication:
		return true
	default:
		return false
	}
}

// resourceKindBrowsable reports whether an allowlisted kind belongs in the SPA's kind selector. The
// metrics-API kinds (NodeMetrics/PodMetrics) are deliberately allowlisted for reads — they power the
// Overview's resource-usage monitoring — but they are derived live-usage data, not browsable
// workloads, so the selector hides them. Surfaced to the SPA via /api/v1/meta's resourceKinds.
func resourceKindBrowsable(kind ResourceKind) bool {
	return kind.GVR.Group != groupMetrics
}

// requireNamespace returns an ErrInvalid-wrapped error (→ 422) when a namespaced kind is addressed
// without a namespace, so single-resource write actions surface a clear message instead of an opaque
// API-server 500 from an empty-namespace request.
func requireNamespace(kind ResourceKind, ref ResourceRef) error {
	if kind.Namespaced && ref.Namespace == "" {
		return fmt.Errorf("%w: namespace is required for %q", ErrInvalid, ref.Kind)
	}

	return nil
}

// mergePatchWith resolves the allowlisted kind, requires a namespace for the target, and applies a
// JSON merge patch via the dynamic client. verb labels the error ("scale", "restart", "reconcile").
// Shared by the scale/restart/reconcile write actions so the resolve-and-patch boilerplate lives in
// one place across the local and operator backends.
func mergePatchWith(
	ctx context.Context,
	dyn dynamic.Interface,
	verb string,
	ref ResourceRef,
	patch []byte,
) error {
	kind, err := ResourceKindFor(ref.Kind)
	if err != nil {
		return err
	}

	err = requireNamespace(kind, ref)
	if err != nil {
		return err
	}

	_, err = dyn.Resource(kind.GVR).Namespace(ref.Namespace).
		Patch(ctx, ref.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("%s %s %q: %w", verb, ref.Kind, ref.Name, err)
	}

	return nil
}

// ScaleResourceWith sets the replica count of a scalable workload via a merge patch on spec.replicas.
// Shared by the local and operator ResourceWriter backends; the only per-backend difference is how the
// dynamic client is obtained.
func ScaleResourceWith(
	ctx context.Context,
	dyn dynamic.Interface,
	ref ResourceRef,
	replicas int32,
) error {
	if !ResourceKindScalable(ref.Kind) {
		return fmt.Errorf("%w: %q is not scalable", ErrInvalid, ref.Kind)
	}

	if replicas < 0 {
		return fmt.Errorf("%w: replicas must be >= 0", ErrInvalid)
	}

	patch := fmt.Appendf(nil, `{"spec":{"replicas":%d}}`, replicas)

	return mergePatchWith(ctx, dyn, "scale", ref, patch)
}

// RestartResourceWith triggers a rolling restart by stamping the pod template's restartedAt annotation
// — the same mechanism `kubectl rollout restart` uses. The stamp is nanosecond-resolution so two
// restarts issued within the same second still change the value and reliably roll the workload.
func RestartResourceWith(ctx context.Context, dyn dynamic.Interface, ref ResourceRef) error {
	if !ResourceKindRestartable(ref.Kind) {
		return fmt.Errorf("%w: %q does not support rollout restart", ErrInvalid, ref.Kind)
	}

	patch := fmt.Appendf(
		nil,
		`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`,
		time.Now().Format(time.RFC3339Nano),
	)

	return mergePatchWith(ctx, dyn, "restart", ref, patch)
}

// ReconcileResourceWith triggers an immediate GitOps reconcile by stamping the engine-specific
// annotation — the same mechanism `flux reconcile` / an ArgoCD refresh use. Flux watches
// reconcile.fluxcd.io/requestedAt (nanosecond stamp so repeats differ); ArgoCD watches
// argocd.argoproj.io/refresh.
func ReconcileResourceWith(ctx context.Context, dyn dynamic.Interface, ref ResourceRef) error {
	if !ResourceKindReconcilable(ref.Kind) {
		return fmt.Errorf("%w: %q does not support reconcile", ErrInvalid, ref.Kind)
	}

	key, value := reconcileAnnotation(ref.Kind)
	patch := fmt.Appendf(nil, `{"metadata":{"annotations":{%q:%q}}}`, key, value)

	return mergePatchWith(ctx, dyn, "reconcile", ref, patch)
}

// reconcileAnnotation returns the metadata annotation (key, value) that triggers a reconcile for the
// kind's GitOps engine.
func reconcileAnnotation(kind string) (string, string) {
	if kind == kindApplication {
		return "argocd.argoproj.io/refresh", "normal"
	}

	return "reconcile.fluxcd.io/requestedAt", time.Now().Format(time.RFC3339Nano)
}

// DeleteResourceWith deletes a namespaced allowlisted resource. Cluster-scoped kinds (Node, Namespace)
// are intentionally NOT deletable from the workload browser — those are high-blast-radius operations
// (a Namespace delete cascades to everything in it) better left to the CLI. Shared by both backends.
func DeleteResourceWith(ctx context.Context, dyn dynamic.Interface, ref ResourceRef) error {
	kind, err := ResourceKindFor(ref.Kind)
	if err != nil {
		return err
	}

	if !kind.Namespaced {
		return fmt.Errorf(
			"%w: cluster-scoped %q cannot be deleted from the workload browser",
			ErrInvalid,
			ref.Kind,
		)
	}

	err = requireNamespace(kind, ref)
	if err != nil {
		return err
	}

	err = dyn.Resource(kind.GVR).Namespace(ref.Namespace).
		Delete(ctx, ref.Name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete %s %q: %w", ref.Kind, ref.Name, err)
	}

	return nil
}
