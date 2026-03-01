package vclusterprovisioner_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
	vclusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/vcluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var (
	errMockStartNodesFailed      = errors.New("start nodes failed")
	errMockStopNodesFailed       = errors.New("stop nodes failed")
	errMockListAllClustersFailed = errors.New("list all clusters failed")
)

// runNodeOperationTest is a shared helper for testing Start/Stop node lifecycle operations.
func runNodeOperationTest(
	t *testing.T,
	clusterName, inputName, mockFn string,
	mockErr error,
	operation func(*vclusterprovisioner.Provisioner, context.Context, string) error,
) {
	t.Helper()

	mockProvider := provider.NewMockProvider()
	mockProvider.On(mockFn, mock.Anything, clusterName).Return(mockErr)

	provisioner := vclusterprovisioner.NewProvisioner(clusterName, "", false, mockProvider)
	err := operation(provisioner, context.Background(), inputName)

	if mockErr != nil {
		require.Error(t, err)
		require.ErrorContains(t, err, mockErr.Error())
	} else {
		require.NoError(t, err)
	}

	mockProvider.AssertExpectations(t)
}

func TestNewProvisioner(t *testing.T) {
	t.Parallel()

	t.Run("creates_with_provided_name", func(t *testing.T) {
		t.Parallel()

		mockProvider := provider.NewMockProvider()
		mockProvider.On("StartNodes", mock.Anything, "test-cluster").Return(nil)

		provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProvider)
		require.NotNil(t, provisioner)

		err := provisioner.Start(context.Background(), "test-cluster")
		require.NoError(t, err)
		mockProvider.AssertExpectations(t)
	})

	t.Run("uses_default_name_when_empty", func(t *testing.T) {
		t.Parallel()

		mockProvider := provider.NewMockProvider()
		// When cluster name is empty, the provisioner should use "vcluster-default"
		mockProvider.On("StartNodes", mock.Anything, "vcluster-default").Return(nil)

		provisioner := vclusterprovisioner.NewProvisioner(
			"",
			"/path/to/values.yaml",
			false,
			mockProvider,
		)
		require.NotNil(t, provisioner)

		// Pass empty name to Start; the provisioner should resolve to the default name
		err := provisioner.Start(context.Background(), "")
		require.NoError(t, err)
		mockProvider.AssertExpectations(t)
	})

	t.Run("preserves_all_options", func(t *testing.T) {
		t.Parallel()

		mockProvider := provider.NewMockProvider()
		mockProvider.On("StartNodes", mock.Anything, "custom").Return(nil)

		provisioner := vclusterprovisioner.NewProvisioner(
			"custom",
			"/custom/path.yaml",
			true,
			mockProvider,
		)
		require.NotNil(t, provisioner)

		err := provisioner.Start(context.Background(), "custom")
		require.NoError(t, err)
		mockProvider.AssertExpectations(t)
	})
}

func TestSetProvider(t *testing.T) {
	t.Parallel()

	mockProvider1 := provider.NewMockProvider()
	mockProvider2 := provider.NewMockProvider()

	provisioner := vclusterprovisioner.NewProvisioner(
		"test-cluster",
		"",
		false,
		mockProvider1,
	)

	provisioner.SetProvider(mockProvider2)

	assert.NotNil(t, provisioner, "provisioner should still be valid after SetProvider")
}

func TestStart_Success(t *testing.T) {
	t.Parallel()

	runNodeOperationTest(t, "test-cluster", "test-cluster", "StartNodes", nil,
		func(p *vclusterprovisioner.Provisioner, ctx context.Context, name string) error {
			return p.Start(ctx, name)
		},
	)
}

func TestStart_Error(t *testing.T) {
	t.Parallel()

	runNodeOperationTest(t, "test-cluster", "test-cluster", "StartNodes", errMockStartNodesFailed,
		func(p *vclusterprovisioner.Provisioner, ctx context.Context, name string) error {
			return p.Start(ctx, name)
		},
	)
}

func TestStart_DefaultName(t *testing.T) {
	t.Parallel()

	runNodeOperationTest(t, "default-cluster", "", "StartNodes", nil,
		func(p *vclusterprovisioner.Provisioner, ctx context.Context, name string) error {
			return p.Start(ctx, name)
		},
	)
}

func TestStop_Success(t *testing.T) {
	t.Parallel()

	runNodeOperationTest(t, "test-cluster", "test-cluster", "StopNodes", nil,
		func(p *vclusterprovisioner.Provisioner, ctx context.Context, name string) error {
			return p.Stop(ctx, name)
		},
	)
}

func TestStop_Error(t *testing.T) {
	t.Parallel()

	runNodeOperationTest(t, "test-cluster", "test-cluster", "StopNodes", errMockStopNodesFailed,
		func(p *vclusterprovisioner.Provisioner, ctx context.Context, name string) error {
			return p.Stop(ctx, name)
		},
	)
}

func TestStop_DefaultName(t *testing.T) {
	t.Parallel()

	runNodeOperationTest(t, "default-cluster", "", "StopNodes", nil,
		func(p *vclusterprovisioner.Provisioner, ctx context.Context, name string) error {
			return p.Stop(ctx, name)
		},
	)
}

func TestList_WithClusters(t *testing.T) {
	t.Parallel()

	mockProvider := provider.NewMockProvider()
	mockProvider.On("ListAllClusters", mock.Anything).
		Return([]string{"cluster1", "cluster2"}, nil)

	provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProvider)
	clusters, err := provisioner.List(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"cluster1", "cluster2"}, clusters)
	mockProvider.AssertExpectations(t)
}

func TestList_Empty(t *testing.T) {
	t.Parallel()

	mockProvider := provider.NewMockProvider()
	mockProvider.On("ListAllClusters", mock.Anything).
		Return([]string{}, nil)

	provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProvider)
	clusters, err := provisioner.List(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{}, clusters)
	mockProvider.AssertExpectations(t)
}

func TestList_Error(t *testing.T) {
	t.Parallel()

	mockProvider := provider.NewMockProvider()
	mockProvider.On("ListAllClusters", mock.Anything).
		Return([]string(nil), errMockListAllClustersFailed)

	provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProvider)
	clusters, err := provisioner.List(context.Background())

	require.Error(t, err)
	require.ErrorContains(t, err, "list all clusters failed")
	assert.Nil(t, clusters)
	mockProvider.AssertExpectations(t)
}

func TestList_NilProvider(t *testing.T) {
	t.Parallel()

	provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, nil)
	clusters, err := provisioner.List(context.Background())

	require.Error(t, err)
	require.ErrorIs(t, err, clustererr.ErrProviderNotSet, "should return ErrProviderNotSet")
	assert.Nil(t, clusters, "clusters should be nil on error")
}

func TestExists_Found(t *testing.T) {
	t.Parallel()

	mockProvider := provider.NewMockProvider()
	mockProvider.On("ListAllClusters", mock.Anything).
		Return([]string{"test-cluster", "other-cluster"}, nil)

	provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProvider)
	exists, err := provisioner.Exists(context.Background(), "test-cluster")

	require.NoError(t, err)
	assert.True(t, exists)
	mockProvider.AssertExpectations(t)
}

func TestExists_NotFound(t *testing.T) {
	t.Parallel()

	mockProvider := provider.NewMockProvider()
	mockProvider.On("ListAllClusters", mock.Anything).
		Return([]string{"other-cluster"}, nil)

	provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProvider)
	exists, err := provisioner.Exists(context.Background(), "test-cluster")

	require.NoError(t, err)
	assert.False(t, exists)
	mockProvider.AssertExpectations(t)
}

func TestExists_EmptyList(t *testing.T) {
	t.Parallel()

	mockProvider := provider.NewMockProvider()
	mockProvider.On("ListAllClusters", mock.Anything).
		Return([]string{}, nil)

	provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProvider)
	exists, err := provisioner.Exists(context.Background(), "test-cluster")

	require.NoError(t, err)
	assert.False(t, exists)
	mockProvider.AssertExpectations(t)
}

func TestExists_Error(t *testing.T) {
	t.Parallel()

	mockProvider := provider.NewMockProvider()
	mockProvider.On("ListAllClusters", mock.Anything).
		Return([]string(nil), errMockListAllClustersFailed)

	provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProvider)
	exists, err := provisioner.Exists(context.Background(), "test-cluster")

	require.Error(t, err)
	require.ErrorContains(t, err, "list all clusters failed")
	assert.False(t, exists)
	mockProvider.AssertExpectations(t)
}

func TestExists_DefaultName(t *testing.T) {
	t.Parallel()

	mockProvider := provider.NewMockProvider()
	mockProvider.On("ListAllClusters", mock.Anything).
		Return([]string{"default-cluster"}, nil)

	provisioner := vclusterprovisioner.NewProvisioner("default-cluster", "", false, mockProvider)
	exists, err := provisioner.Exists(context.Background(), "")

	require.NoError(t, err)
	assert.True(t, exists)
	mockProvider.AssertExpectations(t)
}
