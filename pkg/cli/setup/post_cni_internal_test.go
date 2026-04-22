package setup

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errNotStable        = errors.New("not stable")
	errApproverNotReady = errors.New("approver not ready")
)

func TestRunGitOpsPhase_AlwaysChecksClusterStabilityBeforeInstallingGitOps(t *testing.T) {
	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
			},
		},
	}

	var (
		stabilityChecked bool
		cniInstalledArg  bool
		taskRan          bool
		order            []string
	)

	t.Cleanup(SetClusterStabilityCheckForTests(
		func(_ context.Context, _ *v1alpha1.Cluster, cniInstalled bool) error {
			stabilityChecked = true
			cniInstalledArg = cniInstalled

			order = append(order, "stability-check")

			return nil
		},
	))

	gitopsTasks := []notify.ProgressTask{
		{
			Name: "flux",
			Fn: func(context.Context) error {
				taskRan = true

				order = append(order, "gitops-install")

				return nil
			},
		},
	}

	tmr := timer.New()
	err := runGitOpsPhase(
		context.Background(),
		clusterCfg,
		io.Discard,
		notify.InstallingLabels(),
		tmr,
		nil,
		gitopsTasks,
	)
	require.NoError(t, err)
	assert.True(t, stabilityChecked)
	assert.False(t, cniInstalledArg, "GitOps phase must run full stability check")
	assert.True(t, taskRan)
	assert.Equal(t, []string{"stability-check", "gitops-install"}, order)
}

func TestRunGitOpsPhase_ReturnsErrorBeforeGitOpsInstallWhenStabilityCheckFails(t *testing.T) {
	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineArgoCD,
			},
		},
	}

	var taskRan bool

	t.Cleanup(SetClusterStabilityCheckForTests(
		func(context.Context, *v1alpha1.Cluster, bool) error {
			return errNotStable
		},
	))

	gitopsTasks := []notify.ProgressTask{
		{
			Name: "argocd",
			Fn: func(context.Context) error {
				taskRan = true

				return nil
			},
		},
	}

	tmr := timer.New()
	err := runGitOpsPhase(
		context.Background(),
		clusterCfg,
		io.Discard,
		notify.InstallingLabels(),
		tmr,
		nil,
		gitopsTasks,
	)
	require.Error(t, err)
	require.ErrorContains(t, err, "cluster not stable before GitOps installation")
	assert.False(t, taskRan, "GitOps installation must not start when stability check fails")
}

func TestRunInfraPhase_WaitsForCSRApproverBeforeInfraTasks(t *testing.T) {
	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:  v1alpha1.DistributionTalos,
				MetricsServer: v1alpha1.MetricsServerEnabled,
			},
		},
	}

	var (
		approverWaitCalled bool
		taskRan            bool
		order              []string
	)

	t.Cleanup(SetClusterStabilityCheckForTests(
		func(context.Context, *v1alpha1.Cluster, bool) error {
			return nil
		},
	))

	t.Cleanup(SetCSRApproverWaitForTests(
		func(_ context.Context, _ *v1alpha1.Cluster) error {
			approverWaitCalled = true

			order = append(order, "csr-approver-wait")

			return nil
		},
	))

	infraTasks := []notify.ProgressTask{
		{
			Name: "metrics-server",
			Fn: func(context.Context) error {
				taskRan = true

				order = append(order, "infra-install")

				return nil
			},
		},
	}

	tmr := timer.New()
	err := runInfraPhase(
		context.Background(),
		clusterCfg,
		io.Discard,
		notify.InstallingLabels(),
		tmr,
		infraTasks,
		false,
		true,
	)
	require.NoError(t, err)
	assert.True(t, approverWaitCalled, "CSR approver wait should be called")
	assert.True(t, taskRan, "infrastructure tasks should run after approver wait")
	assert.Equal(t, []string{"csr-approver-wait", "infra-install"}, order,
		"CSR approver wait must happen before infrastructure tasks")
}

func TestRunInfraPhase_ReturnsErrorWhenCSRApproverWaitFails(t *testing.T) {
	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:  v1alpha1.DistributionTalos,
				MetricsServer: v1alpha1.MetricsServerEnabled,
			},
		},
	}

	var taskRan bool

	t.Cleanup(SetClusterStabilityCheckForTests(
		func(context.Context, *v1alpha1.Cluster, bool) error {
			return nil
		},
	))

	t.Cleanup(SetCSRApproverWaitForTests(
		func(context.Context, *v1alpha1.Cluster) error {
			return errApproverNotReady
		},
	))

	infraTasks := []notify.ProgressTask{
		{
			Name: "metrics-server",
			Fn: func(context.Context) error {
				taskRan = true

				return nil
			},
		},
	}

	tmr := timer.New()
	err := runInfraPhase(
		context.Background(),
		clusterCfg,
		io.Discard,
		notify.InstallingLabels(),
		tmr,
		infraTasks,
		false,
		true,
	)
	require.Error(t, err)
	require.ErrorContains(t, err, "kubelet CSR approver not ready")
	assert.False(t, taskRan, "infrastructure tasks must not start when CSR approver wait fails")
}

func TestRunInfraPhase_SkipsCSRApproverWaitWhenNotNeeded(t *testing.T) {
	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:  v1alpha1.DistributionVanilla,
				MetricsServer: v1alpha1.MetricsServerEnabled,
			},
		},
	}

	var approverWaitCalled bool

	t.Cleanup(SetClusterStabilityCheckForTests(
		func(context.Context, *v1alpha1.Cluster, bool) error {
			return nil
		},
	))

	t.Cleanup(SetCSRApproverWaitForTests(
		func(context.Context, *v1alpha1.Cluster) error {
			approverWaitCalled = true

			return nil
		},
	))

	infraTasks := []notify.ProgressTask{
		{
			Name: "metrics-server",
			Fn:   func(context.Context) error { return nil },
		},
	}

	tmr := timer.New()
	err := runInfraPhase(
		context.Background(),
		clusterCfg,
		io.Discard,
		notify.InstallingLabels(),
		tmr,
		infraTasks,
		false,
		false,
	)
	require.NoError(t, err)
	assert.False(t, approverWaitCalled,
		"CSR approver wait should NOT be called when needsCSRApproverWait is false")
}
