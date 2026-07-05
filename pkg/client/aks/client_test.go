package aks_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	azfake "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7/fake"
	"github.com/devantler-tech/ksail/v7/pkg/client/aks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSubscription  = "00000000-0000-0000-0000-000000000000"
	testResourceGroup = "test-rg"
	testClusterName   = "test-cluster"
	testPoolName      = "default"
)

// newTestClient wires an aks.Client to the SDK's in-process fake servers, so
// every request — including long-running-operation polling — is served by the
// injected fakes without credentials or network access.
func newTestClient(t *testing.T, factory *fake.ServerFactory) *aks.Client {
	t.Helper()

	return newTestClientPolling(t, factory, time.Millisecond)
}

// newTestClientPolling is newTestClient with an explicit poll interval, for
// tests that need a wide inter-poll sleep window (e.g. cancellation).
func newTestClientPolling(
	t *testing.T, factory *fake.ServerFactory, pollInterval time.Duration,
) *aks.Client {
	t.Helper()

	client, err := aks.NewClient(
		testSubscription,
		aks.WithCredential(&azfake.TokenCredential{}),
		aks.WithClientOptions(&arm.ClientOptions{
			ClientOptions: azcore.ClientOptions{
				Transport: fake.NewServerFactoryTransport(factory),
			},
		}),
		aks.WithPollInterval(pollInterval),
	)
	require.NoError(t, err)

	return client
}

func namedCluster(name string) armcontainerservice.ManagedCluster {
	return armcontainerservice.ManagedCluster{Name: new(name)}
}

func TestNewClientRejectsEmptySubscriptionID(t *testing.T) {
	t.Parallel()

	_, err := aks.NewClient("")

	require.ErrorIs(t, err, aks.ErrMissingSubscriptionID)
}

func TestCreateClusterWaitsForOperation(t *testing.T) {
	t.Parallel()

	polled := false
	factory := &fake.ServerFactory{ManagedClustersServer: fake.ManagedClustersServer{
		BeginCreateOrUpdate: func(
			_ context.Context, resourceGroup, name string,
			_ armcontainerservice.ManagedCluster,
			_ *armcontainerservice.ManagedClustersClientBeginCreateOrUpdateOptions,
		) (
			azfake.PollerResponder[armcontainerservice.ManagedClustersClientCreateOrUpdateResponse],
			azfake.ErrorResponder,
		) {
			var (
				resp    azfake.PollerResponder[armcontainerservice.ManagedClustersClientCreateOrUpdateResponse]
				errResp azfake.ErrorResponder
			)

			assert.Equal(t, testResourceGroup, resourceGroup)

			// A non-terminal page first forces at least one poll round-trip
			// before the terminal response, pinning the wait-for-completion
			// contract rather than a fire-and-forget.
			polled = true

			resp.AddNonTerminalResponse(http.StatusOK, nil)
			resp.SetTerminalResponse(
				http.StatusOK,
				armcontainerservice.ManagedClustersClientCreateOrUpdateResponse{
					ManagedCluster: namedCluster(name),
				},
				nil,
			)

			return resp, errResp
		},
	}}

	created, err := newTestClient(t, factory).CreateCluster(
		t.Context(), testResourceGroup, testClusterName, armcontainerservice.ManagedCluster{},
	)

	require.NoError(t, err)
	require.NotNil(t, created.Name)
	assert.Equal(t, testClusterName, *created.Name)
	assert.True(t, polled)
}

func TestCreateClusterSurfacesOperationError(t *testing.T) {
	t.Parallel()

	factory := &fake.ServerFactory{ManagedClustersServer: fake.ManagedClustersServer{
		BeginCreateOrUpdate: func(
			_ context.Context, _, _ string,
			_ armcontainerservice.ManagedCluster,
			_ *armcontainerservice.ManagedClustersClientBeginCreateOrUpdateOptions,
		) (
			azfake.PollerResponder[armcontainerservice.ManagedClustersClientCreateOrUpdateResponse],
			azfake.ErrorResponder,
		) {
			var (
				resp    azfake.PollerResponder[armcontainerservice.ManagedClustersClientCreateOrUpdateResponse]
				errResp azfake.ErrorResponder
			)

			resp.AddNonTerminalResponse(http.StatusOK, nil)
			resp.SetTerminalError(http.StatusConflict, "QuotaExceeded")

			return resp, errResp
		},
	}}

	_, err := newTestClient(t, factory).CreateCluster(
		t.Context(), testResourceGroup, testClusterName, armcontainerservice.ManagedCluster{},
	)

	require.ErrorIs(t, err, aks.ErrOperationFailed)
	require.ErrorContains(t, err, "QuotaExceeded")
	require.ErrorContains(t, err, testClusterName)
}

func TestCreateClusterHonoursContextCancellation(t *testing.T) {
	t.Parallel()

	factory := &fake.ServerFactory{ManagedClustersServer: fake.ManagedClustersServer{
		BeginCreateOrUpdate: func(
			_ context.Context, _, _ string,
			_ armcontainerservice.ManagedCluster,
			_ *armcontainerservice.ManagedClustersClientBeginCreateOrUpdateOptions,
		) (
			azfake.PollerResponder[armcontainerservice.ManagedClustersClientCreateOrUpdateResponse],
			azfake.ErrorResponder,
		) {
			var (
				resp    azfake.PollerResponder[armcontainerservice.ManagedClustersClientCreateOrUpdateResponse]
				errResp azfake.ErrorResponder
			)

			// Enough non-terminal pages that the operation cannot finish
			// before the context deadline strikes mid-sleep between polls.
			for range 20 {
				resp.AddNonTerminalResponse(http.StatusOK, nil)
			}

			resp.SetTerminalResponse(
				http.StatusOK,
				armcontainerservice.ManagedClustersClientCreateOrUpdateResponse{},
				nil,
			)

			return resp, errResp
		},
	}}

	// Deadline (40ms) lands inside the second inter-poll sleep (25–50ms), so
	// the poll loop must notice the dead context and stop early.
	expiring, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()

	_, err := newTestClientPolling(t, factory, 25*time.Millisecond).CreateCluster(
		expiring, testResourceGroup, testClusterName, armcontainerservice.ManagedCluster{},
	)

	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestDeleteClusterWaitsForOperation(t *testing.T) {
	t.Parallel()

	factory := &fake.ServerFactory{ManagedClustersServer: fake.ManagedClustersServer{
		BeginDelete: func(
			_ context.Context, _, _ string,
			_ *armcontainerservice.ManagedClustersClientBeginDeleteOptions,
		) (
			azfake.PollerResponder[armcontainerservice.ManagedClustersClientDeleteResponse],
			azfake.ErrorResponder,
		) {
			var (
				resp    azfake.PollerResponder[armcontainerservice.ManagedClustersClientDeleteResponse]
				errResp azfake.ErrorResponder
			)

			resp.AddNonTerminalResponse(http.StatusAccepted, nil)
			resp.SetTerminalResponse(
				http.StatusOK, armcontainerservice.ManagedClustersClientDeleteResponse{}, nil,
			)

			return resp, errResp
		},
	}}

	err := newTestClient(t, factory).DeleteCluster(
		t.Context(), testResourceGroup, testClusterName,
	)

	require.NoError(t, err)
}

func TestDeleteClusterWrapsRequestError(t *testing.T) {
	t.Parallel()

	factory := &fake.ServerFactory{ManagedClustersServer: fake.ManagedClustersServer{
		BeginDelete: func(
			_ context.Context, _, _ string,
			_ *armcontainerservice.ManagedClustersClientBeginDeleteOptions,
		) (
			azfake.PollerResponder[armcontainerservice.ManagedClustersClientDeleteResponse],
			azfake.ErrorResponder,
		) {
			var (
				resp    azfake.PollerResponder[armcontainerservice.ManagedClustersClientDeleteResponse]
				errResp azfake.ErrorResponder
			)

			errResp.SetResponseError(http.StatusNotFound, "ResourceNotFound")

			return resp, errResp
		},
	}}

	err := newTestClient(t, factory).DeleteCluster(
		t.Context(), testResourceGroup, testClusterName,
	)

	require.ErrorContains(t, err, "ResourceNotFound")
	require.ErrorContains(t, err, "delete cluster")
}

func TestGetClusterReturnsCluster(t *testing.T) {
	t.Parallel()

	factory := &fake.ServerFactory{ManagedClustersServer: fake.ManagedClustersServer{
		Get: func(
			_ context.Context, _, name string,
			_ *armcontainerservice.ManagedClustersClientGetOptions,
		) (
			azfake.Responder[armcontainerservice.ManagedClustersClientGetResponse],
			azfake.ErrorResponder,
		) {
			var (
				resp    azfake.Responder[armcontainerservice.ManagedClustersClientGetResponse]
				errResp azfake.ErrorResponder
			)

			resp.SetResponse(
				http.StatusOK,
				armcontainerservice.ManagedClustersClientGetResponse{
					ManagedCluster: namedCluster(name),
				},
				nil,
			)

			return resp, errResp
		},
	}}

	cluster, err := newTestClient(t, factory).GetCluster(
		t.Context(), testResourceGroup, testClusterName,
	)

	require.NoError(t, err)
	require.NotNil(t, cluster.Name)
	assert.Equal(t, testClusterName, *cluster.Name)
}

func TestGetClusterWrapsError(t *testing.T) {
	t.Parallel()

	factory := &fake.ServerFactory{ManagedClustersServer: fake.ManagedClustersServer{
		Get: func(
			_ context.Context, _, _ string,
			_ *armcontainerservice.ManagedClustersClientGetOptions,
		) (
			azfake.Responder[armcontainerservice.ManagedClustersClientGetResponse],
			azfake.ErrorResponder,
		) {
			var (
				resp    azfake.Responder[armcontainerservice.ManagedClustersClientGetResponse]
				errResp azfake.ErrorResponder
			)

			errResp.SetResponseError(http.StatusNotFound, "ResourceNotFound")

			return resp, errResp
		},
	}}

	_, err := newTestClient(t, factory).GetCluster(
		t.Context(), testResourceGroup, testClusterName,
	)

	require.ErrorContains(t, err, "get cluster")
	require.ErrorContains(t, err, "ResourceNotFound")
}

func TestListClustersAcrossSubscriptionDrainsAllPages(t *testing.T) {
	t.Parallel()

	factory := &fake.ServerFactory{ManagedClustersServer: fake.ManagedClustersServer{
		NewListPager: func(
			_ *armcontainerservice.ManagedClustersClientListOptions,
		) azfake.PagerResponder[armcontainerservice.ManagedClustersClientListResponse] {
			var resp azfake.PagerResponder[armcontainerservice.ManagedClustersClientListResponse]

			first := namedCluster("cluster-a")
			second := namedCluster("cluster-b")

			resp.AddPage(http.StatusOK, armcontainerservice.ManagedClustersClientListResponse{
				ManagedClusterListResult: armcontainerservice.ManagedClusterListResult{
					Value: []*armcontainerservice.ManagedCluster{&first},
				},
			}, nil)
			resp.AddPage(http.StatusOK, armcontainerservice.ManagedClustersClientListResponse{
				ManagedClusterListResult: armcontainerservice.ManagedClusterListResult{
					Value: []*armcontainerservice.ManagedCluster{&second},
				},
			}, nil)

			return resp
		},
	}}

	clusters, err := newTestClient(t, factory).ListClusters(t.Context(), "")

	require.NoError(t, err)
	require.Len(t, clusters, 2)
	assert.Equal(t, "cluster-a", *clusters[0].Name)
	assert.Equal(t, "cluster-b", *clusters[1].Name)
}

func TestListClustersInResourceGroupScopesTheRequest(t *testing.T) {
	t.Parallel()

	factory := &fake.ServerFactory{ManagedClustersServer: fake.ManagedClustersServer{
		NewListByResourceGroupPager: func(
			resourceGroup string,
			_ *armcontainerservice.ManagedClustersClientListByResourceGroupOptions,
		) azfake.PagerResponder[armcontainerservice.ManagedClustersClientListByResourceGroupResponse] {
			var resp azfake.PagerResponder[armcontainerservice.ManagedClustersClientListByResourceGroupResponse]

			assert.Equal(t, testResourceGroup, resourceGroup)

			scoped := namedCluster("scoped-cluster")

			resp.AddPage(
				http.StatusOK,
				armcontainerservice.ManagedClustersClientListByResourceGroupResponse{
					ManagedClusterListResult: armcontainerservice.ManagedClusterListResult{
						Value: []*armcontainerservice.ManagedCluster{&scoped},
					},
				},
				nil,
			)

			return resp
		},
	}}

	clusters, err := newTestClient(t, factory).ListClusters(t.Context(), testResourceGroup)

	require.NoError(t, err)
	require.Len(t, clusters, 1)
	assert.Equal(t, "scoped-cluster", *clusters[0].Name)
}

func TestListClustersWrapsPageError(t *testing.T) {
	t.Parallel()

	factory := &fake.ServerFactory{ManagedClustersServer: fake.ManagedClustersServer{
		NewListPager: func(
			_ *armcontainerservice.ManagedClustersClientListOptions,
		) azfake.PagerResponder[armcontainerservice.ManagedClustersClientListResponse] {
			var resp azfake.PagerResponder[armcontainerservice.ManagedClustersClientListResponse]

			resp.AddResponseError(http.StatusInternalServerError, "InternalError")

			return resp
		},
	}}

	_, err := newTestClient(t, factory).ListClusters(t.Context(), "")

	require.ErrorContains(t, err, "list clusters")
	require.ErrorContains(t, err, "InternalError")
}

// threeNodePoolGet fakes fetching an existing three-node agent pool.
func threeNodePoolGet(
	_ context.Context, _, _, poolName string,
	_ *armcontainerservice.AgentPoolsClientGetOptions,
) (
	azfake.Responder[armcontainerservice.AgentPoolsClientGetResponse],
	azfake.ErrorResponder,
) {
	var (
		resp    azfake.Responder[armcontainerservice.AgentPoolsClientGetResponse]
		errResp azfake.ErrorResponder
	)

	resp.SetResponse(http.StatusOK, armcontainerservice.AgentPoolsClientGetResponse{
		AgentPool: armcontainerservice.AgentPool{
			Name: new(poolName),
			Properties: &armcontainerservice.ManagedClusterAgentPoolProfileProperties{
				Count: new(int32(3)),
			},
		},
	}, nil)

	return resp, errResp
}

// capturingPoolUpdate fakes the resize submission, recording the submitted
// count into captured.
func capturingPoolUpdate(t *testing.T, captured **int32) func(
	context.Context, string, string, string,
	armcontainerservice.AgentPool,
	*armcontainerservice.AgentPoolsClientBeginCreateOrUpdateOptions,
) (
	azfake.PollerResponder[armcontainerservice.AgentPoolsClientCreateOrUpdateResponse],
	azfake.ErrorResponder,
) {
	t.Helper()

	return func(
		_ context.Context, _, _, _ string,
		parameters armcontainerservice.AgentPool,
		_ *armcontainerservice.AgentPoolsClientBeginCreateOrUpdateOptions,
	) (
		azfake.PollerResponder[armcontainerservice.AgentPoolsClientCreateOrUpdateResponse],
		azfake.ErrorResponder,
	) {
		var (
			resp    azfake.PollerResponder[armcontainerservice.AgentPoolsClientCreateOrUpdateResponse]
			errResp azfake.ErrorResponder
		)

		require.NotNil(t, parameters.Properties)
		*captured = parameters.Properties.Count

		resp.SetTerminalResponse(
			http.StatusOK,
			armcontainerservice.AgentPoolsClientCreateOrUpdateResponse{AgentPool: parameters},
			nil,
		)

		return resp, errResp
	}
}

func TestSetAgentPoolCountResizesPool(t *testing.T) {
	t.Parallel()

	var submittedCount *int32

	factory := &fake.ServerFactory{AgentPoolsServer: fake.AgentPoolsServer{
		Get:                 threeNodePoolGet,
		BeginCreateOrUpdate: capturingPoolUpdate(t, &submittedCount),
	}}

	err := newTestClient(t, factory).SetAgentPoolCount(
		t.Context(), testResourceGroup, testClusterName, testPoolName, 5,
	)

	require.NoError(t, err)
	require.NotNil(t, submittedCount)
	assert.Equal(t, int32(5), *submittedCount)
}

func TestSetAgentPoolCountRejectsPoolWithoutProperties(t *testing.T) {
	t.Parallel()

	factory := &fake.ServerFactory{AgentPoolsServer: fake.AgentPoolsServer{
		Get: func(
			_ context.Context, _, _, poolName string,
			_ *armcontainerservice.AgentPoolsClientGetOptions,
		) (
			azfake.Responder[armcontainerservice.AgentPoolsClientGetResponse],
			azfake.ErrorResponder,
		) {
			var (
				resp    azfake.Responder[armcontainerservice.AgentPoolsClientGetResponse]
				errResp azfake.ErrorResponder
			)

			resp.SetResponse(http.StatusOK, armcontainerservice.AgentPoolsClientGetResponse{
				AgentPool: armcontainerservice.AgentPool{Name: new(poolName)},
			}, nil)

			return resp, errResp
		},
	}}

	err := newTestClient(t, factory).SetAgentPoolCount(
		t.Context(), testResourceGroup, testClusterName, testPoolName, 5,
	)

	require.ErrorIs(t, err, aks.ErrAgentPoolPropertiesMissing)
}

func TestSetAgentPoolCountSurfacesOperationError(t *testing.T) {
	t.Parallel()

	factory := &fake.ServerFactory{AgentPoolsServer: fake.AgentPoolsServer{
		Get: threeNodePoolGet,
		BeginCreateOrUpdate: func(
			_ context.Context, _, _, _ string,
			_ armcontainerservice.AgentPool,
			_ *armcontainerservice.AgentPoolsClientBeginCreateOrUpdateOptions,
		) (
			azfake.PollerResponder[armcontainerservice.AgentPoolsClientCreateOrUpdateResponse],
			azfake.ErrorResponder,
		) {
			var (
				resp    azfake.PollerResponder[armcontainerservice.AgentPoolsClientCreateOrUpdateResponse]
				errResp azfake.ErrorResponder
			)

			resp.AddNonTerminalResponse(http.StatusOK, nil)
			resp.SetTerminalError(http.StatusConflict, "OperationNotAllowed")

			return resp, errResp
		},
	}}

	err := newTestClient(t, factory).SetAgentPoolCount(
		t.Context(), testResourceGroup, testClusterName, testPoolName, 5,
	)

	require.ErrorIs(t, err, aks.ErrOperationFailed)
	require.ErrorContains(t, err, testPoolName)
}

func TestSetAgentPoolCountWrapsGetError(t *testing.T) {
	t.Parallel()

	factory := &fake.ServerFactory{AgentPoolsServer: fake.AgentPoolsServer{
		Get: func(
			_ context.Context, _, _, _ string,
			_ *armcontainerservice.AgentPoolsClientGetOptions,
		) (
			azfake.Responder[armcontainerservice.AgentPoolsClientGetResponse],
			azfake.ErrorResponder,
		) {
			var (
				resp    azfake.Responder[armcontainerservice.AgentPoolsClientGetResponse]
				errResp azfake.ErrorResponder
			)

			errResp.SetResponseError(http.StatusNotFound, "AgentPoolNotFound")

			return resp, errResp
		},
	}}

	err := newTestClient(t, factory).SetAgentPoolCount(
		t.Context(), testResourceGroup, testClusterName, testPoolName, 5,
	)

	require.ErrorContains(t, err, "get agent pool")
	require.ErrorContains(t, err, "AgentPoolNotFound")
}

// TestStartClusterWaitsForOperation pins the start wrapper's LRO handling:
// BeginStart is polled to completion before the call returns.
func TestStartClusterWaitsForOperation(t *testing.T) {
	t.Parallel()

	factory := &fake.ServerFactory{ManagedClustersServer: fake.ManagedClustersServer{
		BeginStart: func(
			_ context.Context, _, _ string,
			_ *armcontainerservice.ManagedClustersClientBeginStartOptions,
		) (
			azfake.PollerResponder[armcontainerservice.ManagedClustersClientStartResponse],
			azfake.ErrorResponder,
		) {
			var (
				resp    azfake.PollerResponder[armcontainerservice.ManagedClustersClientStartResponse]
				errResp azfake.ErrorResponder
			)

			resp.AddNonTerminalResponse(http.StatusAccepted, nil)
			resp.SetTerminalResponse(
				http.StatusOK, armcontainerservice.ManagedClustersClientStartResponse{}, nil,
			)

			return resp, errResp
		},
	}}

	err := newTestClient(t, factory).StartCluster(
		t.Context(), testResourceGroup, testClusterName,
	)

	require.NoError(t, err)
}

// TestStopClusterWaitsForOperation pins the stop wrapper's LRO handling.
func TestStopClusterWaitsForOperation(t *testing.T) {
	t.Parallel()

	factory := &fake.ServerFactory{ManagedClustersServer: fake.ManagedClustersServer{
		BeginStop: func(
			_ context.Context, _, _ string,
			_ *armcontainerservice.ManagedClustersClientBeginStopOptions,
		) (
			azfake.PollerResponder[armcontainerservice.ManagedClustersClientStopResponse],
			azfake.ErrorResponder,
		) {
			var (
				resp    azfake.PollerResponder[armcontainerservice.ManagedClustersClientStopResponse]
				errResp azfake.ErrorResponder
			)

			resp.AddNonTerminalResponse(http.StatusAccepted, nil)
			resp.SetTerminalResponse(
				http.StatusOK, armcontainerservice.ManagedClustersClientStopResponse{}, nil,
			)

			return resp, errResp
		},
	}}

	err := newTestClient(t, factory).StopCluster(
		t.Context(), testResourceGroup, testClusterName,
	)

	require.NoError(t, err)
}

// TestStopClusterSurfacesOperationError pins the ErrOperationFailed wrap on
// the stop wrapper's PollUntilDone path (a non-terminal page first, so the
// failure surfaces during the wait rather than at BeginStop).
func TestStopClusterSurfacesOperationError(t *testing.T) {
	t.Parallel()

	factory := &fake.ServerFactory{ManagedClustersServer: fake.ManagedClustersServer{
		BeginStop: func(
			_ context.Context, _, _ string,
			_ *armcontainerservice.ManagedClustersClientBeginStopOptions,
		) (
			azfake.PollerResponder[armcontainerservice.ManagedClustersClientStopResponse],
			azfake.ErrorResponder,
		) {
			var (
				resp    azfake.PollerResponder[armcontainerservice.ManagedClustersClientStopResponse]
				errResp azfake.ErrorResponder
			)

			resp.AddNonTerminalResponse(http.StatusAccepted, nil)
			resp.SetTerminalError(http.StatusConflict, "StopInProgress")

			return resp, errResp
		},
	}}

	err := newTestClient(t, factory).StopCluster(
		t.Context(), testResourceGroup, testClusterName,
	)

	require.ErrorIs(t, err, aks.ErrOperationFailed)
	require.ErrorContains(t, err, "StopInProgress")
	require.ErrorContains(t, err, testClusterName)
}

// TestStartClusterSurfacesOperationError pins the ErrOperationFailed wrap on
// the start wrapper's PollUntilDone path, mirroring the stop-side test.
func TestStartClusterSurfacesOperationError(t *testing.T) {
	t.Parallel()

	factory := &fake.ServerFactory{ManagedClustersServer: fake.ManagedClustersServer{
		BeginStart: func(
			_ context.Context, _, _ string,
			_ *armcontainerservice.ManagedClustersClientBeginStartOptions,
		) (
			azfake.PollerResponder[armcontainerservice.ManagedClustersClientStartResponse],
			azfake.ErrorResponder,
		) {
			var (
				resp    azfake.PollerResponder[armcontainerservice.ManagedClustersClientStartResponse]
				errResp azfake.ErrorResponder
			)

			resp.AddNonTerminalResponse(http.StatusAccepted, nil)
			resp.SetTerminalError(http.StatusConflict, "StartInProgress")

			return resp, errResp
		},
	}}

	err := newTestClient(t, factory).StartCluster(
		t.Context(), testResourceGroup, testClusterName,
	)

	require.ErrorIs(t, err, aks.ErrOperationFailed)
	require.ErrorContains(t, err, "StartInProgress")
	require.ErrorContains(t, err, testClusterName)
}
