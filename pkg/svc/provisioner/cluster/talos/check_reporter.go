package talosprovisioner

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/siderolabs/talos/pkg/cluster/check"
	"github.com/siderolabs/talos/pkg/conditions"
	"github.com/siderolabs/talos/pkg/provision/access"
)

// checkReporter implements check.Reporter to log check progress.
type checkReporter struct {
	writer   io.Writer
	lastLine string
}

func (r *checkReporter) Update(condition conditions.Condition) {
	line := fmt.Sprintf("    %s", condition)
	if line != r.lastLine {
		_, _ = fmt.Fprintf(r.writer, "%s\n", line)
		r.lastLine = line
	}
}

// runClusterChecks runs CNI-aware readiness checks on a cluster.
// It selects the appropriate checks based on CNI configuration, logs progress,
// and waits for all checks to pass within the given timeout.
func (p *Provisioner) runClusterChecks(
	ctx context.Context,
	clusterAccess *access.Adapter,
	timeout time.Duration,
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

	reporter := &checkReporter{writer: p.logWriter}

	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	err := check.Wait(checkCtx, clusterAccess, checks, reporter)
	if err != nil {
		return fmt.Errorf("cluster readiness checks failed: %w", err)
	}

	return nil
}
