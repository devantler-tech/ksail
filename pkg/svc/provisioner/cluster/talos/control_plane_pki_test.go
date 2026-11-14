package talosprovisioner_test

import (
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/siderolabs/talos/pkg/machinery/config/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A Talos 1.14 multi-document config can omit the KubeAPIServerCAConfig document, and its
// accessor then returns nil. The PKI check must report "no PKI" instead of panicking.
func TestHasControlPlanePKI_NoCADocument(t *testing.T) {
	t.Parallel()

	provider, err := container.New()
	require.NoError(t, err)
	require.Nil(t, provider.K8sAPIServerCAConfig())

	assert.NotPanics(t, func() {
		assert.False(t, talosprovisioner.HasControlPlanePKIForTest(provider))
	})
}

func TestHasControlPlanePKI_NilProvider(t *testing.T) {
	t.Parallel()

	assert.False(t, talosprovisioner.HasControlPlanePKIForTest(nil))
}
