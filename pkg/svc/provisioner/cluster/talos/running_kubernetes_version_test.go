package talosprovisioner_test

import (
	"context"
	"errors"
	"testing"

	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errDetectorUnavailable = errors.New("kube-apiserver unreachable")

func runningVersionNodes() []talosprovisioner.NodeWithRoleForTest {
	// Workers are listed first, as getNodesByRole returns them.
	return []talosprovisioner.NodeWithRoleForTest{
		{IP: "10.5.0.3", Role: "worker"},
		{IP: "10.5.0.2", Role: talosprovisioner.RoleControlPlane},
	}
}

// TestGetLowestRunningKubernetesVersion_ReadsClusterNotConfig guards ksail#7412:
// after an unpinned `cluster update` upgrades Kubernetes, the rendered machine
// config still carries the version KSail first generated. Planning from it made
// every later update replay the whole upgrade path from that old version and fail
// on "unsupported upgrade path 1.37->1.35". The current version must come from
// the cluster.
func TestGetLowestRunningKubernetesVersion_ReadsClusterNotConfig(t *testing.T) {
	t.Parallel()

	configs, err := talosconfigmanager.NewDefaultConfigsWithPatches(nil)
	require.NoError(t, err)

	const running = "1.37.1"

	require.NotEqual(t, "v"+running, configs.KubernetesVersion(),
		"the test needs a config version that differs from the running cluster")

	var dialled string

	prov := talosprovisioner.NewProvisioner(configs, nil).
		WithKubernetesVersionDetectorForTest(func(_ context.Context, cpNodeIP string) (string, error) {
			dialled = cpNodeIP

			return running, nil
		})

	got, err := prov.GetLowestRunningKubernetesVersionForTest(t.Context(), runningVersionNodes())
	require.NoError(t, err)

	assert.Equal(t, "v1.37.1", got)
	assert.Equal(t, "10.5.0.2", dialled, "the version must be read through a control-plane node")
}

// TestGetLowestRunningKubernetesVersion_FailsClosed asserts that an undeterminable
// running version is an error, never a silent fallback to the config version.
func TestGetLowestRunningKubernetesVersion_FailsClosed(t *testing.T) {
	t.Parallel()

	configs, err := talosconfigmanager.NewDefaultConfigsWithPatches(nil)
	require.NoError(t, err)

	tests := []struct {
		name     string
		nodes    []talosprovisioner.NodeWithRoleForTest
		detected string
		detErr   error
		wantErr  error
	}{
		{
			name:    "detector error is surfaced",
			nodes:   runningVersionNodes(),
			detErr:  errDetectorUnavailable,
			wantErr: errDetectorUnavailable,
		},
		{
			name:     "empty detected version is undetermined",
			nodes:    runningVersionNodes(),
			detected: "  ",
			wantErr:  clustererr.ErrVersionUndetermined,
		},
		{
			name:    "no control-plane node to read through",
			nodes:   []talosprovisioner.NodeWithRoleForTest{{IP: "10.5.0.3", Role: "worker"}},
			wantErr: clustererr.ErrNoControlPlaneNodes,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			prov := talosprovisioner.NewProvisioner(configs, nil).
				WithKubernetesVersionDetectorForTest(func(context.Context, string) (string, error) {
					return testCase.detected, testCase.detErr
				})

			got, err := prov.GetLowestRunningKubernetesVersionForTest(t.Context(), testCase.nodes)
			require.ErrorIs(t, err, testCase.wantErr)
			assert.Empty(t, got)
		})
	}
}

// TestGetLowestRunningKubernetesVersion_KeepsPrefix asserts a detected version that
// already carries the "v" prefix is not double-prefixed.
func TestGetLowestRunningKubernetesVersion_KeepsPrefix(t *testing.T) {
	t.Parallel()

	prov := talosprovisioner.NewProvisioner(nil, nil).
		WithKubernetesVersionDetectorForTest(func(context.Context, string) (string, error) {
			return "v1.36.3", nil
		})

	got, err := prov.GetLowestRunningKubernetesVersionForTest(t.Context(), runningVersionNodes())
	require.NoError(t, err)
	assert.Equal(t, "v1.36.3", got)
}
