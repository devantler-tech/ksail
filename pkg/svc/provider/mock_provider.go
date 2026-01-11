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

	return args.Error(0) //nolint:wrapcheck // Mock function, wrapping not needed
}

// StopNodes mocks stopping nodes for a cluster.
func (m *MockProvider) StopNodes(ctx context.Context, clusterName string) error {
	args := m.Called(ctx, clusterName)

	return args.Error(0) //nolint:wrapcheck // Mock function, wrapping not needed
}

// ListNodes mocks listing nodes for a cluster.
func (m *MockProvider) ListNodes(ctx context.Context, clusterName string) ([]NodeInfo, error) {
	args := m.Called(ctx, clusterName)

	result, ok := args.Get(0).([]NodeInfo)
	if !ok {
		return nil, args.Error(1) //nolint:wrapcheck // Mock function, wrapping not needed
	}

	return result, args.Error(1) //nolint:wrapcheck // Mock function, wrapping not needed
}

// ListAllClusters mocks listing all clusters.
func (m *MockProvider) ListAllClusters(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)

	result, ok := args.Get(0).([]string)
	if !ok {
		return nil, args.Error(1) //nolint:wrapcheck // Mock function, wrapping not needed
	}

	return result, args.Error(1) //nolint:wrapcheck // Mock function, wrapping not needed
}

// NodesExist mocks checking if nodes exist.
func (m *MockProvider) NodesExist(ctx context.Context, clusterName string) (bool, error) {
	args := m.Called(ctx, clusterName)

	return args.Bool(0), args.Error(1)
}

// DeleteNodes mocks deleting nodes for a cluster.
func (m *MockProvider) DeleteNodes(ctx context.Context, clusterName string) error {
	args := m.Called(ctx, clusterName)

	return args.Error(0) //nolint:wrapcheck // Mock function, wrapping not needed
}
