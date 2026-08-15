package gke_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloud.google.com/go/container/apiv1/containerpb"
	"github.com/devantler-tech/ksail/v7/pkg/client/gke"
	"github.com/googleapis/gax-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/status"
)

// errBoom is the sentinel the fakes fail with.
var errBoom = errors.New("boom")

// fakeClusterManager implements the client's cluster-manager seam with
// injectable behaviour per operation.
type fakeClusterManager struct {
	createFunc  func(*containerpb.CreateClusterRequest) (*containerpb.Operation, error)
	deleteFunc  func(*containerpb.DeleteClusterRequest) (*containerpb.Operation, error)
	getFunc     func(*containerpb.GetClusterRequest) (*containerpb.Cluster, error)
	listFunc    func(*containerpb.ListClustersRequest) (*containerpb.ListClustersResponse, error)
	setSizeFunc func(*containerpb.SetNodePoolSizeRequest) (*containerpb.Operation, error)
	opFunc      func(*containerpb.GetOperationRequest) (*containerpb.Operation, error)
	closeErr    error
}

func (f *fakeClusterManager) CreateCluster(
	_ context.Context,
	req *containerpb.CreateClusterRequest,
	_ ...gax.CallOption,
) (*containerpb.Operation, error) {
	return f.createFunc(req)
}

func (f *fakeClusterManager) DeleteCluster(
	_ context.Context,
	req *containerpb.DeleteClusterRequest,
	_ ...gax.CallOption,
) (*containerpb.Operation, error) {
	return f.deleteFunc(req)
}

func (f *fakeClusterManager) GetCluster(
	_ context.Context,
	req *containerpb.GetClusterRequest,
	_ ...gax.CallOption,
) (*containerpb.Cluster, error) {
	return f.getFunc(req)
}

func (f *fakeClusterManager) ListClusters(
	_ context.Context,
	req *containerpb.ListClustersRequest,
	_ ...gax.CallOption,
) (*containerpb.ListClustersResponse, error) {
	return f.listFunc(req)
}

func (f *fakeClusterManager) SetNodePoolSize(
	_ context.Context,
	req *containerpb.SetNodePoolSizeRequest,
	_ ...gax.CallOption,
) (*containerpb.Operation, error) {
	return f.setSizeFunc(req)
}

func (f *fakeClusterManager) GetOperation(
	_ context.Context,
	req *containerpb.GetOperationRequest,
	_ ...gax.CallOption,
) (*containerpb.Operation, error) {
	return f.opFunc(req)
}

func (f *fakeClusterManager) Close() error {
	return f.closeErr
}

// newTestClient builds a Client around the fake with a fast poll interval.
func newTestClient(t *testing.T, fake *fakeClusterManager) *gke.Client {
	t.Helper()

	client, err := gke.NewClient(
		t.Context(),
		gke.WithClusterManager(fake),
		gke.WithPollInterval(time.Millisecond),
	)
	require.NoError(t, err)

	return client
}

func doneOperation(name string) *containerpb.Operation {
	return &containerpb.Operation{
		Name:   name,
		Status: containerpb.Operation_DONE,
	}
}

func runningOperation() *containerpb.Operation {
	return &containerpb.Operation{
		Name:   "op-create",
		Status: containerpb.Operation_RUNNING,
	}
}

func TestCreateClusterWaitsForOperation(t *testing.T) {
	t.Parallel()

	polls := 0
	fake := &fakeClusterManager{
		createFunc: func(req *containerpb.CreateClusterRequest) (*containerpb.Operation, error) {
			assert.Equal(t, "projects/proj/locations/europe-north1", req.GetParent())
			assert.Equal(t, "demo", req.GetCluster().GetName())

			return runningOperation(), nil
		},
		opFunc: func(req *containerpb.GetOperationRequest) (*containerpb.Operation, error) {
			assert.Equal(
				t,
				"projects/proj/locations/europe-north1/operations/op-create",
				req.GetName(),
			)

			polls++
			if polls < 2 {
				return runningOperation(), nil
			}

			return doneOperation("op-create"), nil
		},
	}

	client := newTestClient(t, fake)

	err := client.CreateCluster(
		t.Context(), "proj", "europe-north1", &containerpb.Cluster{Name: "demo"},
	)

	require.NoError(t, err)
	assert.Equal(t, 2, polls)
}

func TestCreateClusterNilClusterIsRejected(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, &fakeClusterManager{})

	err := client.CreateCluster(t.Context(), "proj", "loc", nil)

	require.ErrorIs(t, err, gke.ErrNilCluster)
}

func TestCreateClusterWrapsRequestError(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		createFunc: func(*containerpb.CreateClusterRequest) (*containerpb.Operation, error) {
			return nil, errBoom
		},
	}

	client := newTestClient(t, fake)

	err := client.CreateCluster(t.Context(), "proj", "loc", &containerpb.Cluster{Name: "demo"})

	require.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), `creating GKE cluster "demo"`)
}

func TestCreateClusterSurfacesOperationError(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		createFunc: func(*containerpb.CreateClusterRequest) (*containerpb.Operation, error) {
			operation := doneOperation("op-create")
			operation.Error = &status.Status{Message: "quota exceeded"}

			return operation, nil
		},
	}

	client := newTestClient(t, fake)

	err := client.CreateCluster(t.Context(), "proj", "loc", &containerpb.Cluster{Name: "demo"})

	require.ErrorIs(t, err, gke.ErrOperationFailed)
	assert.Contains(t, err.Error(), "quota exceeded")
}

func TestCreateClusterHonoursContextCancellation(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		createFunc: func(*containerpb.CreateClusterRequest) (*containerpb.Operation, error) {
			return runningOperation(), nil
		},
	}

	client, err := gke.NewClient(
		t.Context(),
		gke.WithClusterManager(fake),
		gke.WithPollInterval(time.Hour),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err = client.CreateCluster(ctx, "proj", "loc", &containerpb.Cluster{Name: "demo"})

	require.ErrorIs(t, err, context.Canceled)
}

func TestCreateClusterWrapsPollError(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		createFunc: func(*containerpb.CreateClusterRequest) (*containerpb.Operation, error) {
			return runningOperation(), nil
		},
		opFunc: func(*containerpb.GetOperationRequest) (*containerpb.Operation, error) {
			return nil, errBoom
		},
	}

	client := newTestClient(t, fake)

	err := client.CreateCluster(t.Context(), "proj", "loc", &containerpb.Cluster{Name: "demo"})

	require.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), `polling GKE operation "op-create"`)
}

func TestDeleteClusterWaitsForOperation(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		deleteFunc: func(req *containerpb.DeleteClusterRequest) (*containerpb.Operation, error) {
			assert.Equal(t, "projects/proj/locations/loc/clusters/demo", req.GetName())

			return doneOperation("op-delete"), nil
		},
	}

	client := newTestClient(t, fake)

	err := client.DeleteCluster(t.Context(), "proj", "loc", "demo")

	require.NoError(t, err)
}

func TestDeleteClusterWrapsRequestError(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		deleteFunc: func(*containerpb.DeleteClusterRequest) (*containerpb.Operation, error) {
			return nil, errBoom
		},
	}

	client := newTestClient(t, fake)

	err := client.DeleteCluster(t.Context(), "proj", "loc", "demo")

	require.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), `deleting GKE cluster "demo"`)
}

func TestGetClusterReturnsCluster(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		getFunc: func(req *containerpb.GetClusterRequest) (*containerpb.Cluster, error) {
			assert.Equal(t, "projects/proj/locations/loc/clusters/demo", req.GetName())

			return &containerpb.Cluster{Name: "demo"}, nil
		},
	}

	client := newTestClient(t, fake)

	cluster, err := client.GetCluster(t.Context(), "proj", "loc", "demo")

	require.NoError(t, err)
	assert.Equal(t, "demo", cluster.GetName())
}

func TestGetClusterWrapsError(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		getFunc: func(*containerpb.GetClusterRequest) (*containerpb.Cluster, error) {
			return nil, errBoom
		},
	}

	client := newTestClient(t, fake)

	_, err := client.GetCluster(t.Context(), "proj", "loc", "demo")

	require.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), `getting GKE cluster "demo"`)
}

func TestListClustersReturnsClusters(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		listFunc: func(req *containerpb.ListClustersRequest) (*containerpb.ListClustersResponse, error) {
			assert.Equal(t, "projects/proj/locations/-", req.GetParent())

			return &containerpb.ListClustersResponse{
				Clusters: []*containerpb.Cluster{{Name: "one"}, {Name: "two"}},
			}, nil
		},
	}

	client := newTestClient(t, fake)

	clusters, err := client.ListClusters(t.Context(), "proj", "-")

	require.NoError(t, err)
	require.Len(t, clusters, 2)
	assert.Equal(t, "one", clusters[0].GetName())
}

func TestListClustersWrapsError(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		listFunc: func(*containerpb.ListClustersRequest) (*containerpb.ListClustersResponse, error) {
			return nil, errBoom
		},
	}

	client := newTestClient(t, fake)

	_, err := client.ListClusters(t.Context(), "proj", "loc")

	require.ErrorIs(t, err, errBoom)
	assert.NotContains(t, err.Error(), "loc")
}

func TestCloseWrapsError(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, &fakeClusterManager{closeErr: errBoom})

	err := client.Close()

	require.ErrorIs(t, err, errBoom)
}

func TestCloseSucceeds(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, &fakeClusterManager{})

	require.NoError(t, client.Close())
}

func TestSetNodePoolSizeWaitsForOperation(t *testing.T) {
	t.Parallel()

	polls := 0
	fake := &fakeClusterManager{
		setSizeFunc: func(req *containerpb.SetNodePoolSizeRequest) (*containerpb.Operation, error) {
			assert.Equal(
				t,
				"projects/proj/locations/europe-north1/clusters/demo/nodePools/default-pool",
				req.GetName(),
			)
			assert.Equal(t, int32(3), req.GetNodeCount())

			operation := runningOperation()
			operation.Name = "op-resize"

			return operation, nil
		},
		opFunc: func(req *containerpb.GetOperationRequest) (*containerpb.Operation, error) {
			assert.Equal(
				t,
				"projects/proj/locations/europe-north1/operations/op-resize",
				req.GetName(),
			)

			polls++

			return doneOperation("op-resize"), nil
		},
	}

	client := newTestClient(t, fake)

	err := client.SetNodePoolSize(
		t.Context(), "proj", "europe-north1", "demo", "default-pool", 3,
	)

	require.NoError(t, err)
	assert.Equal(t, 1, polls)
}

func TestSetNodePoolSizeWrapsRequestError(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		setSizeFunc: func(*containerpb.SetNodePoolSizeRequest) (*containerpb.Operation, error) {
			return nil, errBoom
		},
	}

	client := newTestClient(t, fake)

	err := client.SetNodePoolSize(t.Context(), "proj", "loc", "demo", "default-pool", 0)

	require.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), `resizing GKE node pool "default-pool" to 0`)
}

func TestSetNodePoolSizeSurfacesOperationError(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		setSizeFunc: func(*containerpb.SetNodePoolSizeRequest) (*containerpb.Operation, error) {
			operation := doneOperation("op-resize")
			operation.Error = &status.Status{Message: "node pool is being repaired"}

			return operation, nil
		},
	}

	client := newTestClient(t, fake)

	err := client.SetNodePoolSize(t.Context(), "proj", "loc", "demo", "default-pool", 1)

	require.ErrorIs(t, err, gke.ErrOperationFailed)
	assert.Contains(t, err.Error(), "node pool is being repaired")
}
