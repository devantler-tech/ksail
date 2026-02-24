package talosprovisioner

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"github.com/siderolabs/talos/pkg/provision"
	"github.com/siderolabs/talos/pkg/provision/access"
)

const (
	// dockerClusterReadinessTimeout defines how long to wait for Docker cluster to become ready.
	dockerClusterReadinessTimeout = 5 * time.Minute
)

// waitForDockerClusterReadyAfterStart waits for a Docker cluster to be ready after starting.
// This loads the TalosConfig from disk and runs readiness checks to ensure the cluster is operational.
func (p *Provisioner) waitForDockerClusterReadyAfterStart(
	ctx context.Context,
	clusterName string,
) error {
	_, _ = fmt.Fprintf(p.logWriter, "Waiting for cluster to be ready...\n")

	// Get state directory for cluster state
	stateDir, err := getStateDirectory()
	if err != nil {
		return fmt.Errorf("failed to get state directory: %w", err)
	}

	// Create Talos provisioner to reflect cluster state
	talosProvisioner, err := p.provisionerFactory(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Talos provisioner: %w", err)
	}

	defer func() { _ = talosProvisioner.Close() }()

	// Reflect cluster state from existing state
	cluster, err := talosProvisioner.Reflect(ctx, clusterName, stateDir)
	if err != nil {
		return fmt.Errorf("failed to reflect cluster state: %w", err)
	}

	// Load TalosConfig from disk (since we don't have the config bundle during start)
	rawTalosconfigPath := p.options.TalosconfigPath
	if rawTalosconfigPath == "" {
		rawTalosconfigPath = "~/.talos/config"
	}

	talosconfigPath, err := fsutil.ExpandHomePath(rawTalosconfigPath)
	if err != nil {
		return fmt.Errorf("failed to expand talosconfig path: %w", err)
	}

	talosConfig, err := clientconfig.Open(talosconfigPath)
	if err != nil {
		return fmt.Errorf("failed to load talosconfig: %w", err)
	}

	// Create ClusterAccess adapter using upstream SDK pattern
	clusterAccess := access.NewAdapter(
		cluster,
		provision.WithTalosConfig(talosConfig),
	)

	defer clusterAccess.Close() //nolint:errcheck

	err = p.runClusterChecks(ctx, clusterAccess, dockerClusterReadinessTimeout)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "✓ Cluster is ready\n")

	return nil
}
