package k8s_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

// errAPIServerUnavailable simulates an API server connection error.
var errAPIServerUnavailable = errors.New("connection refused")

// controllableDiscoveryClient allows tests to control when API calls succeed or fail.
type controllableDiscoveryClient struct {
	*fakediscovery.FakeDiscovery

	shouldSucceed atomic.Bool
	callCount     atomic.Int32
}

func newControllableClient() (*fake.Clientset, *controllableDiscoveryClient) {
	clientset := fake.NewClientset()

	fakeDiscovery, ok := clientset.Discovery().(*fakediscovery.FakeDiscovery)
	if !ok {
		panic("expected Discovery() to return *fakediscovery.FakeDiscovery")
	}

	controllable := &controllableDiscoveryClient{
		FakeDiscovery: fakeDiscovery,
	}

	return clientset, controllable
}

func (c *controllableDiscoveryClient) ServerVersion() (*version.Info, error) {
	c.callCount.Add(1)

	if c.shouldSucceed.Load() {
		return &version.Info{Major: "1", Minor: "28"}, nil
	}

	return nil, errAPIServerUnavailable
}

// stubClientset wraps a fake clientset but returns our controllable discovery client.
type stubClientset struct {
	kubernetes.Interface

	discovery *controllableDiscoveryClient
}

func (s *stubClientset) Discovery() discovery.DiscoveryInterface {
	return s.discovery
}

func TestWaitForAPIServerReady(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name        string
		setupClient func() kubernetes.Interface
		timeout     time.Duration
		wantErr     bool
		errContains string
	}

	tests := []testCase{
		{
			name: "returns nil when API server responds immediately",
			setupClient: func() kubernetes.Interface {
				clientset, controllable := newControllableClient()
				controllable.shouldSucceed.Store(true)

				return &stubClientset{Interface: clientset, discovery: controllable}
			},
			timeout: 200 * time.Millisecond,
			wantErr: false,
		},
		{
			name: "returns error when timeout exceeded",
			setupClient: func() kubernetes.Interface {
				clientset, controllable := newControllableClient()
				controllable.shouldSucceed.Store(false) // never succeeds

				return &stubClientset{Interface: clientset, discovery: controllable}
			},
			timeout:     100 * time.Millisecond,
			wantErr:     true,
			errContains: "failed to poll for readiness",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := testCase.setupClient()

			ctx, cancel := context.WithTimeout(
				context.Background(),
				testCase.timeout+100*time.Millisecond,
			)
			defer cancel()

			err := k8s.WaitForAPIServerReady(ctx, client, testCase.timeout)

			if testCase.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), testCase.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

//nolint:funlen // Table-driven test with multiple cases naturally exceeds limit
func TestWaitForAPIServerStable(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name              string
		requiredSuccesses int
		setupClient       func() kubernetes.Interface
		timeout           time.Duration
		wantErr           bool
		errContains       string
	}

	tests := []testCase{
		{
			name:              "returns nil after required consecutive successes",
			requiredSuccesses: 2,
			setupClient: func() kubernetes.Interface {
				clientset, controllable := newControllableClient()
				controllable.shouldSucceed.Store(true) // always succeeds

				return &stubClientset{Interface: clientset, discovery: controllable}
			},
			timeout: 10 * time.Second, // needs to be long enough for 2 poll cycles at 2s intervals
			wantErr: false,
		},
		{
			name:              "defaults to 1 when requiredSuccesses is less than 1",
			requiredSuccesses: 0,
			setupClient: func() kubernetes.Interface {
				clientset, controllable := newControllableClient()
				controllable.shouldSucceed.Store(true)

				return &stubClientset{Interface: clientset, discovery: controllable}
			},
			timeout: 5 * time.Second,
			wantErr: false,
		},
		{
			name:              "returns error when timeout exceeded",
			requiredSuccesses: 100, // require many successes, impossible within timeout
			setupClient: func() kubernetes.Interface {
				clientset, controllable := newControllableClient()
				controllable.shouldSucceed.Store(true)

				return &stubClientset{Interface: clientset, discovery: controllable}
			},
			timeout:     100 * time.Millisecond, // short timeout
			wantErr:     true,
			errContains: "failed to poll for readiness",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := testCase.setupClient()

			ctx, cancel := context.WithTimeout(
				context.Background(),
				testCase.timeout+100*time.Millisecond,
			)
			defer cancel()

			err := k8s.WaitForAPIServerStable(
				ctx,
				client,
				testCase.timeout,
				testCase.requiredSuccesses,
			)

			if testCase.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), testCase.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckAPIServerConnectivity(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name        string
		setupClient func() kubernetes.Interface
		wantErr     bool
		errContains string
	}

	tests := []testCase{
		{
			name: "returns nil when API server responds",
			setupClient: func() kubernetes.Interface {
				return fake.NewClientset()
			},
			wantErr: false,
		},
		{
			name: "returns error when API server is unavailable",
			setupClient: func() kubernetes.Interface {
				clientset, controllable := newControllableClient()
				controllable.shouldSucceed.Store(false)

				return &stubClientset{Interface: clientset, discovery: controllable}
			},
			wantErr:     true,
			errContains: "API server connectivity check failed",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := testCase.setupClient()
			err := k8s.CheckAPIServerConnectivity(client)

			if testCase.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), testCase.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
