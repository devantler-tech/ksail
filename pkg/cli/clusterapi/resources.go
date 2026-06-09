package clusterapi

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Ensure the local backend exposes the read-only resource browser and the safe write actions.
var (
	_ api.ResourceService = (*Service)(nil)
	_ api.ResourceWriter  = (*Service)(nil)
)

// dynamicClientFunc builds a dynamic client for the named local cluster. Injectable so tests can
// substitute a fake client instead of resolving a real kubeconfig context.
type dynamicClientFunc func(ctx context.Context, clusterName string) (dynamic.Interface, error)

// defaultDynamicClient resolves the kubeconfig context for a local cluster by name and builds a
// dynamic client against it. The cluster name (as the UI lists it) is matched to a context using the
// same distribution-context patterns the detector uses (kind-<name>, k3d-<name>, admin@<name>, …).
func defaultDynamicClient(_ context.Context, clusterName string) (dynamic.Interface, error) {
	kubeconfigPath := k8s.DefaultKubeconfigPath()

	contextName, err := contextForCluster(kubeconfigPath, clusterName)
	if err != nil {
		return nil, err
	}

	client, err := k8s.NewDynamicClient(kubeconfigPath, contextName)
	if err != nil {
		return nil, fmt.Errorf("build dynamic client for %q: %w", clusterName, err)
	}

	return client, nil
}

// contextForCluster finds the kubeconfig context whose detected cluster name matches clusterName.
// Returns an ErrNotFound-wrapped error (→ 404) when no context matches.
func contextForCluster(kubeconfigPath, clusterName string) (string, error) {
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("load kubeconfig %q: %w", kubeconfigPath, err)
	}

	for contextName := range config.Contexts {
		_, name, detectErr := clusterdetector.DetectDistributionFromContext(contextName)
		if detectErr == nil && name == clusterName {
			return contextName, nil
		}
	}

	return "", fmt.Errorf("%w: no kubeconfig context for cluster %q", api.ErrNotFound, clusterName)
}

// restConfigForCluster resolves the cluster's kubeconfig context by name and builds a *rest.Config —
// the shared preamble for the apply (dynamic + RESTMapper) and exec (clientset) client builders.
func restConfigForCluster(clusterName string) (*rest.Config, error) {
	kubeconfigPath := k8s.DefaultKubeconfigPath()

	contextName, err := contextForCluster(kubeconfigPath, clusterName)
	if err != nil {
		return nil, err
	}

	restConfig, err := k8s.BuildRESTConfig(kubeconfigPath, contextName)
	if err != nil {
		return nil, fmt.Errorf("build rest config for %q: %w", clusterName, err)
	}

	return restConfig, nil
}

// resolveKindAndClient resolves an allowlisted kind to its GVR mapping and builds a dynamic client
// for the named cluster — the shared preamble of every resource read/write method.
func (s *Service) resolveKindAndClient(
	ctx context.Context,
	clusterName, kindName string,
) (api.ResourceKind, dynamic.Interface, error) {
	kind, err := api.ResourceKindFor(kindName)
	if err != nil {
		return api.ResourceKind{}, nil, fmt.Errorf("resolve resource kind: %w", err)
	}

	client, err := s.newDynamicClient(ctx, clusterName)
	if err != nil {
		return api.ResourceKind{}, nil, err
	}

	return kind, client, nil
}

// requireNamespace returns an ErrInvalid-wrapped error (→ 422) when a namespaced kind is addressed
// without a namespace, so single-resource write actions surface a clear message instead of an opaque
// API-server 500 from an empty-namespace request.
func requireNamespace(kind api.ResourceKind, ref api.ResourceRef) error {
	if kind.Namespaced && ref.Namespace == "" {
		return fmt.Errorf("%w: namespace is required for %q", api.ErrInvalid, ref.Kind)
	}

	return nil
}

// ListResources lists resources of the requested (allowlisted) kind from the named cluster. A
// namespaced kind with an empty query namespace lists across all namespaces.
func (s *Service) ListResources(
	ctx context.Context,
	_, name string,
	query api.ResourceQuery,
) (*unstructured.UnstructuredList, error) {
	kind, client, err := s.resolveKindAndClient(ctx, name, query.Kind)
	if err != nil {
		return nil, err
	}

	list, err := api.ListResourcesWith(ctx, client, kind, query)
	if err != nil {
		return nil, fmt.Errorf("read resources from cluster %q: %w", name, err)
	}

	return list, nil
}

// GetResource fetches a single resource of the requested (allowlisted) kind from the named cluster.
func (s *Service) GetResource(
	ctx context.Context,
	_, name string,
	ref api.ResourceRef,
) (*unstructured.Unstructured, error) {
	kind, client, err := s.resolveKindAndClient(ctx, name, ref.Kind)
	if err != nil {
		return nil, err
	}

	obj, err := api.GetResourceWith(ctx, client, kind, ref)
	if err != nil {
		return nil, fmt.Errorf("read resource from cluster %q: %w", name, err)
	}

	return obj, nil
}

// ScaleResource sets the replica count of a scalable workload via a merge patch on spec.replicas.
func (s *Service) ScaleResource(
	ctx context.Context,
	_, name string,
	ref api.ResourceRef,
	replicas int32,
) error {
	if !api.ResourceKindScalable(ref.Kind) {
		return fmt.Errorf("%w: %q is not scalable", api.ErrInvalid, ref.Kind)
	}

	if replicas < 0 {
		return fmt.Errorf("%w: replicas must be >= 0", api.ErrInvalid)
	}

	patch := fmt.Appendf(nil, `{"spec":{"replicas":%d}}`, replicas)

	return s.mergePatch(ctx, name, "scale", ref, patch)
}

// RestartResource triggers a rolling restart by stamping the pod template's restartedAt annotation —
// the same mechanism `kubectl rollout restart` uses. The stamp is nanosecond-resolution so two
// restarts issued within the same second still change the value and reliably roll the workload.
func (s *Service) RestartResource(ctx context.Context, _, name string, ref api.ResourceRef) error {
	if !api.ResourceKindRestartable(ref.Kind) {
		return fmt.Errorf("%w: %q does not support rollout restart", api.ErrInvalid, ref.Kind)
	}

	patch := fmt.Appendf(
		nil,
		`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`,
		time.Now().Format(time.RFC3339Nano),
	)

	return s.mergePatch(ctx, name, "restart", ref, patch)
}

// mergePatch resolves the kind + dynamic client for the named cluster, requires a namespace for the
// target, and applies a JSON merge patch. verb labels the error ("scale", "restart"). Shared by the
// scale/restart write actions so the resolve-and-patch boilerplate lives in one place.
func (s *Service) mergePatch(
	ctx context.Context,
	name, verb string,
	ref api.ResourceRef,
	patch []byte,
) error {
	kind, client, err := s.resolveKindAndClient(ctx, name, ref.Kind)
	if err != nil {
		return err
	}

	err = requireNamespace(kind, ref)
	if err != nil {
		return err
	}

	_, err = client.Resource(kind.GVR).Namespace(ref.Namespace).
		Patch(ctx, ref.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("%s %s %q: %w", verb, ref.Kind, ref.Name, err)
	}

	return nil
}

// ReconcileResource triggers an immediate GitOps reconcile by stamping the engine-specific
// annotation — the same mechanism `flux reconcile` / an ArgoCD refresh use. Flux watches
// reconcile.fluxcd.io/requestedAt (nanosecond stamp so repeats differ); ArgoCD watches
// argocd.argoproj.io/refresh.
func (s *Service) ReconcileResource(
	ctx context.Context,
	_, name string,
	ref api.ResourceRef,
) error {
	if !api.ResourceKindReconcilable(ref.Kind) {
		return fmt.Errorf("%w: %q does not support reconcile", api.ErrInvalid, ref.Kind)
	}

	key, value := reconcileAnnotation(ref.Kind)
	patch := fmt.Appendf(nil, `{"metadata":{"annotations":{%q:%q}}}`, key, value)

	return s.mergePatch(ctx, name, "reconcile", ref, patch)
}

// reconcileAnnotation returns the metadata annotation (key, value) that triggers a reconcile for the
// kind's GitOps engine.
func reconcileAnnotation(kind string) (string, string) {
	if kind == "Application" {
		return "argocd.argoproj.io/refresh", "normal"
	}

	return "reconcile.fluxcd.io/requestedAt", time.Now().Format(time.RFC3339Nano)
}

// DeleteResource deletes a namespaced allowlisted resource. Cluster-scoped kinds (Node, Namespace)
// are intentionally NOT deletable from the workload browser — those are high-blast-radius operations
// (a Namespace delete cascades to everything in it) better left to the CLI.
func (s *Service) DeleteResource(ctx context.Context, _, name string, ref api.ResourceRef) error {
	kind, client, err := s.resolveKindAndClient(ctx, name, ref.Kind)
	if err != nil {
		return err
	}

	if !kind.Namespaced {
		return fmt.Errorf(
			"%w: cluster-scoped %q cannot be deleted from the workload browser",
			api.ErrInvalid,
			ref.Kind,
		)
	}

	err = requireNamespace(kind, ref)
	if err != nil {
		return err
	}

	err = client.Resource(kind.GVR).Namespace(ref.Namespace).
		Delete(ctx, ref.Name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete %s %q: %w", ref.Kind, ref.Name, err)
	}

	return nil
}
