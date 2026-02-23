package vclusterprovisioner_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
	vclusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/vcluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errMockProvider       = errors.New("mock provider error")
	errStartNodesFailed   = errors.New("start nodes failed")
	errStopNodesFailed    = errors.New("stop nodes failed")
	errListClustersFailed = errors.New("list clusters failed")
)

func TestNewProvisioner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		clusterName      string
		valuesPath       string
		disableFlannel   bool
		expectedName     string
		expectNonNilProv bool
	}{
		{
			name:             "with_explicit_name",
			clusterName:      "test-cluster",
			valuesPath:       "/path/to/values.yaml",
			disableFlannel:   true,
			expectedName:     "test-cluster",
			expectNonNilProv: true,
		},
		{
			name:             "with_empty_name_uses_default",
			clusterName:      "",
			valuesPath:       "",
			disableFlannel:   false,
			expectedName:     "vcluster-default",
			expectNonNilProv: true,
		},
		{
			name:             "with_values_path",
			clusterName:      "my-cluster",
			valuesPath:       "/custom/values.yaml",
			disableFlannel:   false,
			expectedName:     "my-cluster",
			expectNonNilProv: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockProv := provider.NewMockProvider()
			prov := vclusterprovisioner.NewProvisioner(
				tt.clusterName,
				tt.valuesPath,
				tt.disableFlannel,
				mockProv,
			)

			require.NotNil(t, prov, "NewProvisioner should return non-nil")
		})
	}
}

func TestSetProvider(t *testing.T) {
	t.Parallel()

	mockProv1 := provider.NewMockProvider()
	mockProv2 := provider.NewMockProvider()

	prov := vclusterprovisioner.NewProvisioner("test", "", false, mockProv1)
	require.NotNil(t, prov)

	// Set a different provider
	prov.SetProvider(mockProv2)

	// Verify the new provider is used by calling a method that uses it
	mockProv2.On("ListAllClusters", context.Background()).
		Return([]string{"cluster1"}, nil).
		Once()

	clusters, err := prov.List(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"cluster1"}, clusters)

	mockProv2.AssertExpectations(t)
}

func TestStart_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		clusterName     string
		provisionerName string
		expectedTarget  string
	}{
		{
			name:            "with_explicit_name",
			clusterName:     "test-cluster",
			provisionerName: "default",
			expectedTarget:  "test-cluster",
		},
		{
			name:            "with_empty_name_uses_provisioner_default",
			clusterName:     "",
			provisionerName: "my-default",
			expectedTarget:  "my-default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockProv := provider.NewMockProvider()
			prov := vclusterprovisioner.NewProvisioner(
				tt.provisionerName,
				"",
				false,
				mockProv,
			)

			mockProv.On("StartNodes", context.Background(), tt.expectedTarget).
				Return(nil).
				Once()

			err := prov.Start(context.Background(), tt.clusterName)
			require.NoError(t, err, "Start should succeed")

			mockProv.AssertExpectations(t)
		})
	}
}

func TestStart_ProviderError(t *testing.T) {
	t.Parallel()

	mockProv := provider.NewMockProvider()
	prov := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProv)

	mockProv.On("StartNodes", context.Background(), "test-cluster").
		Return(errStartNodesFailed).
		Once()

	err := prov.Start(context.Background(), "test-cluster")
	require.Error(t, err, "Start should fail when provider fails")
	assert.ErrorIs(t, err, errStartNodesFailed)

	mockProv.AssertExpectations(t)
}

func TestStart_NoProvider(t *testing.T) {
	t.Parallel()

	prov := vclusterprovisioner.NewProvisioner("test-cluster", "", false, nil)

	err := prov.Start(context.Background(), "test-cluster")
	require.Error(t, err, "Start should fail when provider is nil")
	assert.ErrorIs(t, err, clustererr.ErrProviderNotSet)
}

func TestStop_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		clusterName     string
		provisionerName string
		expectedTarget  string
	}{
		{
			name:            "with_explicit_name",
			clusterName:     "test-cluster",
			provisionerName: "default",
			expectedTarget:  "test-cluster",
		},
		{
			name:            "with_empty_name_uses_provisioner_default",
			clusterName:     "",
			provisionerName: "my-default",
			expectedTarget:  "my-default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockProv := provider.NewMockProvider()
			prov := vclusterprovisioner.NewProvisioner(
				tt.provisionerName,
				"",
				false,
				mockProv,
			)

			mockProv.On("StopNodes", context.Background(), tt.expectedTarget).
				Return(nil).
				Once()

			err := prov.Stop(context.Background(), tt.clusterName)
			require.NoError(t, err, "Stop should succeed")

			mockProv.AssertExpectations(t)
		})
	}
}

func TestStop_ProviderError(t *testing.T) {
	t.Parallel()

	mockProv := provider.NewMockProvider()
	prov := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProv)

	mockProv.On("StopNodes", context.Background(), "test-cluster").
		Return(errStopNodesFailed).
		Once()

	err := prov.Stop(context.Background(), "test-cluster")
	require.Error(t, err, "Stop should fail when provider fails")
	assert.ErrorIs(t, err, errStopNodesFailed)

	mockProv.AssertExpectations(t)
}

func TestStop_NoProvider(t *testing.T) {
	t.Parallel()

	prov := vclusterprovisioner.NewProvisioner("test-cluster", "", false, nil)

	err := prov.Stop(context.Background(), "test-cluster")
	require.Error(t, err, "Stop should fail when provider is nil")
	assert.ErrorIs(t, err, clustererr.ErrProviderNotSet)
}

func TestList_Success(t *testing.T) {
	t.Parallel()

	mockProv := provider.NewMockProvider()
	prov := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProv)

	expectedClusters := []string{"cluster1", "cluster2", "cluster3"}
	mockProv.On("ListAllClusters", context.Background()).
		Return(expectedClusters, nil).
		Once()

	clusters, err := prov.List(context.Background())
	require.NoError(t, err, "List should succeed")
	assert.Equal(t, expectedClusters, clusters)

	mockProv.AssertExpectations(t)
}

func TestList_ProviderError(t *testing.T) {
	t.Parallel()

	mockProv := provider.NewMockProvider()
	prov := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProv)

	mockProv.On("ListAllClusters", context.Background()).
		Return([]string(nil), errListClustersFailed).
		Once()

	clusters, err := prov.List(context.Background())
	require.Error(t, err, "List should fail when provider fails")
	assert.ErrorIs(t, err, errListClustersFailed)
	assert.Nil(t, clusters)

	mockProv.AssertExpectations(t)
}

func TestList_NoProvider(t *testing.T) {
	t.Parallel()

	prov := vclusterprovisioner.NewProvisioner("test-cluster", "", false, nil)

	clusters, err := prov.List(context.Background())
	require.Error(t, err, "List should fail when provider is nil")
	assert.ErrorIs(t, err, clustererr.ErrProviderNotSet)
	assert.Nil(t, clusters)
}

func TestExists_ClusterExists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		clusterName     string
		provisionerName string
		clusterList     []string
		expectedExists  bool
	}{
		{
			name:            "cluster_exists",
			clusterName:     "test-cluster",
			provisionerName: "default",
			clusterList:     []string{"test-cluster", "other-cluster"},
			expectedExists:  true,
		},
		{
			name:            "cluster_not_exists",
			clusterName:     "missing-cluster",
			provisionerName: "default",
			clusterList:     []string{"test-cluster", "other-cluster"},
			expectedExists:  false,
		},
		{
			name:            "empty_cluster_list",
			clusterName:     "test-cluster",
			provisionerName: "default",
			clusterList:     []string{},
			expectedExists:  false,
		},
		{
			name:            "uses_provisioner_name_when_empty",
			clusterName:     "",
			provisionerName: "my-cluster",
			clusterList:     []string{"my-cluster", "other-cluster"},
			expectedExists:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockProv := provider.NewMockProvider()
			prov := vclusterprovisioner.NewProvisioner(
				tt.provisionerName,
				"",
				false,
				mockProv,
			)

			mockProv.On("ListAllClusters", context.Background()).
				Return(tt.clusterList, nil).
				Once()

			exists, err := prov.Exists(context.Background(), tt.clusterName)
			require.NoError(t, err, "Exists should not error")
			assert.Equal(t, tt.expectedExists, exists)

			mockProv.AssertExpectations(t)
		})
	}
}

func TestExists_ListError(t *testing.T) {
	t.Parallel()

	mockProv := provider.NewMockProvider()
	prov := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProv)

	mockProv.On("ListAllClusters", context.Background()).
		Return([]string(nil), errMockProvider).
		Once()

	exists, err := prov.Exists(context.Background(), "test-cluster")
	require.Error(t, err, "Exists should fail when List fails")
	assert.ErrorIs(t, err, errMockProvider)
	assert.False(t, exists)

	mockProv.AssertExpectations(t)
}

func TestDelete_ClusterNotFound(t *testing.T) {
	t.Parallel()

	mockProv := provider.NewMockProvider()
	prov := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProv)

	// Mock List to return empty cluster list (cluster doesn't exist)
	mockProv.On("ListAllClusters", context.Background()).
		Return([]string{}, nil).
		Once()

	err := prov.Delete(context.Background(), "test-cluster")
	require.Error(t, err, "Delete should fail when cluster doesn't exist")
	assert.ErrorIs(t, err, clustererr.ErrClusterNotFound)

	mockProv.AssertExpectations(t)
}
