package nested

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

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
			secret, err := clientset.CoreV1().Secrets(namespace).Get(
				ctx, secretName, metav1.GetOptions{},
			)
			if apierrors.IsNotFound(err) {
				return false, nil
			}

			if err != nil {
				return false, fmt.Errorf("get kubeconfig secret: %w", err)
			}

			data, ok := secret.Data[key]
			if !ok || len(data) == 0 {
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
