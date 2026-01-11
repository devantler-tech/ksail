package provider

import (
	"context"

	"github.com/stretchr/testify/mock"
)

// MockProvider is a mock implementation of the Provider interface for testing.
type MockProvider struct {
	mock.Mock
}

// NewMockProvider creates a new MockProvider instance.
func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

// StartNodes mocks starting nodes for a cluster.
func (m *MockProvider) StartNodes(ctx context.Context, clusterName string) error {
	args := m.Called(ctx, clusterName)
	return args.Error(0)
}

// StopNodes mocks stopping nodes for a cluster.
func (m *MockProvider) StopNodes(ctx context.Context, clusterName string) error {
	args := m.Called(ctx, clusterName)
	return args.Error(0)
}

// ListNodes mocks listing nodes for a cluster.
func (m *MockProvider) ListNodes(ctx context.Context, clusterName string) ([]NodeInfo, error) {
	args := m.Called(ctx, clusterName)
	if result := args.Get(0); result != nil {
		return result.([]NodeInfo), args.Error(1)
	}
	return nil, args.Error(1)
}

// ListAllClusters mocks listing all clusters.
func (m *MockProvider) ListAllClusters(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	if result := args.Get(0); result != nil {
		return result.([]string), args.Error(1)
	}
	return nil, args.Error(1)
}

// NodesExist mocks checking if nodes exist.
func (m *MockProvider) NodesExist(ctx context.Context, clusterName string) (bool, error) {
	args := m.Called(ctx, clusterName)
	return args.Bool(0), args.Error(1)
}

// DeleteNodes mocks deleting nodes for a cluster.
func (m *MockProvider) DeleteNodes(ctx context.Context, clusterName string) error {
	args := m.Called(ctx, clusterName)
	return args.Error(0)
}
