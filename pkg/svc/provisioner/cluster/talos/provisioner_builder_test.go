package talosprovisioner_test

import (
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProvisioner_NilOptions_DefaultsApplied(t *testing.T) {
	t.Parallel()

	p := talosprovisioner.NewProvisioner(nil, nil)
	require.NotNil(t, p)
}

func TestNewProvisioner_CustomOptions_Applied(t *testing.T) {
	t.Parallel()

	opts := talosprovisioner.NewOptions().
		WithControlPlaneNodes(3).
		WithWorkerNodes(2)

	p := talosprovisioner.NewProvisioner(nil, opts)
	require.NotNil(t, p)
}

func TestProvisioner_WithLogWriter(t *testing.T) {
	t.Parallel()

	p := talosprovisioner.NewProvisioner(nil, nil)
	result := p.WithLogWriter(io.Discard)

	// WithLogWriter should return the same provisioner (fluent API)
	assert.Same(t, p, result)
}

func TestProvisioner_WithHetznerOptions(t *testing.T) {
	t.Parallel()

	p := talosprovisioner.NewProvisioner(nil, nil)
	result := p.WithHetznerOptions(v1alpha1.OptionsHetzner{
		ControlPlaneServerType: "cpx21",
	})

	assert.Same(t, p, result)
}

func TestProvisioner_WithOmniOptions(t *testing.T) {
	t.Parallel()

	p := talosprovisioner.NewProvisioner(nil, nil)
	result := p.WithOmniOptions(v1alpha1.OptionsOmni{
		MachineClass: "test-class",
	})

	assert.Same(t, p, result)
}

func TestProvisioner_WithTalosOptions(t *testing.T) {
	t.Parallel()

	p := talosprovisioner.NewProvisioner(nil, nil)
	result := p.WithTalosOptions(v1alpha1.OptionsTalos{
		ControlPlanes: 5,
	})

	assert.Same(t, p, result)
}

func TestProvisioner_WithDockerClient(t *testing.T) {
	t.Parallel()

	p := talosprovisioner.NewProvisioner(nil, nil)
	result := p.WithDockerClient(nil)

	assert.Same(t, p, result)
}

func TestProvisioner_WithInfraProvider(t *testing.T) {
	t.Parallel()

	p := talosprovisioner.NewProvisioner(nil, nil)
	result := p.WithInfraProvider(nil)

	assert.Same(t, p, result)
}
