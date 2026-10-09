package talosprovisioner_test

import (
	"context"
	"io"
	"net"
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoscalerImageSelectionPreservesCompletedReplacement(t *testing.T) {
	t.Parallel()

	updated := &hcloud.Server{Name: "a-replacement", PrivateNet: []hcloud.ServerPrivateNet{
		{IP: net.ParseIP("10.0.0.2")},
	}}
	old := &hcloud.Server{Name: "b-old-worker", PrivateNet: []hcloud.ServerPrivateNet{
		{IP: net.ParseIP("10.0.0.3")},
	}}

	var checked []string

	selected, err := talosprovisioner.SelectAutoscalerImageServersForTest(
		t.Context(), []*hcloud.Server{old, updated},
		func(_ context.Context, ip string) (bool, error) {
			checked = append(checked, ip)

			return ip == "10.0.0.2", nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []*hcloud.Server{old}, selected)
	assert.ElementsMatch(t, []string{"10.0.0.2", "10.0.0.3"}, checked)
}

func TestAutoscalerImageSelectionFailsBeforeDrainOnUnknownImage(t *testing.T) {
	t.Parallel()

	servers := []*hcloud.Server{
		{Name: "a-old", PrivateNet: []hcloud.ServerPrivateNet{{IP: net.ParseIP("10.0.0.2")}}},
		{Name: "b-unknown", PrivateNet: []hcloud.ServerPrivateNet{{IP: net.ParseIP("10.0.0.3")}}},
	}
	selected, err := talosprovisioner.SelectAutoscalerImageServersForTest(
		t.Context(), servers, func(_ context.Context, ip string) (bool, error) {
			if ip == "10.0.0.3" {
				return false, io.ErrUnexpectedEOF
			}

			return false, nil
		},
	)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	assert.Empty(t, selected, "an incomplete image census must not authorize any drain")
}
