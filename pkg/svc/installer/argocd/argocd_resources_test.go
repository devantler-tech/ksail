package argocdinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	argocdinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/argocd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureDefaultResources_InvalidKubeconfig(t *testing.T) {
	t.Parallel()

	err := argocdinstaller.EnsureDefaultResources(
		context.Background(),
		"/nonexistent/path/kubeconfig",
		5*time.Minute,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "build rest config")
}

func TestEnsureDefaultResources_EmptyKubeconfig(t *testing.T) {
	t.Parallel()

	err := argocdinstaller.EnsureDefaultResources(
		context.Background(),
		"",
		5*time.Minute,
	)

	require.Error(t, err)
}

func TestEnsureSopsAgeSecret_EnabledWithKey_InvalidKubeconfig(t *testing.T) {
	const testKey = "AGE-SECRET-KEY-1TESTKEY000000000000000000000000000000000000000000000000"
	t.Setenv("TEST_ARGOCD_SOPS_AGE_KEY_KUBECONFIG", testKey)

	enabled := true
	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				SOPS: v1alpha1.SOPS{
					Enabled:      &enabled,
					AgeKeyEnvVar: "TEST_ARGOCD_SOPS_AGE_KEY_KUBECONFIG",
				},
			},
		},
	}

	err := argocdinstaller.EnsureSopsAgeSecret(
		context.Background(),
		"/nonexistent/kubeconfig",
		clusterCfg,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build REST config")
}
