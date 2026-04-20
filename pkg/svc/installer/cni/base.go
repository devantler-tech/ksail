package cni

import (
	"context"
	"errors"
	"fmt"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// API server stability configuration for distributions that need it.
const (
	// APIServerStabilityTimeout is the timeout for waiting for API server stability.
	APIServerStabilityTimeout = 60 * time.Second
	// APIServerRequiredSuccesses is the number of consecutive successful API server
	// responses required before considering it stable.
	APIServerRequiredSuccesses = 3
)

// InstallerBase provides common fields and methods for CNI installers.
// It encapsulates shared functionality like Helm client management, kubeconfig handling,
// and timeout management. CNI implementations should embed this type as a pointer
// (*cni.InstallerBase) to inherit these capabilities.
//
// Note: Helm chart installation utilities have been moved to pkg/client/helm package
// (helm.InstallOrUpgradeChart, helm.RepoConfig, helm.ChartConfig). With Helm v4 kstatus
// wait enabled, all resource readiness checking is handled by Helm's StatusWatcher during
// installation, eliminating the need for custom wait functions.
//
// Example usage:
//
//	type MyCNIInstaller struct {
//	    *cni.InstallerBase  // Must be embedded as a pointer
//	}
//
//	installer := &MyCNIInstaller{}
//	installer.InstallerBase = cni.NewInstallerBase(
//	    helmClient, kubeconfig, context, timeout,
//	)
type InstallerBase struct {
	kubeconfig string
	context    string
	timeout    time.Duration
	client     helm.Interface
}

// NewInstallerBase creates a new base installer instance with the provided configuration.
func NewInstallerBase(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
) *InstallerBase {
	return &InstallerBase{
		client:     client,
		kubeconfig: kubeconfig,
		context:    context,
		timeout:    timeout,
	}
}

// WaitForReadiness is a no-op since Helm v4 kstatus wait (Wait: true) already
// ensures all resources are fully reconciled during installation.
// This method exists for interface compatibility with legacy code.
func (b *InstallerBase) WaitForReadiness(_ context.Context) error {
	return nil
}

// BuildRESTConfig builds a Kubernetes REST configuration.
func (b *InstallerBase) BuildRESTConfig() (*rest.Config, error) {
	config, err := k8s.BuildRESTConfig(b.kubeconfig, b.context)
	if err != nil {
		return nil, fmt.Errorf("build REST config: %w", err)
	}

	return config, nil
}

// errAPIServerCheckerNil is returned when the API server checker is not configured.
var errAPIServerCheckerNil = errors.New("api server checker is not configured")

// PrepareInstall validates the Helm client and runs the API server stability
// check for distributions that require it (Talos and K3s). It consolidates
// the common Install() preamble shared by all CNI installers.
func (b *InstallerBase) PrepareInstall(
	ctx context.Context, dist v1alpha1.Distribution, checker func(ctx context.Context) error,
) error {
	_, err := b.GetClient()
	if err != nil {
		return fmt.Errorf("get helm client: %w", err)
	}

	needsCheck := dist == v1alpha1.DistributionTalos || dist == v1alpha1.DistributionK3s

	err = b.RunAPIServerCheck(ctx, needsCheck, checker)
	if err != nil {
		return fmt.Errorf("run API server check: %w", err)
	}

	return nil
}

// RunAPIServerCheck calls checker if shouldCheck is true. It returns a clear
// error when checker is nil to prevent panics. This is intended to be called
// from CNI Install() methods that share the same stability-check pattern.
func (b *InstallerBase) RunAPIServerCheck(
	ctx context.Context, shouldCheck bool, checker func(ctx context.Context) error,
) error {
	if !shouldCheck {
		return nil
	}

	if checker == nil {
		return errAPIServerCheckerNil
	}

	err := checker(ctx)
	if err != nil {
		return fmt.Errorf("failed to wait for API server stability: %w", err)
	}

	return nil
}

// WaitForAPIServerStability waits for the Kubernetes API server to be stable.
// This is needed for distributions like Talos and K3s where the API server may
// be unstable immediately after bootstrap, causing transient connection errors.
// This method should be called before Helm operations for such distributions.
func (b *InstallerBase) WaitForAPIServerStability(ctx context.Context) error {
	restConfig, err := b.BuildRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to build REST config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	err = readiness.WaitForAPIServerStable(
		ctx,
		clientset,
		APIServerStabilityTimeout,
		APIServerRequiredSuccesses,
	)
	if err != nil {
		return fmt.Errorf("API server stability check failed: %w", err)
	}

	return nil
}

var errHelmClientNil = errors.New("helm client is nil")

// GetClient returns the Helm client.
func (b *InstallerBase) GetClient() (helm.Interface, error) {
	if b.client == nil {
		return nil, errHelmClientNil
	}

	return b.client, nil
}

// GetTimeout returns the timeout duration.
func (b *InstallerBase) GetTimeout() time.Duration {
	return b.timeout
}

// GetKubeconfig returns the kubeconfig path.
func (b *InstallerBase) GetKubeconfig() string {
	return b.kubeconfig
}

// GetContext returns the kubeconfig context.
func (b *InstallerBase) GetContext() string {
	return b.context
}

// ImagesFromChart templates the given ChartSpec and extracts container images.
// This provides a common implementation for CNI installers' Images() method.
func (b *InstallerBase) ImagesFromChart(
	ctx context.Context,
	spec *helm.ChartSpec,
) ([]string, error) {
	client, err := b.GetClient()
	if err != nil {
		return nil, fmt.Errorf("get helm client: %w", err)
	}

	images, err := helmutil.ImagesFromChart(ctx, client, spec)
	if err != nil {
		return nil, fmt.Errorf("images from chart %s: %w", spec.ChartName, err)
	}

	return images, nil
}
