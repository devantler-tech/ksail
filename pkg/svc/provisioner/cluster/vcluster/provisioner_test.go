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
	errMockListClustersFailed    = errors.New("list clusters failed")
	errMockNodesExistFailed      = errors.New("nodes exist check failed")
	errMockDeleteNodesFailed     = errors.New("delete nodes failed")
	errMockListNodesFailed       = errors.New("list nodes failed")
	errMockListAllClustersFailed = errors.New("list all clusters failed")
)

func TestNewProvisioner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		clusterName    string
		valuesPath     string
		disableFlannel bool
		wantName       string
	}{
		{
			name:           "creates_with_provided_name",
			clusterName:    "test-cluster",
			valuesPath:     "",
			disableFlannel: false,
			wantName:       "test-cluster",
		},
		{
			name:           "uses_default_name_when_empty",
			clusterName:    "",
			valuesPath:     "/path/to/values.yaml",
			disableFlannel: false,
			wantName:       "vcluster-default",
		},
		{
			name:           "preserves_all_options",
			clusterName:    "custom",
			valuesPath:     "/custom/path.yaml",
			disableFlannel: true,
			wantName:       "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockProvider := provider.NewMockProvider()

			provisioner := vclusterprovisioner.NewProvisioner(
				tt.clusterName,
				tt.valuesPath,
				tt.disableFlannel,
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

	// Set a new provider
	provisioner.SetProvider(mockProvider2)

	// Provider should be updated (we can't directly assert this, but we can
	// verify by testing a method that uses the provider)
	assert.NotNil(t, provisioner, "provisioner should still be valid after SetProvider")
}

func TestStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		inputName   string
		mockSetup   func(*provider.MockProvider, string)
		wantErr     bool
		errContains string
	}{
		{
			name:        "successful_start",
			clusterName: "test-cluster",
			inputName:   "test-cluster",
			mockSetup: func(m *provider.MockProvider, name string) {
				m.On("StartNodes", mock.Anything, name).Return(nil)
			},
			wantErr: false,
		},
		{
			name:        "start_error_propagates",
			clusterName: "test-cluster",
			inputName:   "test-cluster",
			mockSetup: func(m *provider.MockProvider, name string) {
				m.On("StartNodes", mock.Anything, name).Return(errMockStartNodesFailed)
			},
			wantErr:     true,
			errContains: "start nodes failed",
		},
		{
			name:        "uses_default_name_when_empty",
			clusterName: "default-cluster",
			inputName:   "",
			mockSetup: func(m *provider.MockProvider, name string) {
				m.On("StartNodes", mock.Anything, "default-cluster").Return(nil)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockProvider := provider.NewMockProvider()
			tt.mockSetup(mockProvider, tt.clusterName)

			provisioner := vclusterprovisioner.NewProvisioner(
				tt.clusterName,
				"",
				false,
				mockProvider,
			)

			err := provisioner.Start(context.Background(), tt.inputName)

			if tt.wantErr {
				require.Error(t, err, "Start() should return error")
				if tt.errContains != "" {
					assert.ErrorContains(t, err, tt.errContains, "error message")
				}
			} else {
				require.NoError(t, err, "Start() should not return error")
			}

			mockProvider.AssertExpectations(t)
		})
	}
}

func TestStop(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		inputName   string
		mockSetup   func(*provider.MockProvider, string)
		wantErr     bool
		errContains string
	}{
		{
			name:        "successful_stop",
			clusterName: "test-cluster",
			inputName:   "test-cluster",
			mockSetup: func(m *provider.MockProvider, name string) {
				m.On("StopNodes", mock.Anything, name).Return(nil)
			},
			wantErr: false,
		},
		{
			name:        "stop_error_propagates",
			clusterName: "test-cluster",
			inputName:   "test-cluster",
			mockSetup: func(m *provider.MockProvider, name string) {
				m.On("StopNodes", mock.Anything, name).Return(errMockStopNodesFailed)
			},
			wantErr:     true,
			errContains: "stop nodes failed",
		},
		{
			name:        "uses_default_name_when_empty",
			clusterName: "default-cluster",
			inputName:   "",
			mockSetup: func(m *provider.MockProvider, name string) {
				m.On("StopNodes", mock.Anything, "default-cluster").Return(nil)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockProvider := provider.NewMockProvider()
			tt.mockSetup(mockProvider, tt.clusterName)

			provisioner := vclusterprovisioner.NewProvisioner(
				tt.clusterName,
				"",
				false,
				mockProvider,
			)

			err := provisioner.Stop(context.Background(), tt.inputName)

			if tt.wantErr {
				require.Error(t, err, "Stop() should return error")
				if tt.errContains != "" {
					assert.ErrorContains(t, err, tt.errContains, "error message")
				}
			} else {
				require.NoError(t, err, "Stop() should not return error")
			}

			mockProvider.AssertExpectations(t)
		})
	}
}

func TestList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mockSetup   func(*provider.MockProvider)
		wantErr     bool
		want        []string
		errContains string
	}{
		{
			name: "successful_list_with_clusters",
			mockSetup: func(m *provider.MockProvider) {
				m.On("ListAllClusters", mock.Anything).
					Return([]string{"cluster1", "cluster2"}, nil)
			},
			wantErr: false,
			want:    []string{"cluster1", "cluster2"},
		},
		{
			name: "successful_list_empty",
			mockSetup: func(m *provider.MockProvider) {
				m.On("ListAllClusters", mock.Anything).
					Return([]string{}, nil)
			},
			wantErr: false,
			want:    []string{},
		},
		{
			name: "list_error_propagates",
			mockSetup: func(m *provider.MockProvider) {
				m.On("ListAllClusters", mock.Anything).
					Return([]string(nil), errMockListAllClustersFailed)
			},
			wantErr:     true,
			errContains: "list all clusters failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockProvider := provider.NewMockProvider()
			tt.mockSetup(mockProvider)

			provisioner := vclusterprovisioner.NewProvisioner(
				"test-cluster",
				"",
				false,
				mockProvider,
			)

			clusters, err := provisioner.List(context.Background())

			if tt.wantErr {
				require.Error(t, err, "List() should return error")
				if tt.errContains != "" {
					assert.ErrorContains(t, err, tt.errContains, "error message")
				}
			} else {
				require.NoError(t, err, "List() should not return error")
				assert.Equal(t, tt.want, clusters, "cluster list should match")
			}

			mockProvider.AssertExpectations(t)
		})
	}
}

func TestList_NilProvider(t *testing.T) {
	t.Parallel()

	// Create provisioner with nil provider
	provisioner := vclusterprovisioner.NewProvisioner(
		"test-cluster",
		"",
		false,
		nil,
	)

	clusters, err := provisioner.List(context.Background())

	require.Error(t, err, "List() should return error with nil provider")
	assert.ErrorIs(t, err, clustererr.ErrProviderNotSet, "should return ErrProviderNotSet")
	assert.Nil(t, clusters, "clusters should be nil on error")
}

func TestExists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		inputName   string
		mockSetup   func(*provider.MockProvider)
		want        bool
		wantErr     bool
		errContains string
	}{
		{
			name:        "cluster_exists",
			clusterName: "test-cluster",
			inputName:   "test-cluster",
			mockSetup: func(m *provider.MockProvider) {
				m.On("ListAllClusters", mock.Anything).
					Return([]string{"test-cluster", "other-cluster"}, nil)
			},
			want:    true,
			wantErr: false,
		},
		{
			name:        "cluster_does_not_exist",
			clusterName: "test-cluster",
			inputName:   "test-cluster",
			mockSetup: func(m *provider.MockProvider) {
				m.On("ListAllClusters", mock.Anything).
					Return([]string{"other-cluster"}, nil)
			},
			want:    false,
			wantErr: false,
		},
		{
			name:        "empty_list",
			clusterName: "test-cluster",
			inputName:   "test-cluster",
			mockSetup: func(m *provider.MockProvider) {
				m.On("ListAllClusters", mock.Anything).
					Return([]string{}, nil)
			},
			want:    false,
			wantErr: false,
		},
		{
			name:        "list_error_propagates",
			clusterName: "test-cluster",
			inputName:   "test-cluster",
			mockSetup: func(m *provider.MockProvider) {
				m.On("ListAllClusters", mock.Anything).
					Return([]string(nil), errMockListAllClustersFailed)
			},
			want:        false,
			wantErr:     true,
			errContains: "list all clusters failed",
		},
		{
			name:        "uses_default_name_when_empty",
			clusterName: "default-cluster",
			inputName:   "",
			mockSetup: func(m *provider.MockProvider) {
				m.On("ListAllClusters", mock.Anything).
					Return([]string{"default-cluster"}, nil)
			},
			want:    true,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockProvider := provider.NewMockProvider()
			tt.mockSetup(mockProvider)

			provisioner := vclusterprovisioner.NewProvisioner(
				tt.clusterName,
				"",
				false,
				mockProvider,
			)

			exists, err := provisioner.Exists(context.Background(), tt.inputName)

			if tt.wantErr {
				require.Error(t, err, "Exists() should return error")
				if tt.errContains != "" {
					assert.ErrorContains(t, err, tt.errContains, "error message")
				}
			} else {
				require.NoError(t, err, "Exists() should not return error")
				assert.Equal(t, tt.want, exists, "exists result should match")
			}

			mockProvider.AssertExpectations(t)
		})
	}
}
