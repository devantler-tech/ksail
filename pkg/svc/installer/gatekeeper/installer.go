package gatekeeperinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
)

// Installer installs or upgrades Gatekeeper.
//
// It embeds helmutil.Base to provide standard Helm chart lifecycle management.
// After the Helm install succeeds it polls the ValidatingWebhookConfiguration
// until every webhook entry has a non-empty caBundle, ensuring Gatekeeper's
// admission webhook is fully initialised before workloads are deployed.
type Installer struct {
	*helmutil.Base

	kubeconfig  string
	kubeContext string
	timeout     time.Duration
}

// NewInstaller creates a new Gatekeeper installer instance.
//
// kubeconfig and kubeContext are required for the post-install webhook-readiness
// wait. Pass empty strings to disable webhook waiting (tests or environments
// without cluster access).
func NewInstaller(
	client helm.Interface,
	kubeconfig, kubeContext string,
	timeout time.Duration,
) *Installer {
	return &Installer{
		kubeconfig:  kubeconfig,
		kubeContext: kubeContext,
		timeout:     timeout,
		Base: helmutil.NewBase(
			"gatekeeper",
			client,
			timeout,
			&helm.RepositoryEntry{
				Name: "gatekeeper",
				URL:  "https://open-policy-agent.github.io/gatekeeper/charts",
			},
			&helm.ChartSpec{
				ReleaseName:     "gatekeeper",
				ChartName:       "gatekeeper/gatekeeper",
				Namespace:       "gatekeeper-system",
				Version:         chartVersion(),
				RepoURL:         "https://open-policy-agent.github.io/gatekeeper/charts",
				CreateNamespace: true,
				Atomic:          true,
				Wait:            true,
				WaitForJobs:     true,
				UpgradeCRDs:     true,
				Timeout:         timeout,
				SetValues: map[string]string{
					// Disable the chart's CRD upgrade hooks. The hooks create
					// a ServiceAccount that Helm v4's kstatus watcher cannot
					// assess (ServiceAccounts have no .status field), causing
					// the install to fail with "status: Unknown". CRDs are
					// still managed natively by Helm via UpgradeCRDs above.
					"upgradeCRDs.enabled": "false",
					// Use Ignore so webhooks do not block API requests when
					// webhook pods are temporarily unreachable (e.g. during
					// CNI churn on freshly bootstrapped clusters). Both
					// validating and mutating policies must be set explicitly
					// to avoid intermittent kstatus watch timeouts in
					// components installed in parallel (e.g. ArgoCD).
					"webhook.failurePolicy":         "Ignore",
					"mutatingWebhook.failurePolicy": "Ignore",
				},
			},
		),
	}
}

// Install runs the Helm chart install and then waits for Gatekeeper's
// ValidatingWebhookConfiguration to have all caBundle fields populated.
//
// Helm reports success once the pods are Running/Ready, but the Gatekeeper
// cert-controller injects the caBundle asynchronously. Any workload pod
// created before the caBundle is set may experience a readiness-probe
// context-cancellation error because the API server forwards the admission
// request to an endpoint that has not yet completed its TLS handshake setup.
//
// If kubeconfig is empty (e.g. in unit tests), the webhook-readiness wait is
// skipped and only the Helm install runs.
func (g *Installer) Install(ctx context.Context) error {
	// Wrap the entire Install in a single deadline so that the Helm install and
	// the webhook readiness wait share one budget rather than each receiving a
	// full g.timeout, which would allow the total to reach ~2×g.timeout.
	//
	// Include Helm's timeout buffer in the outer deadline so Base.Install can
	// retain its own child-context slack for post-apply readiness observation.
	if g.timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, g.timeout+helm.ContextTimeoutBuffer)
		defer cancel()
	}

	err := g.Base.Install(ctx)
	if err != nil {
		return fmt.Errorf("install gatekeeper: %w", err)
	}

	if g.kubeconfig == "" {
		return nil
	}

	// deadline=0: the poll is bounded solely by the ctx timeout set above.
	err = waitForWebhookReadyFn(ctx, g.kubeconfig, g.kubeContext, 0)
	if err != nil {
		return fmt.Errorf("wait for gatekeeper webhook readiness: %w", err)
	}

	return nil
}
