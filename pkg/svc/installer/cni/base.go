package cni

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail-go/pkg/client/helm"
	"github.com/devantler-tech/ksail-go/pkg/k8s"
	"k8s.io/client-go/rest"
)

// InstallerBase provides common fields and methods for CNI installers.
// It encapsulates shared functionality like Helm client management, kubeconfig handling,
// timeout management, and readiness checks. CNI implementations should embed this type
// as a pointer (*cni.InstallerBase) to inherit these capabilities.
//
// Note: Helm chart installation utilities have been moved to pkg/client/helm package
// (helm.InstallOrUpgradeChart, helm.RepoConfig, helm.ChartConfig). Readiness checking
// utilities are available via installer.WaitForResourceReadiness (pkg/svc/installer/readiness.go)
// which wraps the lower-level k8s.WaitForMultipleResources from pkg/k8s package.
//
// Example usage:
//
//	type MyCNIInstaller struct {
//	    *cni.InstallerBase  // Must be embedded as a pointer
//	}
//
//	installer := &MyCNIInstaller{}
//	installer.InstallerBase = cni.NewInstallerBase(
//	    helmClient, kubeconfig, context, timeout, installer.waitForReadiness,
//	)
type InstallerBase struct {
	kubeconfig string
	context    string
	timeout    time.Duration
	client     helm.Interface
	waitFn     func(context.Context) error
}

// NewInstallerBase creates a new base installer instance with the provided configuration.
// The waitFn parameter allows CNI implementations to provide custom readiness checking logic.
// If waitFn is nil, readiness checks are skipped.
func NewInstallerBase(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
	waitFn func(context.Context) error,
) *InstallerBase {
	return &InstallerBase{
		client:     client,
		kubeconfig: kubeconfig,
		context:    context,
		timeout:    timeout,
		waitFn:     waitFn,
	}
}

// WaitForReadiness waits for the CNI components to become ready.
func (b *InstallerBase) WaitForReadiness(ctx context.Context) error {
	if b.waitFn == nil {
		return nil
	}

	err := b.waitFn(ctx)
	if err != nil {
		return fmt.Errorf("wait for readiness: %w", err)
	}

	return nil
}

// SetWaitForReadinessFunc overrides the readiness wait function. Primarily used for testing.
func (b *InstallerBase) SetWaitForReadinessFunc(
	waitFunc func(context.Context) error,
	defaultWaitFn func(context.Context) error,
) {
	if waitFunc == nil {
		b.waitFn = defaultWaitFn

		return
	}

	b.waitFn = waitFunc
}

// BuildRESTConfig builds a Kubernetes REST configuration.
func (b *InstallerBase) BuildRESTConfig() (*rest.Config, error) {
	config, err := k8s.BuildRESTConfig(b.kubeconfig, b.context)
	if err != nil {
		return nil, fmt.Errorf("build REST config: %w", err)
	}

	return config, nil
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

// GetWaitFn returns the wait function for testing purposes.
// This method is primarily used in tests to verify wait function behavior.
func (b *InstallerBase) GetWaitFn() func(context.Context) error {
	return b.waitFn
}

// SetWaitFn sets the wait function directly for testing purposes.
// This is a low-level method used primarily in tests. Prefer using SetWaitForReadinessFunc for production code.
func (b *InstallerBase) SetWaitFn(fn func(context.Context) error) {
	b.waitFn = fn
}
