package kyvernoinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
)

// Installer installs or upgrades Kyverno.
//
// It embeds helmutil.Base to provide standard Helm chart lifecycle management
// and adds a post-install webhook readiness wait to ensure the admission
// controller is accepting requests before returning.
type Installer struct {
	*helmutil.Base

	kubeconfig string
	context    string
	timeout    time.Duration
}

// NewInstaller creates a new Kyverno installer instance.
// kubeconfig and context are used after Helm install to wait for the Kyverno
// admission webhook to be ready before returning.
func NewInstaller(client helm.Interface, timeout time.Duration, kubeconfig, context string) *Installer {
	return &Installer{
		Base: helmutil.NewBase(
			"kyverno",
			client,
			timeout,
			&helm.RepositoryEntry{
				Name: "kyverno",
				URL:  "https://kyverno.github.io/kyverno/",
			},
			&helm.ChartSpec{
				ReleaseName:     "kyverno",
				ChartName:       "kyverno/kyverno",
				Namespace:       "kyverno",
				Version:         chartVersion(),
				RepoURL:         "https://kyverno.github.io/kyverno/",
				CreateNamespace: true,
				Atomic:          true,
				Wait:            true,
				WaitForJobs:     true,
				Timeout:         timeout,
				SetValues: map[string]string{
					// Use Ignore so the admission webhook does not block
					// API requests when webhook pods are temporarily
					// unreachable (e.g. during bootstrap or CNI churn).
					// The chart default is Fail, which can cause cascading
					// failures for components installed in parallel.
					"admissionController.webhookServer.failurePolicy": "Ignore",
				},
			},
		),
		kubeconfig: kubeconfig,
		context:    context,
		timeout:    timeout,
	}
}

// Install installs or upgrades Kyverno via its Helm chart, then waits for the
// admission webhook to be ready before returning. This extra wait prevents the
// race condition where Helm reports install success but the webhook server has
// not yet populated its caBundle, causing transient admission failures for the
// first workload operations after cluster setup.
//
// A single overall deadline governs both the Helm install and the webhook
// readiness poll so the total wall time stays within timeout + buffer.
func (i *Installer) Install(ctx context.Context) error {
	overallCtx, cancel := context.WithTimeout(ctx, i.timeout+helm.ContextTimeoutBuffer)
	defer cancel()

	if err := i.Base.Install(overallCtx); err != nil {
		return err
	}

	if err := i.waitForWebhookReady(overallCtx); err != nil {
		return fmt.Errorf("kyverno webhook not ready after install: %w", err)
	}

	return nil
}
