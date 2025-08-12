package reconciliationtoolbootstrapper

import (
	"context"
	"os"
	"time"

	"github.com/devantler-tech/ksail/internal/utils"
	helmclient "github.com/mittwald/go-helm-client"
)

type FluxBootstrapper struct {
	kubeconfig string
	context    string
	timeout    time.Duration
}

func NewFluxBootstrapper(kubeconfig, context string, timeout time.Duration) *FluxBootstrapper {
	return &FluxBootstrapper{
		kubeconfig: kubeconfig,
		context:    context,
		timeout:    timeout,
	}
}

// Install installs or upgrades the Flux Operator via its OCI Helm chart.
func (b *FluxBootstrapper) Install() error {
	err := helmInstallOrUpgradeFluxOperator(b)
	if err != nil {
		return err
	}

  // TODO: Apply FluxInstance that syncs with local 'ksail-registry'
	return nil
}

// Uninstall removes the Helm release for the Flux Operator.
func (b *FluxBootstrapper) Uninstall() error {
	client, err := b.newHelmClient()
	if err != nil {
		return err
	}
	return client.UninstallReleaseByName("flux-operator")
}

// --- internals ---

func helmInstallOrUpgradeFluxOperator(b *FluxBootstrapper) error {
	client, err := b.newHelmClient()
	if err != nil {
		return err
	}

	spec := helmclient.ChartSpec{
		ReleaseName:     "flux-operator",
		ChartName:       "oci://ghcr.io/controlplaneio-fluxcd/charts/flux-operator",
		Namespace:       "flux-system",
		CreateNamespace: true,
		Atomic:          true,
		UpgradeCRDs:     true,
		Timeout:         b.timeout,
	}

	ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()
	_, err = client.InstallOrUpgradeChart(ctx, &spec, nil)
	return err
}

func (b *FluxBootstrapper) newHelmClient() (helmclient.Client, error) {
	kubeconfigPath, _ := utils.ExpandPath(b.kubeconfig)
	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	opts := &helmclient.KubeConfClientOptions{
		Options: &helmclient.Options{
			Namespace: "flux-system",
		},
		KubeConfig:  data,
		KubeContext: b.context,
	}
	return helmclient.NewClientFromKubeConf(opts)
}
