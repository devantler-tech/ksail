package argocdinstaller

import (
	"context"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// ExtractAgeKey exports extractAgeKey for testing.
func ExtractAgeKey(input string) string {
	return extractAgeKey(input)
}

// ResolveAgeKey exports resolveAgeKey for testing.
func ResolveAgeKey(sops v1alpha1.SOPS) (string, error) {
	return resolveAgeKey(sops)
}

// BuildSopsAgeSecret exports buildSopsAgeSecretObj for testing.
func BuildSopsAgeSecret(ageKey string) *corev1.Secret {
	return buildSopsAgeSecretObj(ageKey)
}

// UpsertSopsAgeSecret exports upsertSopsAgeSecret for testing with a fake clientset.
func UpsertSopsAgeSecret(ctx context.Context, clientset kubernetes.Interface, ageKey string) error {
	return upsertSopsAgeSecret(ctx, clientset, ageKey)
}
