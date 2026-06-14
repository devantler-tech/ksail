package talosprovisioner_test

import (
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
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

func TestProvisioner_PinnedDistributionVersion(t *testing.T) {
	t.Parallel()

	shippedDefault := clusterupdate.ExtractTag(talosconfigmanager.DefaultTalosImage)
	require.NotEmpty(t, shippedDefault)

	// Docker provider (no Hetzner/Omni options) with no explicit pin reconciles
	// toward the Talos version this KSail build ships (DefaultTalosImage), since
	// container-mode nodes cannot upgrade in place.
	assert.Equal(t, shippedDefault,
		talosprovisioner.NewProvisioner(nil, nil).PinnedDistributionVersion())

	// Hetzner/Omni upgrade in place, so an unset version follows the latest
	// discovered version → empty here.
	hetzner := talosprovisioner.NewProvisioner(nil, nil).
		WithHetznerOptions(v1alpha1.OptionsHetzner{})
	assert.Empty(t, hetzner.PinnedDistributionVersion())

	omni := talosprovisioner.NewProvisioner(nil, nil).
		WithOmniOptions(v1alpha1.OptionsOmni{})
	assert.Empty(t, omni.PinnedDistributionVersion())

	// An explicit pin applies to every provider and is trimmed of whitespace.
	pinned := talosprovisioner.NewProvisioner(nil, nil).
		WithTalosOptions(v1alpha1.OptionsTalos{Version: "  v1.13.3  "})
	assert.Equal(t, "v1.13.3", pinned.PinnedDistributionVersion())
}

func TestProvisioner_PinnedKubernetesVersion(t *testing.T) {
	t.Parallel()

	// Talos has no SDK-embedded Kubernetes pin; it follows OCI discovery /
	// spec.cluster.kubernetesVersion.
	assert.Empty(t, talosprovisioner.NewProvisioner(nil, nil).PinnedKubernetesVersion())
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
