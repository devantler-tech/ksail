package reconboot

import (
	"context"
	"fmt"
	"os"
	"time"

	helmclient "github.com/mittwald/go-helm-client"
)

type FluxOperatorBootstrapper struct {
	// KubeconfigPath is the path to the kubeconfig file. If empty, falls back to $KUBECONFIG or ~/.kube/config
	KubeconfigPath string
	// KubeContext selects a specific context in the kubeconfig (optional)
	KubeContext string
}

func NewFluxOperatorBootstrapper(kubeconfigPath string, kubeContext string) *FluxOperatorBootstrapper {
	return &FluxOperatorBootstrapper{
		KubeconfigPath: kubeconfigPath,
		KubeContext:    kubeContext,
	}
}

// Install installs or upgrades the Flux Operator via its OCI Helm chart.
func (b *FluxOperatorBootstrapper) Install() error {
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
	}

	// No custom values for now; install with chart defaults
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_, err = client.InstallOrUpgradeChart(ctx, &spec, nil)
	return err
}

// Uninstall removes the Helm release for the Flux Operator.
func (b *FluxOperatorBootstrapper) Uninstall() error {
	client, err := b.newHelmClient()
	if err != nil {
		return err
	}
	return client.UninstallReleaseByName("flux-operator")
}

// --- internals ---

func (b *FluxOperatorBootstrapper) newHelmClient() (helmclient.Client, error) {
	if _, err := os.Stat(b.KubeconfigPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("kubeconfig file does not exist: %s", b.KubeconfigPath)
	}

	data, err := os.ReadFile(b.KubeconfigPath)
	if err != nil {
		return nil, err
	}
	opts := &helmclient.KubeConfClientOptions{
		Options: &helmclient.Options{
			Namespace: "flux-system",
			Debug:     true,
		},
		KubeConfig:  data,
		KubeContext: b.KubeContext,
	}
	return helmclient.NewClientFromKubeConf(opts)
}
