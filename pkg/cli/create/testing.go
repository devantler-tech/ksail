package create

import (
	"context"
	"sync"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

// TestFactories holds factory function overrides for testing.
// These allow tests to inject mock implementations without global state.
type TestFactories struct {
	// CertManager overrides the cert-manager installer factory.
	CertManager func(*v1alpha1.Cluster) (installer.Installer, error)
	// CSI overrides the CSI installer factory.
	CSI func(*v1alpha1.Cluster) (installer.Installer, error)
	// ArgoCD overrides the ArgoCD installer factory.
	ArgoCD func(*v1alpha1.Cluster) (installer.Installer, error)
	// EnsureArgoCDResources overrides the ArgoCD resource ensure function.
	EnsureArgoCDResources func(context.Context, string, *v1alpha1.Cluster) error
	// DockerClientInvoker overrides the Docker client invoker.
	DockerClientInvoker func(*cobra.Command, func(client.APIClient) error) error
	// ClusterProvisionerFactory overrides the cluster provisioner factory.
	ClusterProvisionerFactory clusterprovisioner.Factory
}

// TestOverrides provides thread-safe access to test factory overrides.
// Use SetForTests methods to override specific factories in tests.
type TestOverrides struct {
	mu        sync.RWMutex
	factories *TestFactories
}

// NewTestOverrides creates a new TestOverrides instance.
func NewTestOverrides() *TestOverrides {
	return &TestOverrides{
		factories: &TestFactories{},
	}
}

// GlobalTestOverrides provides a global test override instance.
// This is used for backward compatibility with existing tests.
//
//nolint:gochecknoglobals // Required for test injection pattern
var GlobalTestOverrides = NewTestOverrides()

// GetCertManager returns the cert-manager installer factory override.
func (t *TestOverrides) GetCertManager() func(*v1alpha1.Cluster) (installer.Installer, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.factories.CertManager
}

// SetCertManager sets the cert-manager installer factory for tests.
// Returns a restore function that resets the factory to its previous value.
func (t *TestOverrides) SetCertManager(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	t.mu.Lock()

	previous := t.factories.CertManager
	t.factories.CertManager = factory

	t.mu.Unlock()

	return func() {
		t.mu.Lock()
		t.factories.CertManager = previous
		t.mu.Unlock()
	}
}

// GetCSI returns the CSI installer factory override.
func (t *TestOverrides) GetCSI() func(*v1alpha1.Cluster) (installer.Installer, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.factories.CSI
}

// SetCSI sets the CSI installer factory for tests.
// Returns a restore function that resets the factory to its previous value.
func (t *TestOverrides) SetCSI(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	t.mu.Lock()

	previous := t.factories.CSI
	t.factories.CSI = factory

	t.mu.Unlock()

	return func() {
		t.mu.Lock()
		t.factories.CSI = previous
		t.mu.Unlock()
	}
}

// GetArgoCD returns the ArgoCD installer factory override.
func (t *TestOverrides) GetArgoCD() func(*v1alpha1.Cluster) (installer.Installer, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.factories.ArgoCD
}

// SetArgoCD sets the ArgoCD installer factory for tests.
// Returns a restore function that resets the factory to its previous value.
func (t *TestOverrides) SetArgoCD(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	t.mu.Lock()

	previous := t.factories.ArgoCD
	t.factories.ArgoCD = factory

	t.mu.Unlock()

	return func() {
		t.mu.Lock()
		t.factories.ArgoCD = previous
		t.mu.Unlock()
	}
}

// GetEnsureArgoCDResources returns the ArgoCD resource ensure function override.
func (t *TestOverrides) GetEnsureArgoCDResources() func(context.Context, string, *v1alpha1.Cluster) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.factories.EnsureArgoCDResources
}

// SetEnsureArgoCDResources sets the ArgoCD resource ensure function for tests.
// Returns a restore function that resets the function to its previous value.
func (t *TestOverrides) SetEnsureArgoCDResources(
	fn func(context.Context, string, *v1alpha1.Cluster) error,
) func() {
	t.mu.Lock()

	previous := t.factories.EnsureArgoCDResources
	t.factories.EnsureArgoCDResources = fn

	t.mu.Unlock()

	return func() {
		t.mu.Lock()
		t.factories.EnsureArgoCDResources = previous
		t.mu.Unlock()
	}
}

// GetDockerClientInvoker returns the Docker client invoker override.
func (t *TestOverrides) GetDockerClientInvoker() func(*cobra.Command, func(client.APIClient) error) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.factories.DockerClientInvoker
}

// SetDockerClientInvoker sets the Docker client invoker for tests.
// Returns a restore function that resets the invoker to its previous value.
func (t *TestOverrides) SetDockerClientInvoker(
	invoker func(*cobra.Command, func(client.APIClient) error) error,
) func() {
	t.mu.Lock()

	previous := t.factories.DockerClientInvoker
	t.factories.DockerClientInvoker = invoker

	t.mu.Unlock()

	return func() {
		t.mu.Lock()
		t.factories.DockerClientInvoker = previous
		t.mu.Unlock()
	}
}

// GetClusterProvisionerFactory returns the cluster provisioner factory override.
//
//nolint:ireturn // Returns interface to maintain API compatibility with clusterprovisioner.Factory.
func (t *TestOverrides) GetClusterProvisionerFactory() clusterprovisioner.Factory {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.factories.ClusterProvisionerFactory
}

// SetClusterProvisionerFactory sets the cluster provisioner factory for tests.
// Returns a restore function that resets the factory to nil.
func (t *TestOverrides) SetClusterProvisionerFactory(factory clusterprovisioner.Factory) func() {
	t.mu.Lock()

	previous := t.factories.ClusterProvisionerFactory
	t.factories.ClusterProvisionerFactory = factory

	t.mu.Unlock()

	return func() {
		t.mu.Lock()
		t.factories.ClusterProvisionerFactory = previous
		t.mu.Unlock()
	}
}
