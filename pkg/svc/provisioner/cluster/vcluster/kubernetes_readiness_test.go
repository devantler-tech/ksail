package vclusterprovisioner_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	vclusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/vcluster"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestMergeReadyVClusterKubeconfigRetainsAPIErrorBeforeCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var calls atomic.Int32

	client, err := kubernetes.NewForConfigAndClient(
		&rest.Config{Host: "https://vcluster.test"},
		&http.Client{
			Transport: readinessRoundTripper(func(request *http.Request) (*http.Response, error) {
				require.Equal(t, "/readyz", request.URL.Path)

				if calls.Add(1) == 1 {
					return &http.Response{
						StatusCode: http.StatusUnauthorized,
						Header:     http.Header{"Content-Type": {"application/json"}},
						Body: io.NopCloser(strings.NewReader(
							`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Unauthorized","code":401}`,
						)),
					}, nil
				}

				cancel()

				return nil, request.Context().Err()
			}),
		},
	)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "host-kubeconfig")
	original := []byte("preserve the existing host connection\n")
	require.NoError(t, os.WriteFile(path, original, 0o600))

	err = vclusterprovisioner.MergeReadyVClusterKubeconfigWithClientForTest(
		ctx, path, nil, client, time.Millisecond, time.Second,
	)
	require.ErrorIs(t, err, context.Canceled)
	require.True(
		t,
		apierrors.IsUnauthorized(err),
		"the API answer must survive cancellation: %v",
		err,
	)
	require.ErrorContains(t, err, "provide credentials")
	require.GreaterOrEqual(t, calls.Load(), int32(2))

	//nolint:gosec // G304: path is created by the test (temp directory).
	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	require.Equal(t, original, after, "a failed probe must preserve the host connection")
}

type readinessRoundTripper func(*http.Request) (*http.Response, error)

func (transport readinessRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}
