//nolint:testpackage // White-box coverage locks cancellation of the setup phase.
package setup

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunWithReservedSandboxMonitorCancelsSetupAndPreservesDetectorError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	setupStarted := make(chan struct{})
	setupCanceled := make(chan struct{})
	detectorErr := &k8s.RepeatedReservedPodSandboxError{
		Namespace: "argocd",
		Pod:       "argocd-server-0",
		Count:     4,
		Message:   "failed to reserve sandbox name",
	}
	factories := &InstallerFactories{
		ReservedSandboxMonitor: func(context.Context, *v1alpha1.Cluster) error {
			<-setupStarted

			return detectorErr
		},
	}

	err := runWithReservedSandboxMonitor(
		ctx,
		k3sDockerGitOpsCluster(),
		factories,
		func(runCtx context.Context) error {
			close(setupStarted)
			<-runCtx.Done()
			close(setupCanceled)

			return fmt.Errorf("setup cancelled: %w", runCtx.Err())
		},
	)

	require.ErrorIs(t, err, k8s.ErrRepeatedReservedPodSandbox)
	require.ErrorIs(t, err, detectorErr)

	select {
	case <-setupCanceled:
	default:
		t.Fatal("setup did not observe monitor cancellation")
	}
}

func TestRunWithReservedSandboxMonitorJoinsMonitorAfterSuccessfulSetup(t *testing.T) {
	t.Parallel()

	monitorStarted := make(chan struct{})
	monitorStopped := make(chan struct{})
	factories := &InstallerFactories{
		ReservedSandboxMonitor: func(ctx context.Context, _ *v1alpha1.Cluster) error {
			close(monitorStarted)
			<-ctx.Done()
			close(monitorStopped)

			return nil
		},
	}

	err := runWithReservedSandboxMonitor(
		context.Background(),
		k3sDockerGitOpsCluster(),
		factories,
		func(context.Context) error {
			<-monitorStarted

			return nil
		},
	)

	require.NoError(t, err)

	select {
	case <-monitorStopped:
	default:
		t.Fatal("successful setup returned before the monitor stopped")
	}
}

func TestRunWithReservedSandboxMonitorPrioritizesConcurrentDetectorError(t *testing.T) {
	t.Parallel()

	detectorErr := &k8s.RepeatedReservedPodSandboxError{
		Namespace: "argocd",
		Pod:       "argocd-server-0",
		Count:     3,
		Message:   "failed to reserve sandbox name",
	}
	factories := &InstallerFactories{
		ReservedSandboxMonitor: func(ctx context.Context, _ *v1alpha1.Cluster) error {
			<-ctx.Done()

			return detectorErr
		},
	}

	err := runWithReservedSandboxMonitor(
		context.Background(),
		k3sDockerGitOpsCluster(),
		factories,
		func(context.Context) error {
			return assert.AnError
		},
	)

	require.ErrorIs(t, err, k8s.ErrRepeatedReservedPodSandbox)
}

//nolint:funlen // The table documents every cluster-shape guard in one place.
func TestRunWithReservedSandboxMonitorSkipsOtherClusterShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cluster *v1alpha1.Cluster
	}{
		{
			name: "Vanilla Docker GitOps",
			cluster: clusterWithShape(
				v1alpha1.DistributionVanilla,
				v1alpha1.ProviderDocker,
				v1alpha1.GitOpsEngineArgoCD,
			),
		},
		{
			name: "K3s non-Docker GitOps",
			cluster: clusterWithShape(
				v1alpha1.DistributionK3s,
				v1alpha1.ProviderHetzner,
				v1alpha1.GitOpsEngineArgoCD,
			),
		},
		{
			name: "K3s Docker without GitOps",
			cluster: clusterWithShape(
				v1alpha1.DistributionK3s,
				v1alpha1.ProviderDocker,
				v1alpha1.GitOpsEngineNone,
			),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var (
				monitorCalled bool
				setupCalled   bool
			)

			factories := &InstallerFactories{
				ReservedSandboxMonitor: func(context.Context, *v1alpha1.Cluster) error {
					monitorCalled = true

					return nil
				},
			}

			err := runWithReservedSandboxMonitor(
				context.Background(),
				test.cluster,
				factories,
				func(context.Context) error {
					setupCalled = true

					return nil
				},
			)

			require.NoError(t, err)
			assert.True(t, setupCalled)
			assert.False(t, monitorCalled)
		})
	}
}

func k3sDockerGitOpsCluster() *v1alpha1.Cluster {
	return clusterWithShape(
		v1alpha1.DistributionK3s,
		v1alpha1.ProviderDocker,
		v1alpha1.GitOpsEngineArgoCD,
	)
}

func clusterWithShape(
	distribution v1alpha1.Distribution,
	provider v1alpha1.Provider,
	gitOpsEngine v1alpha1.GitOpsEngine,
) *v1alpha1.Cluster {
	return &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: distribution,
				Provider:     provider,
				GitOpsEngine: gitOpsEngine,
			},
		},
	}
}
