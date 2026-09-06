package talosprovisioner_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

var errMembershipUnavailable = errors.New("membership RPC unavailable")

type membershipClient struct {
	forfeitErr error
	leaveErr   error
	calls      []string
}

func (client *membershipClient) EtcdForfeitLeadership(
	context.Context, *machineapi.EtcdForfeitLeadershipRequest, ...grpc.CallOption,
) (*machineapi.EtcdForfeitLeadershipResponse, error) {
	client.calls = append(client.calls, "forfeit")

	return nil, client.forfeitErr
}

func (client *membershipClient) EtcdLeaveCluster(
	context.Context, *machineapi.EtcdLeaveClusterRequest, ...grpc.CallOption,
) error {
	client.calls = append(client.calls, "leave")

	return client.leaveErr
}

func (client *membershipClient) Close() error {
	client.calls = append(client.calls, "close")

	return nil
}

//nolint:funlen // Keep each table-driven transition and its safety assertions together.
func TestEtcdMembershipFailureStopsControlPlaneSequence(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		forfeitErr error
		leaveErr   error
	}{
		{name: "membership removal failure", leaveErr: errMembershipUnavailable},
		{name: "successful removal"},
		{name: "leadership transfer failure with successful leave", forfeitErr: errMembershipUnavailable},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := &membershipClient{
				forfeitErr: testCase.forfeitErr,
				leaveErr:   testCase.leaveErr,
			}
			mockDocker := docker.NewMockAPIClient(t)
			mockDocker.On("ContainerList", mock.Anything, mock.Anything).
				Return([]container.Summary{
					membershipContainer("1", "192.0.2.1"),
					membershipContainer("2", "192.0.2.2"),
				}, nil).Once()
			mockDocker.On("ContainerStop", mock.Anything, mock.Anything, mock.Anything).
				Return(nil).Maybe()
			mockDocker.On("ContainerRemove", mock.Anything, mock.Anything, mock.Anything).
				Return(nil).Maybe()

			var targets []string

			provisioner := newClientErrProvisioner(t).WithDockerClient(mockDocker).
				WithEtcdClientFactoryForTest(func(
					_ context.Context, target string,
				) (talosprovisioner.EtcdMembershipClientForTest, error) {
					targets = append(targets, target)

					return client, nil
				})
			result := clusterupdate.NewEmptyUpdateResult()
			err := provisioner.RemoveDockerNodesForTest(
				t.Context(),
				"scale-cluster",
				talosprovisioner.RoleControlPlane,
				2,
				result,
			)

			if testCase.leaveErr != nil {
				require.ErrorIs(t, err, testCase.leaveErr)
				assert.Equal(t, []string{"192.0.2.2"}, targets, "must not mutate the next member")
				assert.Equal(
					t,
					[]string{"forfeit", "leave", "close"},
					client.calls,
					"membership mutations must not be retried",
				)
				assert.Empty(t, result.AppliedChanges)
				assert.Len(t, result.FailedChanges, 1)
				mockDocker.AssertNumberOfCalls(t, "ContainerStop", 0)
				mockDocker.AssertNumberOfCalls(t, "ContainerRemove", 0)
			} else {
				require.NoError(t, err)
				assert.Equal(t, []string{"192.0.2.2", "192.0.2.1"}, targets)
				assert.Len(t, result.AppliedChanges, 2)
				mockDocker.AssertNumberOfCalls(t, "ContainerRemove", 2)
			}
		})
	}
}

func membershipContainer(index, address string) container.Summary {
	return container.Summary{
		ID: "cp-" + index, Names: []string{"/scale-cluster-control-plane-" + index},
		NetworkSettings: &container.NetworkSettingsSummary{
			Networks: map[string]*network.EndpointSettings{
				"scale-cluster": {IPAddress: address},
			},
		},
	}
}

// membershipCloudTransport serves provider reads entirely in memory and records
// every attempted write, so a forbidden server deletion is directly observable.
type membershipCloudTransport struct {
	address string
	writes  []string
}

func (transport *membershipCloudTransport) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	statusCode := http.StatusOK

	body := fmt.Sprintf(
		`{"servers":[{
			"id":1,"name":"scale-cluster-control-plane-1","status":"running",
			"labels":{"ksail.owned":"true","ksail.cluster.name":"scale-cluster","ksail.node.type":"controlplane"},
			"public_net":{"ipv4":{"ip":%q}}
		}]}`,
		transport.address,
	)
	if request.Method != http.MethodGet {
		transport.writes = append(transport.writes, request.Method+" "+request.URL.Path)
		statusCode = http.StatusForbidden
		body = `{"error":{"code":"forbidden","message":"unexpected infrastructure mutation"}}`
	}

	return &http.Response{
		StatusCode: statusCode, Body: io.NopCloser(strings.NewReader(body)),
		Header: http.Header{"Content-Type": []string{"application/json"}}, Request: request,
	}, nil
}

func newMembershipCloud(transport *membershipCloudTransport) *hetzner.Provider {
	return hetzner.NewProvider(hcloud.NewClient(
		hcloud.WithToken("test-token"),
		hcloud.WithEndpoint("https://membership.invalid"),
		hcloud.WithHTTPClient(&http.Client{Transport: transport}),
	))
}
