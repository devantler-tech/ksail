package clusterapi_test

import (
	"context"
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/clusterapi"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func TestPodLogsStreamsFromClientset(t *testing.T) {
	t.Parallel()

	service := clusterapi.NewTestService(nil)
	client := fake.NewSimpleClientset()

	service.SetLogClientForTest(
		func(_ context.Context, _ string) (kubernetes.Interface, error) { return client, nil },
	)

	stream, err := service.PodLogs(
		context.Background(), "default", "c1",
		api.LogRequest{Namespace: "x", Pod: "p1", Container: "c", TailLines: 100},
	)
	require.NoError(t, err)

	defer func() { _ = stream.Close() }()

	data, err := io.ReadAll(stream)
	require.NoError(t, err)
	// The fake clientset returns a canned "fake logs" body for GetLogs().Stream().
	assert.Equal(t, "fake logs", string(data))
}

func TestPodLogsPropagatesClientError(t *testing.T) {
	t.Parallel()

	service := clusterapi.NewTestService(nil)
	service.SetLogClientForTest(
		func(_ context.Context, _ string) (kubernetes.Interface, error) { return nil, api.ErrNotFound },
	)

	_, err := service.PodLogs(
		context.Background(), "default", "missing",
		api.LogRequest{Namespace: "x", Pod: "p1"},
	)
	require.ErrorIs(t, err, api.ErrNotFound)
}
