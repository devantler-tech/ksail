package hetzner_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	deletedLoadBalancerID   = 7
	loadBalancerDeleteReply = `{"action":{"id":9,"command":"delete_load_balancer",` +
		`"status":"running","progress":0}}`
)

// newLoadBalancerDeleteServer returns an httptest server whose load balancer
// DELETE answers with deleteReply, and whose action lookup reports the action
// as running on the first poll and as successful afterwards. It counts both
// the delete requests and the action polls.
func newLoadBalancerDeleteServer(
	t *testing.T,
	deleteReply string,
	deleteCalls *int32,
	actionPolls *int32,
) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/load_balancers/7",
		func(responseWriter http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodDelete {
				http.Error(responseWriter, "unexpected method", http.StatusMethodNotAllowed)

				return
			}

			atomic.AddInt32(deleteCalls, 1)

			responseWriter.Header().Set("Content-Type", "application/json")
			_, _ = responseWriter.Write([]byte(deleteReply))
		})

	mux.HandleFunc("/actions", func(responseWriter http.ResponseWriter, _ *http.Request) {
		status := "success"
		if atomic.AddInt32(actionPolls, 1) == 1 {
			status = "running"
		}

		responseWriter.Header().Set("Content-Type", "application/json")
		_, _ = responseWriter.Write([]byte(
			`{"actions":[{"id":9,"command":"delete_load_balancer","status":"` + status + `"}]}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

// TestDeleteLoadBalancer_WaitsForTheDeletionAction proves that deleting a load
// balancer only returns once the action the API answered with has finished:
// the deletion is asynchronous, and the network it is attached to is deleted
// right after.
func TestDeleteLoadBalancer_WaitsForTheDeletionAction(t *testing.T) {
	t.Parallel()

	var deleteCalls, actionPolls int32

	srv := newLoadBalancerDeleteServer(t, loadBalancerDeleteReply, &deleteCalls, &actionPolls)
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	err := prov.DeleteLoadBalancerWithRetryForTest(
		t.Context(),
		&hcloud.LoadBalancer{ID: deletedLoadBalancerID, Name: "test-cluster-lb"},
	)

	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&deleteCalls))
	assert.GreaterOrEqual(t, atomic.LoadInt32(&actionPolls), int32(2),
		"the deletion must be polled until the action leaves the running state")
}

// TestDeleteLoadBalancer_AcceptsAReplyWithoutAnAction proves that a deletion
// answered without an action still counts as done and polls nothing.
func TestDeleteLoadBalancer_AcceptsAReplyWithoutAnAction(t *testing.T) {
	t.Parallel()

	var deleteCalls, actionPolls int32

	srv := newLoadBalancerDeleteServer(t, `{}`, &deleteCalls, &actionPolls)
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	err := prov.DeleteLoadBalancerWithRetryForTest(
		t.Context(),
		&hcloud.LoadBalancer{ID: deletedLoadBalancerID, Name: "test-cluster-lb"},
	)

	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&deleteCalls))
	assert.Zero(t, atomic.LoadInt32(&actionPolls))
}
