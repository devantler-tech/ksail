package certmanagerinstaller

import (
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
)

// Installer installs or upgrades cert-manager.
//
// It embeds helmutil.Base to provide standard Helm chart lifecycle management.
type Installer struct {
	*helmutil.Base
}

// NewInstaller creates a new cert-manager installer instance.
// When haEnabled is true the chart is configured with HA defaults
// (replicas, PDB, topology spread) for the controller and webhook deployments.
func NewInstaller(client helm.Interface, timeout time.Duration, haEnabled bool) *Installer {
	setValues := map[string]string{
		"installCRDs":             "true",
		"startupapicheck.timeout": startupAPICheckTimeout(timeout),
	}

	var valuesYaml string

	if haEnabled {
		setValues["replicaCount"] = "2"
		setValues["webhook.replicaCount"] = "2"
		setValues["podDisruptionBudget.enabled"] = "true"
		setValues["podDisruptionBudget.minAvailable"] = "1"
		setValues["webhook.podDisruptionBudget.enabled"] = "true"
		setValues["webhook.podDisruptionBudget.minAvailable"] = "1"

		valuesYaml = `topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: kubernetes.io/hostname
    whenUnsatisfiable: ScheduleAnyway
    labelSelector:
      matchLabels:
        app.kubernetes.io/component: controller
webhook:
  topologySpreadConstraints:
    - maxSkew: 1
      topologyKey: kubernetes.io/hostname
      whenUnsatisfiable: ScheduleAnyway
      labelSelector:
        matchLabels:
          app.kubernetes.io/component: webhook`
	}

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
				SetValues:       setValues,
				ValuesYaml:      valuesYaml,
			},
		),
	}
}

// startupAPICheckTimeout returns the startupapicheck timeout as a duration string
// that scales with the overall install timeout. On resource-constrained runners
// (e.g., Talos on GitHub Actions), the webhook certificate can take longer to
// provision, so the startup check needs proportionally more time.
// See: https://github.com/devantler-tech/ksail/issues/4040
func startupAPICheckTimeout(installTimeout time.Duration) string {
	const minStartupCheckTimeout = 5 * time.Minute

	checkTimeout := max(installTimeout, minStartupCheckTimeout)

	return checkTimeout.String()
}
