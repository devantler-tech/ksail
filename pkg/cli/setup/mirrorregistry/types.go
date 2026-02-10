package mirrorregistry

import (
	"context"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// BackendFactory is an alias for registry.BackendFactory for convenience.
type BackendFactory = registry.BackendFactory

// GetBackendFactory returns the current backend factory from the registry package.
func GetBackendFactory() BackendFactory {
	return registry.GetBackendFactory()
}

// SetBackendFactoryForTests is an alias for registry.SetBackendFactoryForTests.
func SetBackendFactoryForTests(factory BackendFactory) func() {
	return registry.SetBackendFactoryForTests(factory)
}

// Stage message constants for registry operations.
const (
	// RegistryStageTitle is the title for the registry stage that creates and configures registries.
	RegistryStageTitle    = "Create registries..."
	RegistryStageEmoji    = "📦"
	RegistryStageActivity = "creating and configuring registries"
	RegistryStageSuccess  = "registries created"
	RegistryStageFailure  = "failed to create registries"

	// NetworkStageTitle is the title for the network stage that creates Docker network.
	NetworkStageTitle    = "Create network..."
	NetworkStageEmoji    = "🌐"
	NetworkStageActivity = "creating docker network"
	NetworkStageSuccess  = "docker network created"
	NetworkStageFailure  = "failed to create docker network"

	// ConnectStageTitle is the title for the stage that connects registries to Docker network.
	ConnectStageTitle    = "Connect registries..."
	ConnectStageEmoji    = "🔗"
	ConnectStageActivity = "connecting registries to docker network"
	ConnectStageSuccess  = "registries connected to docker network"
	ConnectStageFailure  = "failed to connect registries to docker network"

	// PostClusterConnectStageTitle is the title for the stage that configures containerd inside cluster nodes.
	PostClusterConnectStageTitle    = "Configure registry mirrors..."
	PostClusterConnectStageEmoji    = "⚙️"
	PostClusterConnectStageActivity = "configuring registry mirrors in cluster"
	PostClusterConnectStageSuccess  = "registry mirrors configured"
	PostClusterConnectStageFailure  = "failed to configure registry mirrors"
)

// Role represents the type of registry stage operation.
type Role int

const (
	// RoleRegistry is the stage that creates registries before network creation.
	RoleRegistry Role = iota
	// RoleNetwork is the stage that creates the Docker network.
	RoleNetwork
	// RoleConnect is the stage that connects registries to the Docker network.
	RoleConnect
	// RolePostClusterConnect is the stage that configures containerd inside cluster nodes.
	RolePostClusterConnect
)

// Handler contains the prepare and action functions for a registry stage.
type Handler struct {
	Prepare func() bool
	Action  func(context.Context, client.APIClient) error
}

// Context contains all the configuration needed for registry stage execution.
type Context struct {
	Cmd         *cobra.Command
	ClusterCfg  *v1alpha1.Cluster
	KindConfig  *v1alpha4.Cluster
	K3dConfig   *v1alpha5.SimpleConfig
	TalosConfig *talosconfigmanager.Configs
	MirrorSpecs []registry.MirrorSpec
}

// Definition maps a stage role to its info and distribution-specific actions.
type Definition struct {
	Info        setup.StageInfo
	KindAction  func(*Context) func(context.Context, client.APIClient) error
	K3dAction   func(*Context) func(context.Context, client.APIClient) error
	TalosAction func(*Context) func(context.Context, client.APIClient) error
}

// RegistryInfo returns the stage info for registry creation.
//
//nolint:gochecknoglobals // Constant configuration for registry stage.
var RegistryInfo = setup.StageInfo{
	Title:         RegistryStageTitle,
	Emoji:         RegistryStageEmoji,
	Activity:      RegistryStageActivity,
	Success:       RegistryStageSuccess,
	FailurePrefix: RegistryStageFailure,
}

// NetworkInfo returns the stage info for network creation.
//
//nolint:gochecknoglobals // Constant configuration for network stage.
var NetworkInfo = setup.StageInfo{
	Title:         NetworkStageTitle,
	Emoji:         NetworkStageEmoji,
	Activity:      NetworkStageActivity,
	Success:       NetworkStageSuccess,
	FailurePrefix: NetworkStageFailure,
}

// ConnectInfo returns the stage info for registry connection.
//
//nolint:gochecknoglobals // Constant configuration for registry connection stage.
var ConnectInfo = setup.StageInfo{
	Title:         ConnectStageTitle,
	Emoji:         ConnectStageEmoji,
	Activity:      ConnectStageActivity,
	Success:       ConnectStageSuccess,
	FailurePrefix: ConnectStageFailure,
}

// PostClusterConnectInfo returns the stage info for post-cluster registry configuration.
//
//nolint:gochecknoglobals // Constant configuration for post-cluster registry connection stage.
var PostClusterConnectInfo = setup.StageInfo{
	Title:         PostClusterConnectStageTitle,
	Emoji:         PostClusterConnectStageEmoji,
	Activity:      PostClusterConnectStageActivity,
	Success:       PostClusterConnectStageSuccess,
	FailurePrefix: PostClusterConnectStageFailure,
}
