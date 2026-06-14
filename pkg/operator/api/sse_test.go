package api_test

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// errListService is a ClusterService whose List always fails, to exercise the stream's error event.
type errListService struct {
	stubClusterService
}

var errListFailed = errors.New("discovery failed")

func (errListService) List(_ context.Context) (*v1alpha1.ClusterList, error) {
	return nil, errListFailed
}

// streamUntil opens the SSE stream and reads lines until predicate matches one (returning all lines
// read, matched=true) or the 3s deadline elapses (matched=false). The context bounds the read: when
// it fires, the in-flight Read unblocks and the scan stops. Request, read, and close all happen here
// so the response body is closed in the same scope it is opened.
func streamUntil(t *testing.T, baseURL string, predicate func(string) bool) ([]string, bool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/events", nil)
	require.NoError(t, err)

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	defer func() { _ = response.Body.Close() }()

	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "text/event-stream", response.Header.Get("Content-Type"))

	scanner := bufio.NewScanner(response.Body)

	var lines []string

	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)

		if predicate(line) {
			return lines, true
		}
	}

	return lines, false
}

func TestEventsStreamsInitialClusterSnapshot(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: defaultNS},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{Distribution: v1alpha1.DistributionVCluster},
		},
	}
	server := &api.Server{Service: stubClusterService{clusters: []v1alpha1.Cluster{cluster}}}

	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	lines, matched := streamUntil(t, testServer.URL, func(line string) bool {
		return strings.HasPrefix(line, "data:") && strings.Contains(line, `"name":"c1"`)
	})

	require.True(
		t,
		matched,
		"expected an initial clusters snapshot, got:\n%s",
		strings.Join(lines, "\n"),
	)
	assert.Contains(t, lines, "event: clusters")
}

func TestEventsEmitsHeartbeatWhenUnchanged(t *testing.T) {
	t.Parallel()

	// A stable list means every tick after the initial snapshot is a heartbeat comment. A short
	// interval makes the first heartbeat arrive well within the read deadline.
	server := &api.Server{
		Service:        stubClusterService{clusters: []v1alpha1.Cluster{}},
		EventsInterval: 20 * time.Millisecond,
	}

	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	lines, matched := streamUntil(t, testServer.URL, func(line string) bool {
		return strings.HasPrefix(line, ": heartbeat")
	})

	require.True(t, matched, "expected a heartbeat comment, got:\n%s", strings.Join(lines, "\n"))
}

func TestEventsEmitsErrorEventOnListFailure(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: errListService{}}

	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	lines, matched := streamUntil(t, testServer.URL, func(line string) bool {
		return strings.HasPrefix(line, "data:") && strings.Contains(line, "discovery failed")
	})

	require.True(t, matched, "expected a stream-error event, got:\n%s", strings.Join(lines, "\n"))
	// Named "stream-error", not "error": EventSource reserves "error" for connection-level failures,
	// so a server frame named "error" cannot be consumed cleanly by the client.
	assert.Contains(t, lines, "event: stream-error")
}

func TestEventsClosesStreamWhenSessionExpires(t *testing.T) {
	t.Parallel()

	const secret = "0123456789abcdef0123456789abcdef"

	server := api.NewAuthTestServer(newClient(t), []byte(secret))
	server.EventsInterval = 20 * time.Millisecond

	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	// A session valid at connect but expiring shortly after. Expiry is unix-seconds, so the margin
	// only needs to clear the current second; the stream closes within one tick of crossing it.
	auth := api.NewConfigAuthenticator(api.OIDCConfig{SessionSecret: []byte(secret)})
	cookie := auth.SignedCookie(
		api.SessionCookieName,
		api.MustMarshal(api.SessionClaims{
			Subject: "u1",
			Expiry:  time.Now().Add(1500 * time.Millisecond).Unix(),
		}),
		"/",
		int(time.Hour.Seconds()),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		testServer.URL+"/api/v1/events",
		nil,
	)
	require.NoError(t, err)
	request.AddCookie(cookie)

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	defer func() { _ = response.Body.Close() }()

	require.Equal(t, http.StatusOK, response.StatusCode) // authenticated at connect

	// The server stops streaming once the session expires; the client read then returns EOF well
	// before the 6s deadline. Reaching EOF proves the stream ended on expiry, not on the deadline.
	scanner := bufio.NewScanner(response.Body)
	sawSnapshot := false

	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "event: clusters") {
			sawSnapshot = true
		}
	}

	assert.True(t, sawSnapshot, "should stream the initial snapshot before the session expires")
	assert.NoError(t, ctx.Err(), "stream must close on session expiry, not on the test deadline")
}
