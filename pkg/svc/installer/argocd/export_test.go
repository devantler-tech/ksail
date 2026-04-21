package argocdinstaller

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/sopsutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// ExtractAgeKey exports sopsutil.ExtractAgeKey for testing.
func ExtractAgeKey(input string) string {
	return sopsutil.ExtractAgeKey(input)
}

// ResolveAgeKey exports sopsutil.ResolveAgeKey for testing.
func ResolveAgeKey(sops v1alpha1.SOPS) (string, error) {
	key, err := sopsutil.ResolveAgeKey(sops)
	if err != nil {
		return "", fmt.Errorf("resolve age key: %w", err)
	}

	return key, nil
}

// BuildSopsAgeSecret exports sopsutil.BuildSopsAgeSecret for testing with the argocd namespace.
func BuildSopsAgeSecret(ageKey string) *corev1.Secret {
	return sopsutil.BuildSopsAgeSecret(argoCDNamespace, ageKey)
}

// UpsertSopsAgeSecret exports upsertSopsAgeSecret for testing with a fake clientset.
func UpsertSopsAgeSecret(ctx context.Context, clientset kubernetes.Interface, ageKey string) error {
	return upsertSopsAgeSecret(ctx, clientset, ageKey)
}

// BuildSOPSValuesYaml exports buildSOPSValuesYaml for testing.
func BuildSOPSValuesYaml() string {
	return buildSOPSValuesYaml()
}

// ChartSpec exports chartSpec for testing.
func (a *Installer) ChartSpec() *helm.ChartSpec {
	return a.chartSpec()
}
