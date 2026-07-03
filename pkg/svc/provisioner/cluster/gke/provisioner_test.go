package gkeprovisioner_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloud.google.com/go/container/apiv1/containerpb"
	"github.com/devantler-tech/ksail/v7/pkg/client/gke"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	gkeprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/gke"
	"github.com/googleapis/gax-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errBoom is the sentinel the fakes fail with.
var errBoom = errors.New("boom")

// fakeClusterManager implements the GKE client's cluster-manager seam with
// injectable behaviour per operation, mirroring pkg/client/gke's test fake.
type fakeClusterManager struct {
	createFunc func(*containerpb.CreateClusterRequest) (*containerpb.Operation, error)
	deleteFunc func(*containerpb.DeleteClusterRequest) (*containerpb.Operation, error)
	listFunc   func(*containerpb.ListClustersRequest) (*containerpb.ListClustersResponse, error)
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
	_ *containerpb.GetClusterRequest,
	_ ...gax.CallOption,
) (*containerpb.Cluster, error) {
	return nil, errBoom
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
	_ *containerpb.SetNodePoolSizeRequest,
	_ ...gax.CallOption,
) (*containerpb.Operation, error) {
	return nil, errBoom
}

func (f *fakeClusterManager) GetOperation(
	_ context.Context,
	_ *containerpb.GetOperationRequest,
	_ ...gax.CallOption,
) (*containerpb.Operation, error) {
	return nil, errBoom
}

func (f *fakeClusterManager) Close() error {
	return nil
}

// fakeProvider records Start/Stop delegation without touching real
// infrastructure. The embedded interface panics on anything not overridden,
// pinning that the provisioner only uses StartNodes/StopNodes.
type fakeProvider struct {
	provider.Provider

	started  []string
	stopped  []string
	startErr error
	stopErr  error
}

func (f *fakeProvider) StartNodes(_ context.Context, clusterName string) error {
	f.started = append(f.started, clusterName)

	return f.startErr
}

func (f *fakeProvider) StopNodes(_ context.Context, clusterName string) error {
	f.stopped = append(f.stopped, clusterName)

	return f.stopErr
}

func doneOperation(name string) *containerpb.Operation {
	return &containerpb.Operation{
		Name:   name,
		Status: containerpb.Operation_DONE,
	}
}

func listResponse(clusters ...*containerpb.Cluster) *containerpb.ListClustersResponse {
	return &containerpb.ListClustersResponse{Clusters: clusters}
}

func namedCluster(name, location string) *containerpb.Cluster {
	return &containerpb.Cluster{Name: name, Location: location}
}

// newProvisioner wires a Provisioner around the fake manager with a fast poll
// interval, mirroring the EKS test constructor.
func newProvisioner(
	t *testing.T,
	fake *fakeClusterManager,
	location string,
	clusterSpec *containerpb.Cluster,
	infra provider.Provider,
) *gkeprovisioner.Provisioner {
	t.Helper()

	client, err := gke.NewClient(
		t.Context(),
		gke.WithClusterManager(fake),
		gke.WithPollInterval(time.Millisecond),
	)
	require.NoError(t, err)

	provisioner, err := gkeprovisioner.NewProvisioner(
		"gke-default", "test-project", location, clusterSpec, client, infra,
	)
	require.NoError(t, err)

	return provisioner
}

func TestNewProvisioner_RequiresClient(t *testing.T) {
	t.Parallel()

	_, err := gkeprovisioner.NewProvisioner(
		"gke-default", "test-project", "europe-north1", nil, nil, nil,
	)

	require.ErrorIs(t, err, gkeprovisioner.ErrClientRequired)
}

func TestNewProvisioner_RequiresProject(t *testing.T) {
	t.Parallel()

	client, err := gke.NewClient(
		t.Context(),
		gke.WithClusterManager(&fakeClusterManager{}),
	)
	require.NoError(t, err)

	_, err = gkeprovisioner.NewProvisioner("gke-default", "", "europe-north1", nil, client, nil)

	require.ErrorIs(t, err, gkeprovisioner.ErrProjectRequired)
}

func TestCreate_RequiresClusterSpec(t *testing.T) {
	t.Parallel()

	provisioner := newProvisioner(t, &fakeClusterManager{}, "europe-north1", nil, nil)

	err := provisioner.Create(t.Context(), "")

	require.ErrorIs(t, err, gkeprovisioner.ErrClusterSpecRequired)
}

func TestCreate_RequiresConcreteLocation(t *testing.T) {
	t.Parallel()

	for _, location := range []string{"", "-"} {
		provisioner := newProvisioner(
			t, &fakeClusterManager{}, location, namedCluster("gke-default", ""), nil,
		)

		err := provisioner.Create(t.Context(), "")

		require.ErrorIs(t, err, gkeprovisioner.ErrLocationRequired)
	}
}

func TestCreate_SubmitsDeclarativeSpec(t *testing.T) {
	t.Parallel()

	var captured *containerpb.CreateClusterRequest

	fake := &fakeClusterManager{
		createFunc: func(req *containerpb.CreateClusterRequest) (*containerpb.Operation, error) {
			captured = req

			return doneOperation("op-create"), nil
		},
	}
	spec := namedCluster("gke-default", "")
	provisioner := newProvisioner(t, fake, "europe-north1", spec, nil)

	err := provisioner.Create(t.Context(), "ignored-flag-name")

	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "projects/test-project/locations/europe-north1", captured.GetParent())
	assert.Equal(t, "gke-default", captured.GetCluster().GetName())
}

func TestCreate_WrapsClientError(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		createFunc: func(_ *containerpb.CreateClusterRequest) (*containerpb.Operation, error) {
			return nil, errBoom
		},
	}
	provisioner := newProvisioner(t, fake, "europe-north1", namedCluster("gke-default", ""), nil)

	err := provisioner.Create(t.Context(), "")

	require.ErrorIs(t, err, errBoom)
	require.ErrorContains(t, err, "gke create cluster")
}

func TestDelete_UsesPinnedLocation(t *testing.T) {
	t.Parallel()

	var captured *containerpb.DeleteClusterRequest

	fake := &fakeClusterManager{
		deleteFunc: func(req *containerpb.DeleteClusterRequest) (*containerpb.Operation, error) {
			captured = req

			return doneOperation("op-delete"), nil
		},
	}
	provisioner := newProvisioner(t, fake, "europe-north1", nil, nil)

	err := provisioner.Delete(t.Context(), "")

	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(
		t,
		"projects/test-project/locations/europe-north1/clusters/gke-default",
		captured.GetName(),
	)
}

func TestDelete_ResolvesLocationWhenUnpinned(t *testing.T) {
	t.Parallel()

	var (
		capturedList   *containerpb.ListClustersRequest
		capturedDelete *containerpb.DeleteClusterRequest
	)

	fake := &fakeClusterManager{
		listFunc: func(
			req *containerpb.ListClustersRequest,
		) (*containerpb.ListClustersResponse, error) {
			capturedList = req

			return listResponse(namedCluster("gke-default", "us-central1")), nil
		},
		deleteFunc: func(req *containerpb.DeleteClusterRequest) (*containerpb.Operation, error) {
			capturedDelete = req

			return doneOperation("op-delete"), nil
		},
	}
	provisioner := newProvisioner(t, fake, "", nil, nil)

	err := provisioner.Delete(t.Context(), "")

	require.NoError(t, err)
	require.NotNil(t, capturedList)
	assert.Equal(t, "projects/test-project/locations/-", capturedList.GetParent())
	require.NotNil(t, capturedDelete)
	assert.Equal(
		t,
		"projects/test-project/locations/us-central1/clusters/gke-default",
		capturedDelete.GetName(),
	)
}

func TestDelete_UnknownClusterFailsResolution(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		listFunc: func(
			_ *containerpb.ListClustersRequest,
		) (*containerpb.ListClustersResponse, error) {
			return listResponse(), nil
		},
	}
	provisioner := newProvisioner(t, fake, "", nil, nil)

	err := provisioner.Delete(t.Context(), "missing")

	require.ErrorIs(t, err, gkeprovisioner.ErrClusterNotFound)
}

func TestDelete_EmptyTargetFailsFastWithoutCalls(t *testing.T) {
	t.Parallel()

	client, err := gke.NewClient(
		t.Context(),
		gke.WithClusterManager(&fakeClusterManager{}),
	)
	require.NoError(t, err)

	provisioner, err := gkeprovisioner.NewProvisioner(
		"", "test-project", "europe-north1", nil, client, nil,
	)
	require.NoError(t, err)

	err = provisioner.Delete(t.Context(), "")

	require.ErrorIs(t, err, gkeprovisioner.ErrClusterNotFound)
}

func TestStart_DelegatesToProvider(t *testing.T) {
	t.Parallel()

	infra := &fakeProvider{}
	provisioner := newProvisioner(t, &fakeClusterManager{}, "europe-north1", nil, infra)

	err := provisioner.Start(t.Context(), "")

	require.NoError(t, err)
	assert.Equal(t, []string{"gke-default"}, infra.started)
}

func TestStart_RequiresProvider(t *testing.T) {
	t.Parallel()

	provisioner := newProvisioner(t, &fakeClusterManager{}, "europe-north1", nil, nil)

	err := provisioner.Start(t.Context(), "")

	require.ErrorIs(t, err, clustererr.ErrUnsupportedProvider)
}

func TestStop_DelegatesToProvider(t *testing.T) {
	t.Parallel()

	infra := &fakeProvider{}
	provisioner := newProvisioner(t, &fakeClusterManager{}, "europe-north1", nil, infra)

	err := provisioner.Stop(t.Context(), "named")

	require.NoError(t, err)
	assert.Equal(t, []string{"named"}, infra.stopped)
}

func TestStop_RequiresProvider(t *testing.T) {
	t.Parallel()

	provisioner := newProvisioner(t, &fakeClusterManager{}, "europe-north1", nil, nil)

	err := provisioner.Stop(t.Context(), "")

	require.ErrorIs(t, err, clustererr.ErrUnsupportedProvider)
}

func TestStop_WrapsProviderError(t *testing.T) {
	t.Parallel()

	infra := &fakeProvider{stopErr: errBoom}
	provisioner := newProvisioner(t, &fakeClusterManager{}, "europe-north1", nil, infra)

	err := provisioner.Stop(t.Context(), "")

	require.ErrorIs(t, err, errBoom)
	require.ErrorContains(t, err, "stop nodes")
}

func TestSetProvider_Overrides(t *testing.T) {
	t.Parallel()

	original := &fakeProvider{}
	replacement := &fakeProvider{}
	provisioner := newProvisioner(t, &fakeClusterManager{}, "europe-north1", nil, original)

	provisioner.SetProvider(replacement)
	err := provisioner.Start(t.Context(), "")

	require.NoError(t, err)
	assert.Empty(t, original.started)
	assert.Equal(t, []string{"gke-default"}, replacement.started)
}

func TestList_ReturnsClusterNamesAcrossAllLocations(t *testing.T) {
	t.Parallel()

	var captured *containerpb.ListClustersRequest

	fake := &fakeClusterManager{
		listFunc: func(
			req *containerpb.ListClustersRequest,
		) (*containerpb.ListClustersResponse, error) {
			captured = req

			return listResponse(
				namedCluster("alpha", "us-central1"),
				namedCluster("beta", "europe-north1"),
			), nil
		},
	}
	provisioner := newProvisioner(t, fake, "", nil, nil)

	names, err := provisioner.List(t.Context())

	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "projects/test-project/locations/-", captured.GetParent())
	assert.Equal(t, []string{"alpha", "beta"}, names)
}

func TestList_WrapsClientError(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		listFunc: func(
			_ *containerpb.ListClustersRequest,
		) (*containerpb.ListClustersResponse, error) {
			return nil, errBoom
		},
	}
	provisioner := newProvisioner(t, fake, "europe-north1", nil, nil)

	_, err := provisioner.List(t.Context())

	require.ErrorIs(t, err, errBoom)
	require.ErrorContains(t, err, "gke list clusters")
}

func TestExists_ReportsMembership(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		listFunc: func(
			_ *containerpb.ListClustersRequest,
		) (*containerpb.ListClustersResponse, error) {
			return listResponse(namedCluster("gke-default", "europe-north1")), nil
		},
	}
	provisioner := newProvisioner(t, fake, "europe-north1", nil, nil)

	found, err := provisioner.Exists(t.Context(), "")
	require.NoError(t, err)
	assert.True(t, found)

	found, err = provisioner.Exists(t.Context(), "missing")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestExists_EmptyTargetIsFalseWithoutCalls(t *testing.T) {
	t.Parallel()

	client, err := gke.NewClient(
		t.Context(),
		gke.WithClusterManager(&fakeClusterManager{}),
	)
	require.NoError(t, err)

	provisioner, err := gkeprovisioner.NewProvisioner(
		"", "test-project", "europe-north1", nil, client, nil,
	)
	require.NoError(t, err)

	found, err := provisioner.Exists(t.Context(), "")

	require.NoError(t, err)
	assert.False(t, found)
}
