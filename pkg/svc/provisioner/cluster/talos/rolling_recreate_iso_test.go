package talosprovisioner_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRollingReplaceSingleNode_UnavailableISOPreservesNode(t *testing.T) {
	t.Parallel()

	for _, role := range []string{talosprovisioner.RoleControlPlane, talosprovisioner.RoleWorker} {
		for _, status := range []int{http.StatusNotFound, http.StatusForbidden} {
			t.Run(fmt.Sprintf("%s/%d", role, status), func(t *testing.T) {
				t.Parallel()
				assertUnavailableISOPreservesNode(t, role, status)
			})
		}
	}
}

func assertUnavailableISOPreservesNode(t *testing.T, role string, status int) {
	t.Helper()

	transport := &membershipCloudTransport{address: "192.0.2.1", isoStatus: status}
	membership := &membershipClient{leaveErr: errMembershipUnavailable}
	provisioner := newClientErrProvisioner(t).
		WithTalosConfigsForTest(loadConfigs(t)).
		WithTalosOptions(v1alpha1.OptionsTalos{ISO: v1alpha1.DefaultTalosISO}).
		WithEtcdClientFactoryForTest(func(
			context.Context, string,
		) (talosprovisioner.EtcdMembershipClientForTest, error) {
			return membership, nil
		})
	server := &hcloud.Server{ID: 1, Name: "scale-cluster-" + role + "-1"}
	server.PublicNet.IPv4.IP = net.ParseIP(transport.address)
	clientset := fake.NewClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: server.Name, UID: "original-node"},
		Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{
			{Type: corev1.NodeInternalIP, Address: transport.address},
		}},
	})

	err := provisioner.RollingReplaceSingleNodeForTest(
		t.Context(), clientset, newMembershipCloud(transport), "scale-cluster", role, server,
	)

	require.ErrorContains(t, err, "ISO")
	assert.Empty(t, clientset.Actions(), "unavailable ISO must prevent cordon or drain")
	assert.Empty(t, membership.calls, "unavailable ISO must prevent membership mutation")
	assert.Empty(t, transport.writes, "unavailable ISO must prevent server deletion")
}
