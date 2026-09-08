package talosprovisioner_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRollingReplaceSingleNode_PreparationFailurePreservesNode(t *testing.T) {
	t.Parallel()

	for _, role := range []string{talosprovisioner.RoleControlPlane, talosprovisioner.RoleWorker} {
		for _, failure := range []string{
			"missing role config", "missing snapshot version", "missing boot source",
		} {
			t.Run(role+"/"+failure, func(t *testing.T) {
				t.Parallel()
				assertReplacementPreparationFailure(t, role, failure)
			})
		}
	}
}

func TestRollingReplaceSingleNode_UsesImagePreparedBeforeDeletion(t *testing.T) {
	t.Parallel()

	transport := &preparedReplacementTransport{}
	cloudClient := hcloud.NewClient(
		hcloud.WithToken("test-token"), hcloud.WithEndpoint("https://prepare.invalid"),
		hcloud.WithHTTPClient(&http.Client{Transport: transport}),
	)
	provisioner := newClientErrProvisioner(t).
		WithTalosConfigsForTest(loadConfigs(t)).
		WithTalosOptions(v1alpha1.OptionsTalos{SchematicID: "test-schematic", Version: "v1.13.3"}).
		WithHetznerOptions(v1alpha1.OptionsHetzner{Location: "fsn1", ControlPlaneServerType: "cx23"}).
		WithSnapshotManager(hetzner.NewSnapshotManager(cloudClient, io.Discard)).
		WithEtcdClientFactoryForTest(func(
			context.Context, string,
		) (talosprovisioner.EtcdMembershipClientForTest, error) {
			return &membershipClient{}, nil
		})
	server := &hcloud.Server{ID: 1, Name: "scale-cluster-control-plane-1"}
	server.PublicNet.IPv4.IP = net.ParseIP("192.0.2.1")

	err := provisioner.RollingReplaceSingleNodeForTest(
		t.Context(), fake.NewClientset(), hetzner.NewProvider(cloudClient),
		"scale-cluster", talosprovisioner.RoleControlPlane, server,
	)

	require.ErrorContains(t, err, "fixture stops after observing prepared image")
	assert.Equal(t, []string{
		"GET /images", "DELETE /servers/1", "GET /servers", "POST /servers",
	}, transport.events, "image resolution must happen once, before deletion")
	assert.Equal(t, int64(42), transport.createdImageID)
}

// preparedReplacementTransport permits only the offline provider sequence up
// to the replacement POST, where it rejects creation after recording the image.
type preparedReplacementTransport struct {
	events         []string
	createdImageID int64
}

func (transport *preparedReplacementTransport) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	event := request.Method + " " + request.URL.Path
	transport.events = append(transport.events, event)
	statusCode := http.StatusOK

	var response string

	switch event {
	case "GET /images":
		response = `{"images":[{"id":42,"status":"available","type":"snapshot"}]}`
	case "GET /servers":
		response = `{"servers":[]}`
	case "DELETE /servers/1":
		response = `{"action":{"id":1,"status":"success"}}`
	case "POST /servers":
		var payload struct {
			Image int64 `json:"image"`
		}

		err := json.NewDecoder(request.Body).Decode(&payload)
		if err != nil {
			return nil, fmt.Errorf("decode replacement request: %w", err)
		}

		transport.createdImageID = payload.Image
		statusCode = http.StatusForbidden
		response = `{"error":{"code":"forbidden","message":"fixture stops after observing prepared image"}}`
	default:
		statusCode = http.StatusForbidden
		response = `{"error":{"code":"forbidden","message":"unexpected preparation request"}}`
	}

	return &http.Response{
		StatusCode: statusCode, Body: io.NopCloser(strings.NewReader(response)),
		Header: http.Header{"Content-Type": []string{"application/json"}}, Request: request,
	}, nil
}

func assertReplacementPreparationFailure(t *testing.T, role, failure string) {
	t.Helper()

	transport := &membershipCloudTransport{address: "192.0.2.1"}
	membership := &membershipClient{leaveErr: errMembershipUnavailable}
	provisioner := newClientErrProvisioner(t).
		WithSnapshotManager(hetzner.NewSnapshotManager(nil, io.Discard)).
		WithTalosOptions(v1alpha1.OptionsTalos{SchematicID: "test-schematic"}).
		WithEtcdClientFactoryForTest(func(
			context.Context, string,
		) (talosprovisioner.EtcdMembershipClientForTest, error) {
			return membership, nil
		})
	expectedErr := talosprovisioner.ErrNoConfigForRole

	if failure != "missing role config" {
		provisioner.WithTalosConfigsForTest(loadConfigs(t))

		expectedErr = talosprovisioner.ErrSchematicRequiresVersion
	}

	if failure == "missing boot source" {
		provisioner.WithSnapshotManager(nil).WithTalosOptions(v1alpha1.OptionsTalos{})

		expectedErr = hetzner.ErrImageOrISORequired
	}

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

	require.ErrorIs(t, err, expectedErr)
	assert.Empty(t, clientset.Actions(), "preparation must finish before cordon or drain")
	assert.Empty(t, membership.calls, "preparation must finish before membership mutation")
	assert.Empty(t, transport.writes, "preparation failure must retain the server")
}
