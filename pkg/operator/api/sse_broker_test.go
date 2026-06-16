package api_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// countingListService records how many times List is called so a test can prove the shared broker
// runs ONE discovery loop regardless of the number of open SSE connections.
type countingListService struct {
	stubClusterService

	calls atomic.Int64
}

func (c *countingListService) List(_ context.Context) (*v1alpha1.ClusterList, error) {
	c.calls.Add(1)

	cluster := v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: defaultNS},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{Distribution: v1alpha1.DistributionVCluster},
		},
	}

	return &v1alpha1.ClusterList{Items: []v1alpha1.Cluster{cluster}}, nil
}

// openEventStream opens one SSE connection, reads until it sees the initial snapshot, and returns a
// cancel func that closes it. The connection keeps streaming until cancel is called.
func openEventStream(t *testing.T, baseURL string) context.CancelFunc {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/events", nil)
	require.NoError(t, err)

	//nolint:bodyclose // closed in the reader goroutine below
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)

	ready := make(chan struct{})

	var once sync.Once

	go func() {
		defer func() { _ = response.Body.Close() }()

		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "event: clusters") {
				once.Do(func() { close(ready) })
			}
		}
	}()

	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("never received the initial clusters snapshot")
	}

	return cancel
}

// TestSSEBrokerSharesSingleDiscoveryLoop opens two concurrent SSE connections and asserts the backend
// runs a single shared List loop: over a window of N ticks the call count grows by roughly N, not 2N,
// proving connection count does not multiply provider discovery.
func TestSSEBrokerSharesSingleDiscoveryLoop(t *testing.T) {
	t.Parallel()

	const interval = 25 * time.Millisecond

	service := &countingListService{}
	server := &api.Server{Service: service, EventsInterval: interval}

	testServer := httptest.NewServer(server.Handler())
	t.Cleanup(testServer.Close)

	cancelA := openEventStream(t, testServer.URL)
	defer cancelA()

	cancelB := openEventStream(t, testServer.URL)
	defer cancelB()

	// Let several broker ticks elapse with both connections open.
	const ticks = 6

	baseline := service.calls.Load()

	time.Sleep(ticks * interval)

	delta := service.calls.Load() - baseline

	// One shared loop ticks ~`ticks` times; two independent loops would tick ~2*ticks. Assert the
	// growth stays well under the two-loop count (generous bounds absorb scheduling jitter).
	assert.Less(t, delta, int64(2*ticks),
		"shared broker must not multiply List calls by the number of connections")
	assert.Positive(
		t,
		delta,
		"the shared discovery loop must keep ticking while connections are open",
	)
}

// TestSSEBrokerIdlesWithZeroSubscribers asserts the broker stops its discovery loop once the last
// connection disconnects: subscriber count returns to zero and List stops being called.
func TestSSEBrokerIdlesWithZeroSubscribers(t *testing.T) {
	t.Parallel()

	const interval = 25 * time.Millisecond

	service := &countingListService{}
	server := &api.Server{Service: service, EventsInterval: interval}

	testServer := httptest.NewServer(server.Handler())
	t.Cleanup(testServer.Close)

	cancel := openEventStream(t, testServer.URL)

	require.Eventually(t, func() bool {
		return server.BrokerSubscriberCount() == 1
	}, 3*time.Second, 10*time.Millisecond, "the open connection must register as a subscriber")

	cancel()

	// After the connection closes, the broker drops to zero subscribers and stops its loop.
	require.Eventually(t, func() bool {
		return server.BrokerSubscriberCount() == 0
	}, 3*time.Second, 10*time.Millisecond, "broker must idle once the last subscriber leaves")

	// With the loop stopped, List must stop being called: the count is stable across a window.
	settled := service.calls.Load()

	time.Sleep(4 * interval)
	assert.Equal(t, settled, service.calls.Load(),
		"the idle broker must not keep polling after the last subscriber disconnects")
}
