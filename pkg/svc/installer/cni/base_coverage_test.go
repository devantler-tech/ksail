package cni_test

import (
	"context"
	"errors"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errCNIAPIServerUnstable          = errors.New("api server unstable")
	errCNIAPIServerConnectionRefused = errors.New("api server connection refused")
)

func TestNewInstallerBase_Fields(t *testing.T) {
	t.Parallel()

	helmMock := helm.NewMockInterface(t)
	base := cni.NewInstallerBase(
		helmMock,
		"/path/to/kubeconfig",
		"my-context",
		10*time.Minute,
	)

	require.NotNil(t, base)
	assert.Equal(t, "/path/to/kubeconfig", base.GetKubeconfig())
	assert.Equal(t, "my-context", base.GetContext())
	assert.Equal(t, 10*time.Minute, base.GetTimeout())
}

func TestNewInstallerBase_NilClient(t *testing.T) {
	t.Parallel()

	base := cni.NewInstallerBase(nil, "", "", 0)

	require.NotNil(t, base)
	assert.Empty(t, base.GetKubeconfig())
	assert.Empty(t, base.GetContext())
	assert.Equal(t, time.Duration(0), base.GetTimeout())
}

func TestInstallerBase_WaitForReadiness_Noop(t *testing.T) {
	t.Parallel()

	base := cni.NewInstallerBase(helm.NewMockInterface(t), "", "", time.Second)

	err := base.WaitForReadiness(context.Background())

	require.NoError(t, err, "WaitForReadiness should be a no-op and return nil")
}

func TestInstallerBase_GetClient_Success(t *testing.T) {
	t.Parallel()

	helmMock := helm.NewMockInterface(t)
	base := cni.NewInstallerBase(helmMock, "", "", time.Second)

	client, err := base.GetClient()

	require.NoError(t, err)
	assert.Equal(t, helmMock, client)
}

func TestInstallerBase_GetClient_NilReturnsError(t *testing.T) {
	t.Parallel()

	base := cni.NewInstallerBase(nil, "", "", time.Second)

	client, err := base.GetClient()

	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "helm client is nil")
}

func TestInstallerBase_GetTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{"zero", 0},
		{"one_second", time.Second},
		{"five_minutes", 5 * time.Minute},
		{"one_hour", time.Hour},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			base := cni.NewInstallerBase(helm.NewMockInterface(t), "", "", testCase.timeout)

			assert.Equal(t, testCase.timeout, base.GetTimeout())
		})
	}
}

func TestInstallerBase_GetKubeconfig(t *testing.T) {
	t.Parallel()

	base := cni.NewInstallerBase(helm.NewMockInterface(t), "/custom/path", "", time.Second)

	assert.Equal(t, "/custom/path", base.GetKubeconfig())
}

func TestInstallerBase_GetContext(t *testing.T) {
	t.Parallel()

	base := cni.NewInstallerBase(helm.NewMockInterface(t), "", "custom-context", time.Second)

	assert.Equal(t, "custom-context", base.GetContext())
}

func TestInstallerBase_RunAPIServerCheck_ShouldCheckFalse(t *testing.T) {
	t.Parallel()

	base := cni.NewInstallerBase(helm.NewMockInterface(t), "", "", time.Second)

	err := base.RunAPIServerCheck(context.Background(), false, nil)

	require.NoError(t, err, "should return nil when shouldCheck is false")
}

func TestInstallerBase_RunAPIServerCheck_ShouldCheckTrue_NilChecker(t *testing.T) {
	t.Parallel()

	base := cni.NewInstallerBase(helm.NewMockInterface(t), "", "", time.Second)

	err := base.RunAPIServerCheck(context.Background(), true, nil)

	require.Error(t, err, "should error when checker is nil and shouldCheck is true")
	assert.Contains(t, err.Error(), "api server checker is not configured")
}

func TestInstallerBase_RunAPIServerCheck_ShouldCheckTrue_CheckerSucceeds(t *testing.T) {
	t.Parallel()

	base := cni.NewInstallerBase(helm.NewMockInterface(t), "", "", time.Second)

	checker := func(_ context.Context) error {
		return nil
	}

	err := base.RunAPIServerCheck(context.Background(), true, checker)

	require.NoError(t, err)
}

func TestInstallerBase_RunAPIServerCheck_ShouldCheckTrue_CheckerFails(t *testing.T) {
	t.Parallel()

	base := cni.NewInstallerBase(helm.NewMockInterface(t), "", "", time.Second)

	checkerErr := errCNIAPIServerUnstable
	checker := func(_ context.Context) error {
		return checkerErr
	}

	err := base.RunAPIServerCheck(context.Background(), true, checker)

	require.Error(t, err)
	require.ErrorIs(t, err, checkerErr)
	assert.Contains(t, err.Error(), "failed to wait for API server stability")
}

func TestInstallerBase_PrepareInstall_NilClient(t *testing.T) {
	t.Parallel()

	base := cni.NewInstallerBase(nil, "", "", time.Second)

	err := base.PrepareInstall(context.Background(), v1alpha1.DistributionVanilla, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "get helm client")
}

func TestInstallerBase_PrepareInstall_VanillaSkipsAPICheck(t *testing.T) {
	t.Parallel()

	base := cni.NewInstallerBase(helm.NewMockInterface(t), "", "", time.Second)

	// Vanilla should not trigger an API server check, so nil checker is fine.
	err := base.PrepareInstall(context.Background(), v1alpha1.DistributionVanilla, nil)

	require.NoError(t, err)
}

func TestInstallerBase_PrepareInstall_TalosRequiresChecker(t *testing.T) {
	t.Parallel()

	base := cni.NewInstallerBase(helm.NewMockInterface(t), "", "", time.Second)

	err := base.PrepareInstall(context.Background(), v1alpha1.DistributionTalos, nil)

	require.Error(t, err, "Talos needs an API server check, nil checker should error")
	assert.Contains(t, err.Error(), "api server checker is not configured")
}

func TestInstallerBase_PrepareInstall_K3sRequiresChecker(t *testing.T) {
	t.Parallel()

	base := cni.NewInstallerBase(helm.NewMockInterface(t), "", "", time.Second)

	err := base.PrepareInstall(context.Background(), v1alpha1.DistributionK3s, nil)

	require.Error(t, err, "K3s needs an API server check, nil checker should error")
	assert.Contains(t, err.Error(), "api server checker is not configured")
}

func TestInstallerBase_PrepareInstall_TalosWithChecker(t *testing.T) {
	t.Parallel()

	base := cni.NewInstallerBase(helm.NewMockInterface(t), "", "", time.Second)

	checker := func(_ context.Context) error { return nil }

	err := base.PrepareInstall(context.Background(), v1alpha1.DistributionTalos, checker)

	require.NoError(t, err)
}

func TestInstallerBase_PrepareInstall_K3sWithChecker(t *testing.T) {
	t.Parallel()

	base := cni.NewInstallerBase(helm.NewMockInterface(t), "", "", time.Second)

	checker := func(_ context.Context) error { return nil }

	err := base.PrepareInstall(context.Background(), v1alpha1.DistributionK3s, checker)

	require.NoError(t, err)
}

func TestInstallerBase_PrepareInstall_CheckerError(t *testing.T) {
	t.Parallel()

	base := cni.NewInstallerBase(helm.NewMockInterface(t), "", "", time.Second)

	checkerErr := errCNIAPIServerConnectionRefused
	checker := func(_ context.Context) error { return checkerErr }

	err := base.PrepareInstall(context.Background(), v1alpha1.DistributionTalos, checker)

	require.Error(t, err)
	require.ErrorIs(t, err, checkerErr)
	assert.Contains(t, err.Error(), "run API server check")
}

func TestAPIServerConstants(t *testing.T) {
	t.Parallel()

	t.Run("stability_timeout", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 60*time.Second, cni.APIServerStabilityTimeout)
	})

	t.Run("required_successes", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 3, cni.APIServerRequiredSuccesses)
	})
}
