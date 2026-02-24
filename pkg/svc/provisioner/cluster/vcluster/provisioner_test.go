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

	tests := []struct {
		name           string
		clusterName    string
		valuesPath     string
		disableFlannel bool
	}{
		{
			name:           "creates_with_provided_name",
			clusterName:    "test-cluster",
			valuesPath:     "",
			disableFlannel: false,
		},
		{
			name:           "uses_default_name_when_empty",
			clusterName:    "",
			valuesPath:     "/path/to/values.yaml",
			disableFlannel: false,
		},
		{
			name:           "preserves_all_options",
			clusterName:    "custom",
			valuesPath:     "/custom/path.yaml",
			disableFlannel: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockProvider := provider.NewMockProvider()
			provisioner := vclusterprovisioner.NewProvisioner(
				testCase.clusterName,
				testCase.valuesPath,
				testCase.disableFlannel,
				mockProvider,
			)

			assert.NotNil(t, provisioner, "provisioner should not be nil")
		})
	}
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
