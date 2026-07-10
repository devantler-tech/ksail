package workload

import (
	"context"
	"errors"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
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

// waitForEphemeralCluster verifies a freshly provisioned --ephemeral cluster
// is genuinely usable — the API server answers /readyz AND a basic authorized
// read succeeds — before runFn is invoked, so downstream steps never race the
// control plane's warm-up window. A package var so tests can substitute a
// fake without a live Docker/KWOK dependency.
//
//nolint:gochecknoglobals // test seam, mirrors newEphemeralProvisioner above
var waitForEphemeralCluster = k8s.WaitForClusterReady

// ephemeralCluster describes how a --ephemeral run reaches its throwaway
// cluster once provisioned: the kubeconfig file the embedded kwokctl merged
// the new context into, and that context's name. Phase 3b-2 of ksail#5919
// consumes this handle to install the workload's declared operators into the
// cluster; Phase 3b-3 applies the rendered manifests and scans their children.
type ephemeralCluster struct {
	// Name is the throwaway cluster's own name (ksail-ephemeral-<nanos>).
	Name string
	// KubeconfigPath is the kubeconfig file holding the cluster's context —
	// the same file kwokctl writes to ($KUBECONFIG, or ~/.kube/config).
	KubeconfigPath string
	// Context is the kubeconfig context kwokctl created ("kwok-<name>").
	Context string
}

// resolveEphemeralCluster derives the connection handle for a provisioned
// --ephemeral cluster. The KWOK provisioner does not hand back a kubeconfig;
// the embedded kwokctl merges the cluster's context into the kubeconfig at
// $KUBECONFIG (or ~/.kube/config) under the distribution's context naming
// convention, so the handle is derived from the cluster name.
func resolveEphemeralCluster(name string) (ephemeralCluster, error) {
	kubeconfigPath, err := k8s.ResolveKubeconfigPath("")
	if err != nil {
		return ephemeralCluster{}, fmt.Errorf(
			"resolve kubeconfig path for ephemeral cluster %q: %w", name, err,
		)
	}

	distribution := v1alpha1.DistributionKWOK

	return ephemeralCluster{
		Name:           name,
		KubeconfigPath: kubeconfigPath,
		Context:        distribution.ContextName(name),
	}, nil
}

// withEphemeralCluster provisions a throwaway cluster, waits until it is
// genuinely ready, runs runFn with the cluster's connection handle while it
// is live, and guarantees the cluster is deleted afterwards — on success, on
// an error from any step, and on SIGINT/SIGTERM. runFn receives a verified
// ephemeralCluster (kubeconfig path + context); wiring it into the
// installer/apply/scan pipeline is the remainder of ksail#5919 Phase
// 3b-2/3b-3.
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
	runFn func(ctx context.Context, cluster ephemeralCluster) error,
) (err error) {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	name := ephemeralClusterName()
	provisioner := newEphemeralProvisioner(name)

	deleteCluster := func() error {
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
			return fmt.Errorf("delete ephemeral cluster %q: %w", name, deleteErr)
		}

		return nil
	}

	notify.Infof(cmd.OutOrStdout(), "provisioning ephemeral cluster %q for --ephemeral...", name)

	createErr := provisioner.Create(ctx, name)
	if createErr != nil {
		return errors.Join(
			fmt.Errorf("provision ephemeral cluster %q: %w", name, createErr),
			deleteCluster(),
		)
	}

	defer func() {
		err = errors.Join(err, deleteCluster())
	}()

	cluster, resolveErr := resolveEphemeralCluster(name)
	if resolveErr != nil {
		return resolveErr
	}

	notify.Infof(cmd.OutOrStdout(), "waiting for ephemeral cluster %q to become ready...", name)

	waitErr := waitForEphemeralCluster(ctx, cluster.KubeconfigPath, cluster.Context)
	if waitErr != nil {
		return fmt.Errorf("ephemeral cluster %q never became ready: %w", name, waitErr)
	}

	return runFn(ctx, cluster)
}

// ephemeralClusterName generates a unique, DNS-1123-safe name for a throwaway
// --ephemeral cluster so concurrent invocations never collide.
func ephemeralClusterName() string {
	return fmt.Sprintf("%s%d", ephemeralClusterNamePrefix, time.Now().UnixNano())
}
