package clusterprovisioner

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
	k3dprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/k3d"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	talosprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talos"
	k3dtypes "github.com/k3d-io/k3d/v5/pkg/config/types"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// MultiProvisioner wraps multiple distribution provisioners and routes operations
// to the appropriate one based on which cluster exists. It is used when only a
// cluster name and provider are known (without distribution information).
type MultiProvisioner struct {
	clusterName string
}

// NewMultiProvisioner creates a provisioner that tries multiple distributions.
func NewMultiProvisioner(clusterName string) *MultiProvisioner {
	return &MultiProvisioner{clusterName: clusterName}
}

// clusterOperation is a function that operates on a provisioner with a cluster name.
type clusterOperation func(Provisioner, string) error

// supportedDistributions returns all supported distributions in priority order.
func supportedDistributions() []v1alpha1.Distribution {
	return []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos,
	}
}

// Start starts the cluster by trying each distribution's provisioner.
func (m *MultiProvisioner) Start(ctx context.Context, name string) error {
	return m.delegateToExisting(ctx, name, "start",
		func(p Provisioner, n string) error { return p.Start(ctx, n) },
	)
}

// Stop stops the cluster by trying each distribution's provisioner.
func (m *MultiProvisioner) Stop(ctx context.Context, name string) error {
	return m.delegateToExisting(ctx, name, "stop",
		func(p Provisioner, n string) error { return p.Stop(ctx, n) },
	)
}

// Delete deletes the cluster by trying each distribution's provisioner.
func (m *MultiProvisioner) Delete(ctx context.Context, name string) error {
	return m.delegateToExisting(ctx, name, "delete",
		func(p Provisioner, n string) error { return p.Delete(ctx, n) },
	)
}

// Exists checks if the cluster exists in any distribution.
func (m *MultiProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	found := false
	err := m.forExistingCluster(
		ctx,
		name,
		func(_ Provisioner, _ string) error {
			found = true

			return nil
		},
	)

	// forExistingCluster returns ErrClusterNotFoundInDistributions if not found,
	// which is expected - we just return false in that case
	if err != nil && !errors.Is(err, clustererr.ErrClusterNotFoundInDistributions) {
		return false, err
	}

	return found, nil
}

// List lists all clusters across all distributions.
func (m *MultiProvisioner) List(ctx context.Context) ([]string, error) {
	var allClusters []string

	for _, dist := range supportedDistributions() {
		provisioner, err := CreateMinimalProvisioner(dist, m.clusterName, "", "")
		if err != nil {
			continue
		}

		clusters, err := provisioner.List(ctx)
		if err != nil {
			continue
		}

		allClusters = append(allClusters, clusters...)
	}

	return allClusters, nil
}

// Create is not supported by the multi-distribution provisioner.
func (m *MultiProvisioner) Create(_ context.Context, _ string) error {
	return clustererr.ErrCreateNotSupported
}

// delegateToExisting finds the existing cluster and delegates the operation,
// wrapping any error with a descriptive verb.
func (m *MultiProvisioner) delegateToExisting(
	ctx context.Context,
	name string,
	verb string,
	operation clusterOperation,
) error {
	return m.forExistingCluster(
		ctx,
		name,
		func(p Provisioner, n string) error {
			err := operation(p, n)
			if err != nil {
				return fmt.Errorf("failed to %s cluster: %w", verb, err)
			}

			return nil
		},
	)
}

// forExistingCluster finds an existing cluster across all distributions and applies an operation.
// Returns ErrClusterNotFoundInDistributions if the cluster doesn't exist in any distribution.
func (m *MultiProvisioner) forExistingCluster(
	ctx context.Context,
	name string,
	operation clusterOperation,
) error {
	clusterName := name
	if clusterName == "" {
		clusterName = m.clusterName
	}

	for _, dist := range supportedDistributions() {
		provisioner, err := CreateMinimalProvisioner(dist, clusterName, "", "")
		if err != nil {
			continue
		}

		exists, err := provisioner.Exists(ctx, clusterName)
		if err != nil {
			continue
		}

		if exists {
			return operation(provisioner, clusterName)
		}
	}

	return fmt.Errorf("%w: %s", clustererr.ErrClusterNotFoundInDistributions, clusterName)
}

// CreateMinimalProvisioner creates a minimal provisioner for a specific distribution.
// Minimal provisioners only need enough configuration to identify containers —
// they don't require full cluster configuration.
//
// The kubeconfigPath and providerType parameters allow callers that have
// richer context to pass it through; when empty, defaults
// are used ("" and ProviderDocker respectively).
func CreateMinimalProvisioner(
	dist v1alpha1.Distribution,
	clusterName string,
	kubeconfigPath string,
	providerType v1alpha1.Provider,
) (Provisioner, error) {
	if providerType == "" {
		providerType = v1alpha1.ProviderDocker
	}

	switch dist {
	case v1alpha1.DistributionVanilla:
		kindConfig := &v1alpha4.Cluster{Name: clusterName}

		provisioner, err := kindprovisioner.CreateProvisioner(kindConfig, "")
		if err != nil {
			return nil, fmt.Errorf("failed to create Kind provisioner: %w", err)
		}

		return provisioner, nil

	case v1alpha1.DistributionK3s:
		k3dConfig := &k3dv1alpha5.SimpleConfig{
			ObjectMeta: k3dtypes.ObjectMeta{Name: clusterName},
		}

		return k3dprovisioner.CreateProvisioner(k3dConfig, ""), nil

	case v1alpha1.DistributionTalos:
		talosConfig := &talosconfigmanager.Configs{Name: clusterName}

		provisioner, err := talosprovisioner.CreateProvisioner(
			talosConfig,
			kubeconfigPath,
			providerType,
			v1alpha1.OptionsTalos{},
			v1alpha1.OptionsHetzner{},
			false,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create Talos provisioner: %w", err)
		}

		return provisioner, nil

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedDistribution, dist)
	}
}
