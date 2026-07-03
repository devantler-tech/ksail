package nested

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

// FetchKubeconfigSecret reads the kubeconfig bytes published under key in the named Secret once
// (no polling — the Connector capability's read path). A not-yet-created Secret (NotFound) and an
// empty/absent key value are reported as clustererr.ErrKubeconfigNotReady so operator callers can
// retry; any other API error is wrapped with the Secret's coordinates.
//
// This is the shared fetch the K3d (k3k operator) and VCluster (vc-<name>) Connector
// implementations use; the per-distribution secret name and data key are passed in by the caller.
func FetchKubeconfigSecret(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, secretName, key string,
) ([]byte, error) {
	raw, err := fetchSecretData(ctx, clientset, namespace, secretName, key)
	if err != nil {
		return nil, err
	}

	if len(raw) == 0 {
		return nil, clustererr.ErrKubeconfigNotReady
	}

	return raw, nil
}

// ConnectorKubeconfig is the shared Connector read path: it validates the
// resolved cluster name and host clientset, then fetches the published
// kubeconfig Secret. Callers resolve the per-distribution connection
// coordinates (namespace/secret/key) and wrap errors with their
// distribution label.
func ConnectorKubeconfig(
	ctx context.Context,
	clientset kubernetes.Interface,
	clusterName, namespace, secretName, key string,
) ([]byte, error) {
	if clusterName == "" {
		return nil, fmt.Errorf("%w: cluster name not set", clustererr.ErrConfigNil)
	}

	if clientset == nil {
		return nil, fmt.Errorf("%w: host clientset not set", clustererr.ErrConfigNil)
	}

	return FetchKubeconfigSecret(ctx, clientset, namespace, secretName, key)
}

// UpsertSecret creates secret, or — when it already exists — re-reads the live
// object and updates that in place so the write carries the current
// resourceVersion (an Update built from a fresh object would be rejected by
// the API server's optimistic-concurrency check). Get/Update conflicts with
// concurrent writers are retried.
func UpsertSecret(
	ctx context.Context,
	clientset kubernetes.Interface,
	secret *corev1.Secret,
) error {
	_, err := clientset.CoreV1().Secrets(secret.Namespace).
		Create(ctx, secret, metav1.CreateOptions{})
	if err == nil {
		return nil
	}

	if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create secret %s/%s: %w", secret.Namespace, secret.Name, err)
	}

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, getErr := clientset.CoreV1().Secrets(secret.Namespace).
			Get(ctx, secret.Name, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("get secret %s/%s: %w", secret.Namespace, secret.Name, getErr)
		}

		existing.Labels = secret.Labels
		existing.Data = secret.Data

		_, updateErr := clientset.CoreV1().Secrets(secret.Namespace).
			Update(ctx, existing, metav1.UpdateOptions{})
		if updateErr != nil {
			return fmt.Errorf("update secret %s/%s: %w", secret.Namespace, secret.Name, updateErr)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("upsert secret %s/%s: %w", secret.Namespace, secret.Name, err)
	}

	return nil
}

// fetchSecretData is the single Get+NotFound+Data[key] step shared by
// FetchKubeconfigSecret and WaitForKubeconfigSecret. A missing Secret or an
// absent key yields (nil, nil) — the callers decide whether that means
// "not ready" or "keep waiting"; any other API error is wrapped with the
// Secret's coordinates.
func fetchSecretData(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, secretName, key string,
) ([]byte, error) {
	secret, err := clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("get kubeconfig secret %s/%s: %w", namespace, secretName, err)
	}

	return secret.Data[key], nil
}

// NamespaceExists reports whether namespace exists on the host cluster. A
// NotFound result is reported as (false, nil); any other API error is wrapped and
// returned. Operator-style nested provisioners (K3d, VCluster) use this as the
// existence check because their child cluster is a namespace of host workloads
// rather than DinD node pods.
func NamespaceExists(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace string,
) (bool, error) {
	_, err := clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("check namespace %s: %w", namespace, err)
	}

	return true, nil
}

// WaitForKubeconfigSecret polls the named Secret in namespace until it carries
// non-empty data under key, then returns that data. A not-yet-created Secret
// (NotFound) and an empty/absent key value are treated as "keep waiting"; any
// other API error aborts the poll.
//
// This is the shared poll loop the K3d (k3k operator) and VCluster (vc-<name>)
// Kubernetes provisioners previously hand-rolled; the per-distribution secret
// name, data key, poll interval, and timeout are passed in by the caller.
func WaitForKubeconfigSecret(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, secretName, key string,
	interval, timeout time.Duration,
) ([]byte, error) {
	var kubeconfigData []byte

	err := wait.PollUntilContextTimeout(
		ctx, interval, timeout, true,
		func(ctx context.Context) (bool, error) {
			data, err := fetchSecretData(ctx, clientset, namespace, secretName, key)
			if err != nil {
				return false, err
			}

			if len(data) == 0 {
				return false, nil
			}

			kubeconfigData = data

			return true, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("wait for kubeconfig secret: %w", err)
	}

	return kubeconfigData, nil
}
