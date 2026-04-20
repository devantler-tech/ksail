package hetzner_test

import (
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	hetzner "github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/hetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInstaller_FieldsPopulated(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	cfg := hetzner.ChartConfig{
		Name:        "test-component",
		ReleaseName: "test-release",
		ChartName:   "hcloud/test-chart",
		Version:     "1.0.0",
	}

	installer := hetzner.NewInstaller(
		client,
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		cfg,
	)

	require.NotNil(t, installer)
}

func TestInstaller_Install_TokenNotSet(t *testing.T) {
	t.Setenv(hetzner.TokenEnvVar, "")

	client := helm.NewMockInterface(t)
	cfg := hetzner.ChartConfig{
		Name:        "test",
		ReleaseName: "test-release",
		ChartName:   "hcloud/test-chart",
		Version:     "1.0.0",
	}

	installer := hetzner.NewInstaller(
		client,
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		cfg,
	)

	err := installer.Install(t.Context())
	require.Error(t, err)
	assert.ErrorIs(t, err, hetzner.ErrTokenNotSet)
}

func TestEnsureSecret_InvalidKubeconfig(t *testing.T) {
	t.Setenv(hetzner.TokenEnvVar, "test-token-value")

	err := hetzner.EnsureSecret(t.Context(), "/nonexistent/kubeconfig", "test-context")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create kubernetes client")
}
