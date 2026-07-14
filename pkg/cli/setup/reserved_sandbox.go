package setup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
)

const (
	reservedSandboxPollInterval        = 5 * time.Second
	reservedSandboxShutdownGracePeriod = 5 * time.Second
)

func (f *InstallerFactories) reservedSandboxMonitor() func(
	context.Context,
	*v1alpha1.Cluster,
) error {
	if f != nil && f.ReservedSandboxMonitor != nil {
		return f.ReservedSandboxMonitor
	}

	return watchRepeatedReservedPodSandboxes
}

func watchRepeatedReservedPodSandboxes(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
) error {
	kubeconfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(clusterCfg)
	if err != nil {
		return fmt.Errorf("resolve kubeconfig for pod sandbox monitor: %w", err)
	}

	clientset, err := k8s.NewClientset(
		kubeconfigPath,
		clusterCfg.Spec.Cluster.Connection.Context,
	)
	if err != nil {
		return fmt.Errorf("create client for pod sandbox monitor: %w", err)
	}

	err = k8s.WatchRepeatedReservedPodSandboxes(
		ctx,
		clientset,
		reservedSandboxPollInterval,
	)
	if err != nil {
		return fmt.Errorf("watch for repeated reserved pod sandboxes: %w", err)
	}

	return nil
}

func runWithReservedSandboxMonitor(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	setup func(context.Context) error,
) error {
	if !needsReservedSandboxMonitor(clusterCfg) {
		return setup(ctx)
	}

	runCtx, stop := context.WithCancel(ctx)
	defer stop()

	setupResult := make(chan error, 1)
	monitorResult := make(chan error, 1)

	go func() {
		setupResult <- setup(runCtx)
	}()
	go func() {
		monitorResult <- factories.reservedSandboxMonitor()(runCtx, clusterCfg)
	}()

	select {
	case setupErr := <-setupResult:
		stop()

		stopped, monitorErr := waitForReservedSandboxResult(monitorResult)
		if stopped && errors.Is(monitorErr, k8s.ErrRepeatedReservedPodSandbox) {
			return monitorErr
		}

		return setupErr
	case monitorErr := <-monitorResult:
		if errors.Is(monitorErr, k8s.ErrRepeatedReservedPodSandbox) {
			stop()
			// The detector error controls retry classification; the cancelled
			// setup result is secondary and is joined only for bounded shutdown.
			_, _ = waitForReservedSandboxResult(setupResult)

			return monitorErr
		}

		// Losing the diagnostic event stream must not abort healthy setup.
		return <-setupResult
	}
}

func waitForReservedSandboxResult(result <-chan error) (bool, error) {
	timer := time.NewTimer(reservedSandboxShutdownGracePeriod)
	defer timer.Stop()

	select {
	case err := <-result:
		return true, err
	case <-timer.C:
		return false, nil
	}
}

func needsReservedSandboxMonitor(clusterCfg *v1alpha1.Cluster) bool {
	if clusterCfg == nil ||
		clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionK3s ||
		clusterCfg.Spec.Cluster.Provider != v1alpha1.ProviderDocker {
		return false
	}

	gitOpsEngine := clusterCfg.Spec.Cluster.GitOpsEngine

	return gitOpsEngine == v1alpha1.GitOpsEngineArgoCD ||
		gitOpsEngine == v1alpha1.GitOpsEngineFlux
}
