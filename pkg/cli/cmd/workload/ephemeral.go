package workload

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/spf13/cobra"
)

// ephemeralClusterNamePrefix names throwaway clusters provisioned for
// --ephemeral validate/scan runs, distinguishing them from user-managed
// clusters in `docker ps` / `kind get clusters` output.
const ephemeralClusterNamePrefix = "ksail-ephemeral-"

// ephemeralDeleteTimeout bounds the guaranteed-teardown delete call so a
// wedged provisioner delete cannot hang the CLI forever after a signal.
const ephemeralDeleteTimeout = 2 * time.Minute

// waitForEphemeralCluster verifies a freshly provisioned --ephemeral cluster
// is genuinely usable — the API server answers /readyz AND a basic authorized
// read succeeds — before runFn is invoked, so downstream steps never race the
// control plane's warm-up window. A package var so tests can substitute a
// fake without a live Docker/Kind dependency.
//
//nolint:gochecknoglobals // test seam, mirrors newEphemeralBackend below
var waitForEphemeralCluster = k8s.WaitForClusterReady

// ephemeralCluster describes how a --ephemeral run reaches its throwaway Kind
// cluster once provisioned. Phase 3b-2 of ksail#5919 consumes this handle to
// install the workload's declared operators into the cluster; Phase 3b-3
// applies the rendered manifests and scans their children.
type ephemeralCluster struct {
	// Name is the throwaway cluster's own name (ksail-ephemeral-<nanos>).
	Name string
	// KubeconfigPath is the isolated temporary kubeconfig file holding the
	// cluster's context. It is removed after the cluster is deleted.
	KubeconfigPath string
	// Context is the kubeconfig context Kind created ("kind-<name>").
	Context string
}

// ephemeralBackend owns both halves of an --ephemeral run: the cluster
// provisioner and connection handle, plus local-only resources that must be
// removed after cluster deletion.
type ephemeralBackend struct {
	provisioner clusterprovisioner.Provisioner
	cluster     ephemeralCluster
	cleanup     func() error
}

// newEphemeralBackend builds a controller-capable Kind backend with a
// per-invocation kubeconfig. It is a package variable so lifecycle tests can
// substitute a fake backend without Docker.
//
//nolint:gochecknoglobals // test seam, mirrors newFlowObserver/newMirrorClients in this package
var newEphemeralBackend = createEphemeralBackend

// createEphemeralBackend constructs the production Kind backend without
// touching the user's shared kubeconfig. Constructing the backend does not
// create a cluster; Provisioner.Create does that after the lifecycle's cleanup
// defers are armed.
func createEphemeralBackend(name string) (ephemeralBackend, error) {
	workspace, err := os.MkdirTemp("", ephemeralClusterNamePrefix)
	if err != nil {
		return ephemeralBackend{}, fmt.Errorf("create ephemeral backend workspace: %w", err)
	}

	cleanup := func() error {
		removeErr := os.RemoveAll(workspace)
		if removeErr != nil {
			return fmt.Errorf("remove ephemeral backend workspace %q: %w", workspace, removeErr)
		}

		return nil
	}

	kubeconfigPath := filepath.Join(workspace, "kubeconfig")

	provisioner, err := clusterprovisioner.CreateMinimalProvisioner(
		v1alpha1.DistributionVanilla,
		name,
		kubeconfigPath,
		v1alpha1.ProviderDocker,
	)
	if err != nil {
		return ephemeralBackend{}, errors.Join(
			fmt.Errorf("create Kind provisioner for ephemeral cluster %q: %w", name, err),
			cleanup(),
		)
	}

	distribution := v1alpha1.DistributionVanilla

	return ephemeralBackend{
		provisioner: provisioner,
		cluster: ephemeralCluster{
			Name:           name,
			KubeconfigPath: kubeconfigPath,
			Context:        distribution.ContextName(name),
		},
		cleanup: cleanup,
	}, nil
}

// withEphemeralCluster provisions a throwaway cluster, waits until it is
// genuinely ready, runs runFn with the cluster's connection handle while it
// is live, and guarantees the cluster is deleted afterwards — on success, on
// an error from any step, and on SIGINT/SIGTERM. runFn receives a verified
// ephemeralCluster (kubeconfig path + context). Applying workload manifests,
// waiting for operator reconciliation, and inspecting generated children are
// deliberately left to ksail#5919 Phase 3b-3.
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
// Errors from runFn, cluster deletion, and local workspace cleanup are joined
// so none is silently dropped.
func withEphemeralCluster(
	ctx context.Context,
	cmd *cobra.Command,
	runFn func(ctx context.Context, cluster ephemeralCluster) error,
) (err error) {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	name := ephemeralClusterName()

	backend, backendErr := newEphemeralBackend(name)
	if backendErr != nil {
		return fmt.Errorf("create ephemeral cluster backend: %w", backendErr)
	}

	defer func() {
		if backend.cleanup == nil {
			return
		}

		err = errors.Join(err, backend.cleanup())
	}()

	provisioner := backend.provisioner
	cluster := backend.cluster

	notify.Infof(cmd.OutOrStdout(), "provisioning ephemeral cluster %q for --ephemeral...", name)

	createErr := provisioner.Create(ctx, name)
	if createErr != nil {
		cleanupErr := deleteEphemeralCluster(ctx, cmd, provisioner, name)
		if errors.Is(cleanupErr, clustererr.ErrClusterNotFound) {
			cleanupErr = nil
		}

		return errors.Join(
			fmt.Errorf("provision ephemeral cluster %q: %w", name, createErr),
			cleanupErr,
		)
	}

	defer func() {
		err = errors.Join(err, deleteEphemeralCluster(ctx, cmd, provisioner, name))
	}()

	notify.Infof(cmd.OutOrStdout(), "waiting for ephemeral cluster %q to become ready...", name)

	waitErr := waitForEphemeralCluster(ctx, cluster.KubeconfigPath, cluster.Context)
	if waitErr != nil {
		return fmt.Errorf("ephemeral cluster %q never became ready: %w", name, waitErr)
	}

	return runFn(ctx, cluster)
}

// deleteEphemeralCluster tears down the cluster with a fresh bounded context.
// The outer context may already be cancelled by SIGINT/SIGTERM or runFn, but
// cleanup must still run.
func deleteEphemeralCluster(
	ctx context.Context,
	cmd *cobra.Command,
	provisioner clusterprovisioner.Provisioner,
	name string,
) error {
	deleteCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ephemeralDeleteTimeout)
	defer cancel()

	notify.Infof(cmd.OutOrStdout(), "tearing down ephemeral cluster %q...", name)

	deleteErr := provisioner.Delete(deleteCtx, name)
	if deleteErr != nil {
		return fmt.Errorf("delete ephemeral cluster %q: %w", name, deleteErr)
	}

	return nil
}

// ephemeralClusterName generates a unique, DNS-1123-safe name for a throwaway
// --ephemeral cluster so concurrent invocations never collide.
func ephemeralClusterName() string {
	return fmt.Sprintf("%s%d", ephemeralClusterNamePrefix, time.Now().UnixNano())
}
