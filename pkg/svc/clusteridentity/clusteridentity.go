// Package clusteridentity writes the marker KSail leaves in every cluster it provisions:
// the kube-system/ksail-cluster-info ConfigMap. It records which cluster KSail provisioned
// (name, distribution and provider), the KSail version that wrote it, and the UID of the
// kube-system namespace it was written against, so a marker copied or restored into another
// cluster can be told apart from the original (ksail#7191, epic ksail#7190).
package clusteridentity

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// Namespace is where the marker lives.
	Namespace = metav1.NamespaceSystem
	// Name is the marker ConfigMap's name.
	Name = "ksail-cluster-info"

	// KeyClusterName holds the cluster name KSail provisioned.
	KeyClusterName = "clusterName"
	// KeyDistribution holds the Kubernetes distribution KSail provisioned.
	KeyDistribution = "distribution"
	// KeyProvider holds the infrastructure provider KSail provisioned on.
	KeyProvider = "provider"
	// KeyKSailVersion holds the version of KSail that last wrote the marker.
	KeyKSailVersion = "ksailVersion"
	// KeyKubeSystemUID holds the UID of the kube-system namespace the marker was written
	// against. A marker whose UID differs from the live namespace was copied or restored.
	KeyKubeSystemUID = "kubeSystemUID"

	managedByLabel = "app.kubernetes.io/managed-by"
	managedByValue = "ksail"
)

// ErrNoKubeSystemUID is returned when the kube-system namespace reports no UID, so the
// marker could not be bound to this cluster.
var ErrNoKubeSystemUID = errors.New("the kube-system namespace has no UID")

// Marker is the identity KSail provisioned a cluster with.
type Marker struct {
	ClusterName  string
	Distribution string
	Provider     string
	KSailVersion string
}

// Write creates the marker, or replaces every field of an existing one, bound to the live
// kube-system namespace UID. Replacing the whole data map means a stale or copied field never
// survives a refresh.
func Write(ctx context.Context, client kubernetes.Interface, marker Marker) error {
	namespace, err := client.CoreV1().Namespaces().Get(ctx, Namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read the %s namespace: %w", Namespace, err)
	}

	if namespace.UID == "" {
		return ErrNoKubeSystemUID
	}

	data := map[string]string{
		KeyClusterName:   marker.ClusterName,
		KeyDistribution:  marker.Distribution,
		KeyProvider:      marker.Provider,
		KeyKSailVersion:  marker.KSailVersion,
		KeyKubeSystemUID: string(namespace.UID),
	}

	configMaps := client.CoreV1().ConfigMaps(Namespace)

	existing, err := configMaps.Get(ctx, Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = configMaps.Create(ctx, &corev1.ConfigMap{
			Name:      Name,
			Namespace: Namespace,
			Labels:    map[string]string{managedByLabel: managedByValue},
			Data:      data,
		}, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create the cluster identity marker: %w", err)
		}

		return nil
	}

	if err != nil {
		return fmt.Errorf("read the cluster identity marker: %w", err)
	}

	updated := existing.DeepCopy()
	updated.Data = data

	if updated.Labels == nil {
		updated.Labels = map[string]string{}
	}

	updated.Labels[managedByLabel] = managedByValue

	_, err = configMaps.Update(ctx, updated, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("update the cluster identity marker: %w", err)
	}

	return nil
}
