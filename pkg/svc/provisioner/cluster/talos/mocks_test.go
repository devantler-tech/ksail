package talosprovisioner_test

import (
	"context"

	"github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/bundle"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/siderolabs/talos/pkg/provision"
	"github.com/stretchr/testify/mock"
)

// MockCluster is a mock implementation of provision.Cluster.
type MockCluster struct {
	mock.Mock
}

func NewMockCluster() *MockCluster {
	return &MockCluster{}
}

func (m *MockCluster) Provisioner() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockCluster) StatePath() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockCluster) Info() provision.ClusterInfo {
	args := m.Called()
	return args.Get(0).(provision.ClusterInfo)
}

// MockProvisioner is a mock implementation of provision.Provisioner.
type MockProvisioner struct {
	mock.Mock
}

func NewMockProvisioner() *MockProvisioner {
	return &MockProvisioner{}
}

func (m *MockProvisioner) Create(
	ctx context.Context,
	req provision.ClusterRequest,
	opts ...provision.Option,
) (provision.Cluster, error) {
	args := m.Called(ctx, req, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(provision.Cluster), args.Error(1)
}

func (m *MockProvisioner) Destroy(
	ctx context.Context,
	cluster provision.Cluster,
	opts ...provision.Option,
) error {
	args := m.Called(ctx, cluster, opts)
	return args.Error(0)
}

func (m *MockProvisioner) Reflect(
	ctx context.Context,
	clusterName, stateDirectory string,
) (provision.Cluster, error) {
	args := m.Called(ctx, clusterName, stateDirectory)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(provision.Cluster), args.Error(1)
}

func (m *MockProvisioner) GenOptions(
	req provision.NetworkRequest,
	versionContract *config.VersionContract,
) ([]generate.Option, []bundle.Option) {
	args := m.Called(req, versionContract)
	var genOpts []generate.Option
	var bundleOpts []bundle.Option
	if args.Get(0) != nil {
		genOpts = args.Get(0).([]generate.Option)
	}
	if args.Get(1) != nil {
		bundleOpts = args.Get(1).([]bundle.Option)
	}
	return genOpts, bundleOpts
}

func (m *MockProvisioner) GetInClusterKubernetesControlPlaneEndpoint(
	req provision.NetworkRequest,
	controlPlanePort int,
) string {
	args := m.Called(req, controlPlanePort)
	return args.String(0)
}

func (m *MockProvisioner) GetExternalKubernetesControlPlaneEndpoint(
	req provision.NetworkRequest,
	controlPlanePort int,
) string {
	args := m.Called(req, controlPlanePort)
	return args.String(0)
}

func (m *MockProvisioner) GetTalosAPIEndpoints(req provision.NetworkRequest) []string {
	args := m.Called(req)
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).([]string)
}

func (m *MockProvisioner) GetFirstInterface() v1alpha1.IfaceSelector {
	args := m.Called()
	return args.Get(0).(v1alpha1.IfaceSelector)
}

func (m *MockProvisioner) GetFirstInterfaceName() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockProvisioner) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockProvisioner) UserDiskName(index int) string {
	args := m.Called(index)
	return args.String(0)
}
