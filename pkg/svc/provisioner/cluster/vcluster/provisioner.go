package vclusterprovisioner

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
	loftlog "github.com/loft-sh/log"
	"github.com/loft-sh/vcluster/pkg/cli"
	cliconfig "github.com/loft-sh/vcluster/pkg/cli/config"
	"github.com/loft-sh/vcluster/pkg/cli/flags"
	"github.com/sirupsen/logrus"
)

// defaultVClusterName is used when no name is provided.
const defaultVClusterName = "vcluster-default"

// defaultChartVersion is the vCluster Helm chart version used by the Docker driver.
// Must be available on ghcr.io/loft-sh/vcluster-pro as an OCI artifact.
// Check: https://ghcr.io/loft-sh/vcluster-pro for available tags.
const defaultChartVersion = "0.31.0-alpha.0"

// Provisioner implements the cluster provisioner interface for vCluster's Docker
// driver (Vind). Create and Delete use the vCluster Go SDK directly, while
// Start/Stop/List/Exists delegate to the Docker infrastructure provider.
type Provisioner struct {
	name          string
	valuesPath    string
	infraProvider provider.Provider
}

// NewProvisioner constructs a new vCluster provisioner.
//
// Parameters:
//   - name: default cluster name (used when no name is passed to methods)
//   - valuesPath: optional path to a vcluster.yaml values file
//   - infraProvider: Docker infrastructure provider for Start/Stop/List/Exists
func NewProvisioner(
	name string,
	valuesPath string,
	infraProvider provider.Provider,
) *Provisioner {
	if name == "" {
		name = defaultVClusterName
	}

	return &Provisioner{
		name:          name,
		valuesPath:    valuesPath,
		infraProvider: infraProvider,
	}
}

// SetProvider sets the infrastructure provider for node operations.
// This implements the ProviderAware interface.
func (p *Provisioner) SetProvider(prov provider.Provider) {
	p.infraProvider = prov
}

// Create provisions a vCluster using the Docker driver via the vCluster Go SDK.
func (p *Provisioner) Create(ctx context.Context, name string) error {
	target := p.resolveName(name)

	opts := &cli.CreateOptions{
		Driver:       "docker",
		ChartVersion: defaultChartVersion,
		Connect:      false,
		Upgrade:      false,
		Distro:       "k8s",
	}

	if p.valuesPath != "" {
		opts.Values = []string{p.valuesPath}
	}

	globalFlags := newGlobalFlags()
	logger := newStreamLogger()

	err := cli.CreateDocker(ctx, opts, globalFlags, target, logger)
	if err != nil {
		return fmt.Errorf("failed to create vCluster: %w", err)
	}

	return nil
}

// Delete removes a vCluster using the Docker driver via the vCluster Go SDK.
// Returns clustererr.ErrClusterNotFound if the cluster does not exist.
func (p *Provisioner) Delete(ctx context.Context, name string) error {
	target := p.resolveName(name)

	exists, err := p.Exists(ctx, target)
	if err != nil {
		return fmt.Errorf("failed to check cluster existence: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: %s", clustererr.ErrClusterNotFound, target)
	}

	opts := &cli.DeleteOptions{
		Driver:        "docker",
		DeleteContext: true,
		IgnoreNotFound: true,
	}

	globalFlags := newGlobalFlags()
	logger := newStreamLogger()

	// platformClient is nil for local Docker-based clusters (no platform integration).
	err = cli.DeleteDocker(ctx, nil, opts, globalFlags, target, logger)
	if err != nil {
		return fmt.Errorf("failed to delete vCluster: %w", err)
	}

	return nil
}

// Start starts a stopped vCluster by starting its Docker containers.
// Delegates to the infrastructure provider for container operations.
func (p *Provisioner) Start(ctx context.Context, name string) error {
	return p.withProvider(ctx, name, "start", p.infraProvider.StartNodes)
}

// Stop stops a running vCluster by stopping its Docker containers.
// Delegates to the infrastructure provider for container operations.
func (p *Provisioner) Stop(ctx context.Context, name string) error {
	return p.withProvider(ctx, name, "stop", p.infraProvider.StopNodes)
}

// List returns all vCluster clusters by querying the Docker infrastructure provider.
func (p *Provisioner) List(ctx context.Context) ([]string, error) {
	if p.infraProvider == nil {
		return nil, fmt.Errorf("%w for vCluster list", clustererr.ErrProviderNotSet)
	}

	return p.infraProvider.ListAllClusters(ctx)
}

// Exists checks if a vCluster cluster exists by querying the Docker infrastructure provider.
func (p *Provisioner) Exists(ctx context.Context, name string) (bool, error) {
	target := p.resolveName(name)

	clusters, err := p.List(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to list vClusters: %w", err)
	}

	return slices.Contains(clusters, target), nil
}

// --- internals ---

// withProvider executes a provider operation with proper nil check and error wrapping.
func (p *Provisioner) withProvider(
	ctx context.Context,
	name string,
	operationName string,
	providerFunc func(ctx context.Context, clusterName string) error,
) error {
	target := p.resolveName(name)

	if p.infraProvider == nil {
		return fmt.Errorf("%w for cluster '%s'", clustererr.ErrProviderNotSet, target)
	}

	err := providerFunc(ctx, target)
	if err != nil {
		return fmt.Errorf("failed to %s cluster '%s': %w", operationName, target, err)
	}

	return nil
}

func (p *Provisioner) resolveName(name string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}

	return p.name
}

// newGlobalFlags creates a minimal GlobalFlags for the vCluster Go SDK.
// Config is set to the default path (~/.vcluster/config.json) so that
// OCI image caches are stored persistently across runs.
func newGlobalFlags() *flags.GlobalFlags {
	configPath, err := cliconfig.DefaultFilePath()
	if err != nil {
		configPath = ""
	}

	return &flags.GlobalFlags{
		Config: configPath,
	}
}

// newStreamLogger creates a loft-sh/log Logger that writes to stdout.
func newStreamLogger() loftlog.Logger {
	return loftlog.NewStreamLogger(os.Stdout, os.Stderr, logrus.InfoLevel)
}
