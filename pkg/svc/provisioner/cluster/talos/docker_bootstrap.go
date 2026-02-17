package talosprovisioner

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/siderolabs/talos/pkg/cluster/check"
	"github.com/siderolabs/talos/pkg/conditions"
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
	talosConfig, err := clientconfig.Open("")
	if err != nil {
		return fmt.Errorf("failed to load talosconfig: %w", err)
	}

	// Create ClusterAccess adapter using upstream SDK pattern
	clusterAccess := access.NewAdapter(
		cluster,
		provision.WithTalosConfig(talosConfig),
	)

	defer clusterAccess.Close() //nolint:errcheck

	err = p.runDockerClusterChecks(ctx, clusterAccess)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "✓ Cluster is ready\n")

	return nil
}

// runDockerClusterChecks runs CNI-aware readiness checks on a Docker cluster.
// It selects the appropriate checks based on CNI configuration, logs progress,
// and waits for all checks to pass.
func (p *Provisioner) runDockerClusterChecks(
	ctx context.Context,
	clusterAccess *access.Adapter,
) error {
	checks := p.clusterReadinessChecks()

	if (p.talosConfigs != nil && p.talosConfigs.IsCNIDisabled()) || p.options.SkipCNIChecks {
		_, _ = fmt.Fprintf(
			p.logWriter,
			"  Running pre-boot and K8s component checks (CNI not installed yet)...\n",
		)
	} else {
		_, _ = fmt.Fprintf(p.logWriter, "  Running full cluster readiness checks...\n")
	}

	reporter := &dockerCheckReporter{writer: p.logWriter}

	checkCtx, cancel := context.WithTimeout(ctx, dockerClusterReadinessTimeout)
	defer cancel()

	err := check.Wait(checkCtx, clusterAccess, checks, reporter)
	if err != nil {
		return fmt.Errorf("cluster readiness checks failed: %w", err)
	}

	return nil
}

// dockerCheckReporter implements check.Reporter to log check progress for Docker clusters.
type dockerCheckReporter struct {
	writer   io.Writer
	lastLine string
}

func (r *dockerCheckReporter) Update(condition conditions.Condition) {
	line := fmt.Sprintf("    %s", condition)
	if line != r.lastLine {
		_, _ = fmt.Fprintf(r.writer, "%s\n", line)
		r.lastLine = line
	}
}
