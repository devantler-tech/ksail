package argocdinstaller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"k8s.io/client-go/kubernetes"
)

var errNilContext = errors.New("context is nil")

// EnsureDefaultResources waits for the core Argo CD components to become ready.
func EnsureDefaultResources(ctx context.Context, kubeconfig string, timeout time.Duration) error {
	if ctx == nil {
		return errNilContext
	}

	restConfig, err := k8s.BuildRESTConfig(kubeconfig, "")
	if err != nil {
		return fmt.Errorf("build rest config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create kubernetes client: %w", err)
	}

	checks := []k8s.ReadinessCheck{
		{Type: "deployment", Namespace: argoCDNamespace, Name: "argocd-server"},
		{Type: "deployment", Namespace: argoCDNamespace, Name: "argocd-repo-server"},
	}

	err = k8s.WaitForMultipleResources(ctx, clientset, checks, timeout)
	if err != nil {
		return fmt.Errorf("wait for Argo CD components: %w", err)
	}

	return nil
}
