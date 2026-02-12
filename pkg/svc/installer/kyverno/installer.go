package kyvernoinstaller

import (
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/helmutil"
)

// Installer installs or upgrades Kyverno.
//
// It embeds helmutil.Base to provide standard Helm chart lifecycle management.
type Installer struct {
	*helmutil.Base
}

// NewInstaller creates a new Kyverno installer instance.
func NewInstaller(client helm.Interface, timeout time.Duration) *Installer {
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
	}
}
