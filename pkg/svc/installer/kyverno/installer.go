package kyvernoinstaller

import (
	"context"
	"errors"
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

	kubeconfig  string
	kubecontext string
	timeout     time.Duration
}

// NewInstaller creates a new Kyverno installer instance.
// kubeconfig and kubecontext are used after Helm install to wait for the Kyverno
// admission webhook to be ready before returning.
// When haEnabled is true the chart is configured with HA defaults
// (replicas, PDB, topology spread) for the admission controller.
func NewInstaller(
	client helm.Interface,
	timeout time.Duration,
	kubeconfig, kubecontext string,
	haEnabled bool,
) *Installer {
	setValues := map[string]string{
		// Use Ignore so the admission webhook does not block
		// API requests when webhook pods are temporarily
		// unreachable (e.g. during bootstrap or CNI churn).
		// The chart default is Fail, which can cause cascading
		// failures for components installed in parallel.
		"admissionController.webhookServer.failurePolicy": "Ignore",
	}

	var valuesYaml string

	if haEnabled {
		setValues["admissionController.replicas"] = "2"
		setValues["admissionController.podDisruptionBudget.enabled"] = "true"
		setValues["admissionController.podDisruptionBudget.minAvailable"] = "1"

		valuesYaml = `admissionController:
  topologySpreadConstraints:
    - maxSkew: 1
      topologyKey: kubernetes.io/hostname
      whenUnsatisfiable: ScheduleAnyway
      labelSelector:
        matchLabels:
          app.kubernetes.io/component: admission-controller`
	}

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
				SetValues:       setValues,
				ValuesYaml:      valuesYaml,
			},
		),
		kubeconfig:  kubeconfig,
		kubecontext: kubecontext,
		timeout:     timeout,
	}
}

// Install installs or upgrades Kyverno via its Helm chart, then performs a
// best-effort wait for the admission webhook caBundle to be populated.
//
// The wait is best-effort: if the caBundle is not populated within
// webhookReadinessTimeout, Install returns success rather than an error.
// This is safe because the webhook is configured with failurePolicy: Ignore,
// which means API operations succeed even when the webhook is not yet fully
// initialised. In some environments (e.g. Cilium + cert-manager CI) the
// Kyverno certmanager-controller informer cache sync takes longer than any
// practical timeout, so treating the timeout as non-fatal avoids permanent
// CI failures while still confirming readiness when the environment is fast.
//
// Base.Install manages its own context deadline (timeout + buffer) internally,
// so ctx is passed through as-is. The webhook readiness poll derives from ctx
// so it remains cancellable by the caller while still being capped at
// webhookReadinessTimeout.
func (i *Installer) Install(ctx context.Context) error {
	err := i.Base.Install(ctx)
	if err != nil {
		return fmt.Errorf("installing kyverno base chart: %w", err)
	}

	webhookCtx, webhookCancel := context.WithTimeout(ctx, webhookReadinessTimeout)
	defer webhookCancel()

	err = i.waitForWebhookReady(webhookCtx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) &&
		!errors.Is(err, errNoTimeRemaining) {
		return fmt.Errorf("kyverno webhook not ready after install: %w", err)
	}

	return nil
}
