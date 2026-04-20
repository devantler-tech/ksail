package talosprovisioner_test

import (
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talos "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
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
	assert.Empty(t, opts.KubeconfigContext)
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
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			opts := talosprovisioner.NewOptions().WithTalosImage(testCase.image)

			assert.Equal(t, testCase.expected, opts.TalosImage)
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

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			opts := talosprovisioner.NewOptions().WithControlPlaneNodes(testCase.count)

			assert.Equal(t, testCase.expected, opts.ControlPlaneNodes)
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
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			opts := talosprovisioner.NewOptions().WithWorkerNodes(testCase.count)

			assert.Equal(t, testCase.expected, opts.WorkerNodes)
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
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			opts := talosprovisioner.NewOptions().WithNetworkCIDR(testCase.cidr)

			assert.Equal(t, testCase.expected, opts.NetworkCIDR)
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
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			opts := talosprovisioner.NewOptions().WithKubeconfigPath(testCase.path)

			assert.Equal(t, testCase.expected, opts.KubeconfigPath)
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
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			opts := talosprovisioner.NewOptions().WithTalosconfigPath(testCase.path)

			assert.Equal(t, testCase.expected, opts.TalosconfigPath)
		})
	}
}

func TestOptions_WithKubeconfigContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		context  string
		expected string
	}{
		{"sets context", "admin@my-cluster", "admin@my-cluster"},
		{"sets custom context", "devantler-dev", "devantler-dev"},
		{"allows empty string", "", ""},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			opts := talosprovisioner.NewOptions().WithKubeconfigContext(testCase.context)

			assert.Equal(t, testCase.expected, opts.KubeconfigContext)
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
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			opts := talosprovisioner.NewOptions().WithSkipCNIChecks(testCase.skip)

			assert.Equal(t, testCase.expected, opts.SkipCNIChecks)
		})
	}
}

func TestOptions_WithExtraPortMappings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ports    []string
		expected []string
	}{
		{
			"sets port mappings",
			[]string{"8080:80/tcp", "8443:443/tcp"},
			[]string{"8080:80/tcp", "8443:443/tcp"},
		},
		{"sets nil", nil, nil},
		{"sets empty", []string{}, []string{}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			opts := talosprovisioner.NewOptions().WithExtraPortMappings(testCase.ports)

			assert.Equal(t, testCase.expected, opts.ExtraPortMappings)
		})
	}
}

func TestPortMappingsToStrings_ValidMappings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mappings []v1alpha1.PortMapping
		expected []string
	}{
		{
			"converts TCP mappings",
			[]v1alpha1.PortMapping{
				{ContainerPort: 80, HostPort: 8080, Protocol: "TCP"},
				{ContainerPort: 443, HostPort: 8443, Protocol: "TCP"},
			},
			[]string{"8080:80/tcp", "8443:443/tcp"},
		},
		{
			"converts UDP mappings",
			[]v1alpha1.PortMapping{
				{ContainerPort: 53, HostPort: 5353, Protocol: "UDP"},
			},
			[]string{"5353:53/udp"},
		},
		{
			"defaults protocol to tcp",
			[]v1alpha1.PortMapping{
				{ContainerPort: 80, HostPort: 8080},
			},
			[]string{"8080:80/tcp"},
		},
		{
			"uses zero host port when not specified",
			[]v1alpha1.PortMapping{
				{ContainerPort: 80},
			},
			[]string{"0:80/tcp"},
		},
		{
			"returns nil for empty input",
			nil,
			nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := talosprovisioner.PortMappingsToStrings(testCase.mappings)
			require.NoError(t, err)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestPortMappingsToStrings_InvalidMappings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mappings []v1alpha1.PortMapping
		wantErr  error
	}{
		{
			"rejects zero container port",
			[]v1alpha1.PortMapping{
				{ContainerPort: 0, HostPort: 8080, Protocol: "TCP"},
			},
			talosprovisioner.ErrContainerPortOutOfRange,
		},
		{
			"rejects negative container port",
			[]v1alpha1.PortMapping{
				{ContainerPort: -1, HostPort: 8080, Protocol: "TCP"},
			},
			talosprovisioner.ErrContainerPortOutOfRange,
		},
		{
			"rejects container port over 65535",
			[]v1alpha1.PortMapping{
				{ContainerPort: 70000, HostPort: 8080, Protocol: "TCP"},
			},
			talosprovisioner.ErrContainerPortOutOfRange,
		},
		{
			"rejects invalid protocol",
			[]v1alpha1.PortMapping{
				{ContainerPort: 80, HostPort: 8080, Protocol: "SCTP"},
			},
			talosprovisioner.ErrInvalidProtocol,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := talosprovisioner.PortMappingsToStrings(testCase.mappings)
			require.ErrorIs(t, err, testCase.wantErr)
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
		WithKubeconfigContext("admin@test").
		WithTalosconfigPath("/tmp/tc").
		WithSkipCNIChecks(true).
		WithExtraPortMappings([]string{"8080:80/tcp"})
	assert.Equal(t, "custom:v1.0", opts.TalosImage)
	assert.Equal(t, 3, opts.ControlPlaneNodes)
	assert.Equal(t, 2, opts.WorkerNodes)
	assert.Equal(t, "10.0.0.0/8", opts.NetworkCIDR)
	assert.Equal(t, "/tmp/kc", opts.KubeconfigPath)
	assert.Equal(t, "admin@test", opts.KubeconfigContext)
	assert.Equal(t, "/tmp/tc", opts.TalosconfigPath)
	assert.True(t, opts.SkipCNIChecks)
	assert.Equal(t, []string{"8080:80/tcp"}, opts.ExtraPortMappings)
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
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dirs := talosprovisioner.NewPatchDirs(testCase.patchesDir)

			assert.Equal(t, testCase.expectedRoot, dirs.Root)
			assert.Equal(t, testCase.expectedCluster, dirs.Cluster)
			assert.Equal(t, testCase.expectedCP, dirs.ControlPlanes)
			assert.Equal(t, testCase.expectedWorkers, dirs.Workers)
		})
	}
}
