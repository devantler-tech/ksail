package bootstrapper

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
	// Namespace to install the release into. Default: "flux-system"
	Namespace string
	// ReleaseName for the Helm release. Default: "flux-operator"
	ReleaseName string
	// Version of the chart to install (SemVer / OCI tag). Empty means latest.
	Version string
}

func NewFluxOperatorBootstrapper() *FluxOperatorBootstrapper {
	return &FluxOperatorBootstrapper{}
}

// Install installs or upgrades the Flux Operator via its OCI Helm chart.
func (b *FluxOperatorBootstrapper) Install() error {
	if b.ReleaseName == "" {
		b.ReleaseName = "flux-operator"
	}
	if b.Namespace == "" {
		b.Namespace = "flux-system"
	}
	client, err := b.newHelmClient()
	if err != nil {
		return err
	}

	spec := helmclient.ChartSpec{
		ReleaseName:     b.ReleaseName,
		ChartName:       "oci://ghcr.io/controlplaneio-fluxcd/charts/flux-operator",
		Namespace:       b.Namespace,
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
	if b.ReleaseName == "" {
		b.ReleaseName = "flux-operator"
	}
	if b.Namespace == "" {
		b.Namespace = "flux-system"
	}
	client, err := b.newHelmClient()
	if err != nil {
		return err
	}
	return client.UninstallReleaseByName(b.ReleaseName)
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
			Namespace: b.Namespace,
			Debug:     false,
		},
		KubeConfig:  data,
		KubeContext: b.KubeContext,
	}
	return helmclient.NewClientFromKubeConf(opts)
}
