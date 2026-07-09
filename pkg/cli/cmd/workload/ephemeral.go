package workload

import (
	"context"
	"errors"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/notify"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	kwokprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kwok"
	"github.com/spf13/cobra"
)

// ephemeralClusterNamePrefix names throwaway clusters provisioned for
// --ephemeral validate/scan runs, distinguishing them from user-managed
// clusters in `docker ps` / `kwokctl get clusters` output.
const ephemeralClusterNamePrefix = "ksail-ephemeral-"

// ephemeralDeleteTimeout bounds the guaranteed-teardown delete call so a
// wedged provisioner delete cannot hang the CLI forever after a signal.
const ephemeralDeleteTimeout = 2 * time.Minute

// newEphemeralProvisioner builds the cluster provisioner an --ephemeral run
// provisions and tears down. KWOK is the only backend for now (Phase 3b-1 of
// ksail#5919): cheap, API-only cluster expansion with no real workload
// scheduling, which is all a provision/teardown scaffold needs. A package var
// so tests can substitute a fake without a live Docker/KWOK dependency.
//
//nolint:gochecknoglobals // test seam, mirrors newFlowObserver/newMirrorClients in this package
var newEphemeralProvisioner = func(name string) clusterprovisioner.Provisioner {
	return kwokprovisioner.NewProvisioner(name, "", nil)
}

// withEphemeralCluster provisions a throwaway cluster, runs runFn while it is
// live, and guarantees the cluster is deleted afterwards — on success, on an
// error from runFn, and on SIGINT/SIGTERM. It is scaffold-only: runFn runs
// unchanged today (validate/scan still operate on local files); wiring the
// ephemeral cluster's kubeconfig into the installer/apply/scan pipeline is a
// follow-up (ksail#5919 Phase 3b-2/3b-3).
//
// ctx should be the caller's own context (typically cmd.Context()); cmd is
// used only for output (notify) and is not read for its context here, so the
// two never silently disagree.
//
// Unlike --ttl (pkg/cli/cmd/cluster/ttl.go), which *skips* deletion on
// interrupt so a user can inspect a debug cluster, an --ephemeral cluster is
// always torn down: it exists only for the duration of this command and was
// never meant to survive it.
//
// If both runFn and the deferred delete fail, both errors are returned via
// errors.Join so neither is silently dropped.
func withEphemeralCluster(
	ctx context.Context,
	cmd *cobra.Command,
	runFn func(ctx context.Context) error,
) (err error) {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	name := ephemeralClusterName()
	provisioner := newEphemeralProvisioner(name)

	notify.Infof(cmd.OutOrStdout(), "provisioning ephemeral cluster %q for --ephemeral...", name)

	createErr := provisioner.Create(ctx, name)
	if createErr != nil {
		return fmt.Errorf("provision ephemeral cluster %q: %w", name, createErr)
	}

	defer func() {
		// context.WithoutCancel: the outer ctx may already be Done() (the
		// SIGINT/SIGTERM that triggered this deferred call, or runFn's own
		// early-return on context cancellation) — teardown must run
		// regardless, bounded only by its own fresh timeout. Mirrors the
		// KWOK provisioner's own cleanupAttempt pattern
		// (pkg/svc/provisioner/cluster/kwok/provisioner.go).
		deleteCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ephemeralDeleteTimeout)
		defer cancel()

		notify.Infof(cmd.OutOrStdout(), "tearing down ephemeral cluster %q...", name)

		deleteErr := provisioner.Delete(deleteCtx, name)
		if deleteErr != nil {
			err = errors.Join(err, fmt.Errorf("delete ephemeral cluster %q: %w", name, deleteErr))
		}
	}()

	return runFn(ctx)
}

// ephemeralClusterName generates a unique, DNS-1123-safe name for a throwaway
// --ephemeral cluster so concurrent invocations never collide.
func ephemeralClusterName() string {
	return fmt.Sprintf("%s%d", ephemeralClusterNamePrefix, time.Now().UnixNano())
}
