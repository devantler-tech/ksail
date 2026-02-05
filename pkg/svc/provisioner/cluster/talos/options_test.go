package talosprovisioner_test

import (
	"path/filepath"
	"testing"

	talos "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOptions_DefaultValues(t *testing.T) {
	t.Parallel()
	opts := talosprovisioner.NewOptions()
	require.NotNil(t, opts)
	assert.Equal(t, talos.DefaultTalosImage, opts.TalosImage)
	assert.Equal(t, talosprovisioner.DefaultControlPlaneNodes, opts.ControlPlaneNodes)
	assert.Equal(t, talosprovisioner.DefaultWorkerNodes, opts.WorkerNodes)
	assert.Equal(t, talos.DefaultNetworkCIDR, opts.NetworkCIDR)
	assert.Empty(t, opts.KubeconfigPath)
	assert.Empty(t, opts.TalosconfigPath)
	assert.False(t, opts.SkipCNIChecks)
}

func TestOptions_WithTalosImage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		image    string
		expected string
	}{
		{"sets custom image", "ghcr.io/siderolabs/talos:v1.8.0", "ghcr.io/siderolabs/talos:v1.8.0"},
		{"ignores empty string", "", talos.DefaultTalosImage},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := talosprovisioner.NewOptions().WithTalosImage(tc.image)
			assert.Equal(t, tc.expected, opts.TalosImage)
		})
	}
}

func TestOptions_WithControlPlaneNodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		count    int
		expected int
	}{
		{"sets positive count", 3, 3},
		{"rejects zero", 0, talosprovisioner.DefaultControlPlaneNodes},
		{"rejects negative", -1, talosprovisioner.DefaultControlPlaneNodes},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := talosprovisioner.NewOptions().WithControlPlaneNodes(tc.count)
			assert.Equal(t, tc.expected, opts.ControlPlaneNodes)
		})
	}
}

func TestOptions_WithWorkerNodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		count    int
		expected int
	}{
		{"sets positive count", 5, 5},
		{"allows zero", 0, 0},
		{"rejects negative", -1, talosprovisioner.DefaultWorkerNodes},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := talosprovisioner.NewOptions().WithWorkerNodes(tc.count)
			assert.Equal(t, tc.expected, opts.WorkerNodes)
		})
	}
}

func TestOptions_WithNetworkCIDR(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		cidr     string
		expected string
	}{
		{"sets custom CIDR", "192.168.0.0/16", "192.168.0.0/16"},
		{"ignores empty string", "", talos.DefaultNetworkCIDR},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := talosprovisioner.NewOptions().WithNetworkCIDR(tc.cidr)
			assert.Equal(t, tc.expected, opts.NetworkCIDR)
		})
	}
}

func TestOptions_WithKubeconfigPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"sets path", "/tmp/kubeconfig", "/tmp/kubeconfig"},
		{"ignores empty string", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := talosprovisioner.NewOptions().WithKubeconfigPath(tc.path)
			assert.Equal(t, tc.expected, opts.KubeconfigPath)
		})
	}
}

func TestOptions_WithTalosconfigPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"sets path", "/tmp/talosconfig", "/tmp/talosconfig"},
		{"ignores empty string", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := talosprovisioner.NewOptions().WithTalosconfigPath(tc.path)
			assert.Equal(t, tc.expected, opts.TalosconfigPath)
		})
	}
}

func TestOptions_WithSkipCNIChecks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		skip     bool
		expected bool
	}{
		{"enables skip", true, true},
		{"disables skip", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := talosprovisioner.NewOptions().WithSkipCNIChecks(tc.skip)
			assert.Equal(t, tc.expected, opts.SkipCNIChecks)
		})
	}
}

func TestOptions_Chaining(t *testing.T) {
	t.Parallel()
	opts := talosprovisioner.NewOptions().
		WithTalosImage("custom:v1.0").
		WithControlPlaneNodes(3).
		WithWorkerNodes(2).
		WithNetworkCIDR("10.0.0.0/8").
		WithKubeconfigPath("/tmp/kc").
		WithTalosconfigPath("/tmp/tc").
		WithSkipCNIChecks(true)
	assert.Equal(t, "custom:v1.0", opts.TalosImage)
	assert.Equal(t, 3, opts.ControlPlaneNodes)
	assert.Equal(t, 2, opts.WorkerNodes)
	assert.Equal(t, "10.0.0.0/8", opts.NetworkCIDR)
	assert.Equal(t, "/tmp/kc", opts.KubeconfigPath)
	assert.Equal(t, "/tmp/tc", opts.TalosconfigPath)
	assert.True(t, opts.SkipCNIChecks)
}

func TestNewPatchDirs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		patchesDir      string
		expectedRoot    string
		expectedCluster string
		expectedCP      string
		expectedWorkers string
	}{
		{
			"uses default when empty",
			"",
			talos.DefaultPatchesDir,
			filepath.Join(talos.DefaultPatchesDir, "cluster"),
			filepath.Join(talos.DefaultPatchesDir, "control-planes"),
			filepath.Join(talos.DefaultPatchesDir, "workers"),
		},
		{
			"uses custom directory",
			"my-patches",
			"my-patches",
			filepath.Join("my-patches", "cluster"),
			filepath.Join("my-patches", "control-planes"),
			filepath.Join("my-patches", "workers"),
		},
		{
			"handles nested path",
			"config/talos/patches",
			"config/talos/patches",
			filepath.Join("config/talos/patches", "cluster"),
			filepath.Join("config/talos/patches", "control-planes"),
			filepath.Join("config/talos/patches", "workers"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dirs := talosprovisioner.NewPatchDirs(tc.patchesDir)
			assert.Equal(t, tc.expectedRoot, dirs.Root)
			assert.Equal(t, tc.expectedCluster, dirs.Cluster)
			assert.Equal(t, tc.expectedCP, dirs.ControlPlanes)
			assert.Equal(t, tc.expectedWorkers, dirs.Workers)
		})
	}
}
