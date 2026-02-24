package certmanagerinstaller

import (
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/helmutil"
)

// Installer installs or upgrades cert-manager.
//
// It embeds helmutil.Base to provide standard Helm chart lifecycle management.
type Installer struct {
	*helmutil.Base
}

// NewInstaller creates a new cert-manager installer instance.
func NewInstaller(client helm.Interface, timeout time.Duration) *Installer {
	return &Installer{
		Base: helmutil.NewBase(
			"cert-manager",
			client,
			timeout,
			&helm.RepositoryEntry{
				Name: "jetstack",
				URL:  "https://charts.jetstack.io",
			},
			&helm.ChartSpec{
				ReleaseName:     "cert-manager",
				ChartName:       "jetstack/cert-manager",
				Namespace:       "cert-manager",
				Version:         chartVersion(),
				RepoURL:         "https://charts.jetstack.io",
				CreateNamespace: true,
				Atomic:          true,
				Wait:            true,
				WaitForJobs:     true,
				Timeout:         timeout,
				SetValues: map[string]string{
					"installCRDs":             "true",
					"startupapicheck.timeout": "5m",
				},
			},
		),
	}
}
