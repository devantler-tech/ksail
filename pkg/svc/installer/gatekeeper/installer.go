package gatekeeperinstaller

import (
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/helmutil"
)

// Installer installs or upgrades Gatekeeper.
//
// It embeds helmutil.Base to provide standard Helm chart lifecycle management.
type Installer struct {
	*helmutil.Base
}

// NewInstaller creates a new Gatekeeper installer instance.
func NewInstaller(client helm.Interface, timeout time.Duration) *Installer {
	return &Installer{
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
				RepoURL:         "https://open-policy-agent.github.io/gatekeeper/charts",
				CreateNamespace: true,
				Atomic:          true,
				Wait:            true,
				WaitForJobs:     true,
				Timeout:         timeout,
				SetValues: map[string]string{
					// Use Ignore so the validating webhook does not block API
					// requests when webhook pods are temporarily unreachable
					// (e.g. during CNI churn on freshly bootstrapped clusters).
					"webhook.failurePolicy": "Ignore",
				},
			},
		),
	}
}
