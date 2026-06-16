package controller

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// cleanupNamespace deletes the Cluster's namespace when the operator created it (it carries the
// managed-namespace label) and it no longer holds any other Cluster resources or user workloads.
// It is best-effort: any failure is logged and ignored so it never blocks deletion. Namespaces the
// operator did not create (e.g. "default" or user namespaces) are never touched.
func (r *ClusterReconciler) cleanupNamespace(ctx context.Context, cluster *v1alpha1.Cluster) {
	log := logf.FromContext(ctx)

	name := cluster.Namespace
	if name == "" {
		return
	}

	reader := r.reader()

	var namespace corev1.Namespace

	getErr := reader.Get(ctx, client.ObjectKey{Name: name}, &namespace)
	if getErr != nil {
		return
	}

	if namespace.Labels[v1alpha1.ManagedNamespaceLabel] != "true" ||
		!namespace.DeletionTimestamp.IsZero() {
		return
	}

	empty, err := r.namespaceHasOnlyOperatorResources(ctx, name, cluster.Name)
	if err != nil {
		log.Info(
			"namespace cleanup check failed (best-effort)",
			"namespace",
			name,
			"error",
			err.Error(),
		)

		return
	}

	if !empty {
		return
	}

	delErr := r.Delete(ctx, &namespace)
	if delErr != nil && !apierrors.IsNotFound(delErr) {
		log.Info(
			"delete operator-managed namespace (best-effort)",
			"namespace",
			name,
			"error",
			delErr.Error(),
		)

		return
	}

	log.Info("deleted operator-managed namespace", "namespace", name)
}

// rootCAConfigMapName is the ConfigMap Kubernetes injects into every namespace; it is ignored when
// deciding whether a managed namespace still holds user data.
const rootCAConfigMapName = "kube-root-ca.crt"

// namespaceHasOnlyOperatorResources reports whether the namespace contains nothing other than the
// Cluster being deleted: no other live Cluster resources, no user workloads or batch jobs, and no
// user-authored ConfigMaps/Secrets. It is conservative — any such resource keeps the namespace —
// so the operator never deletes a namespace that still holds user data.
func (r *ClusterReconciler) namespaceHasOnlyOperatorResources(
	ctx context.Context,
	namespace, excludeCluster string,
) (bool, error) {
	reader := r.reader()

	var clusters v1alpha1.ClusterList

	listErr := reader.List(ctx, &clusters, client.InNamespace(namespace))
	if listErr != nil {
		return false, fmt.Errorf("list clusters in %q: %w", namespace, listErr)
	}

	for index := range clusters.Items {
		other := &clusters.Items[index]
		// Ignore the cluster being deleted and any other clusters already terminating.
		if other.Name == excludeCluster || !other.DeletionTimestamp.IsZero() {
			continue
		}

		return false, nil
	}

	// Keep the namespace if it holds any workload, batch job, networking object, or RBAC object. The
	// list is bounded; one item is enough to know the namespace is in use. ServiceAccounts and
	// ConfigMaps/Secrets need filtering (Kubernetes auto-creates some) and are handled separately.
	presenceLists := []client.ObjectList{
		&corev1.PodList{},
		&corev1.ServiceList{},
		&corev1.PersistentVolumeClaimList{},
		&appsv1.DeploymentList{},
		&appsv1.ReplicaSetList{},
		&appsv1.StatefulSetList{},
		&appsv1.DaemonSetList{},
		&batchv1.JobList{},
		&batchv1.CronJobList{},
		&networkingv1.IngressList{},
		&networkingv1.NetworkPolicyList{},
		&rbacv1.RoleList{},
		&rbacv1.RoleBindingList{},
	}

	for _, list := range presenceLists {
		err := reader.List(ctx, list, client.InNamespace(namespace), client.Limit(1))
		if err != nil {
			return false, fmt.Errorf("list %T in %q: %w", list, namespace, err)
		}

		items, err := apimeta.ExtractList(list)
		if err != nil {
			return false, fmt.Errorf("extract %T: %w", list, err)
		}

		if len(items) > 0 {
			return false, nil
		}
	}

	return r.namespaceHasNoUserConfig(ctx, namespace)
}

// defaultServiceAccountName is the ServiceAccount Kubernetes provisions in every namespace; it is
// ignored when deciding whether a managed namespace still holds user resources.
const defaultServiceAccountName = "default"

// namespaceHasNoUserConfig reports whether the namespace holds no user-authored ConfigMaps,
// Secrets, or ServiceAccounts, ignoring the objects Kubernetes provisions in every namespace (the
// kube-root-ca.crt ConfigMap, the default ServiceAccount, and service-account token Secrets). These
// need filtering rather than a presence check, so they are listed in full (a namespace's set is
// small).
func (r *ClusterReconciler) namespaceHasNoUserConfig(
	ctx context.Context,
	namespace string,
) (bool, error) {
	reader := r.reader()

	var configMaps corev1.ConfigMapList

	cmErr := reader.List(ctx, &configMaps, client.InNamespace(namespace))
	if cmErr != nil {
		return false, fmt.Errorf("list configmaps in %q: %w", namespace, cmErr)
	}

	for index := range configMaps.Items {
		if configMaps.Items[index].Name != rootCAConfigMapName {
			return false, nil
		}
	}

	var secrets corev1.SecretList

	secretErr := reader.List(ctx, &secrets, client.InNamespace(namespace))
	if secretErr != nil {
		return false, fmt.Errorf("list secrets in %q: %w", namespace, secretErr)
	}

	for index := range secrets.Items {
		if secrets.Items[index].Type != corev1.SecretTypeServiceAccountToken {
			return false, nil
		}
	}

	var serviceAccounts corev1.ServiceAccountList

	saErr := reader.List(ctx, &serviceAccounts, client.InNamespace(namespace))
	if saErr != nil {
		return false, fmt.Errorf("list serviceaccounts in %q: %w", namespace, saErr)
	}

	for index := range serviceAccounts.Items {
		if serviceAccounts.Items[index].Name != defaultServiceAccountName {
			return false, nil
		}
	}

	return true, nil
}
