package registrystage

import (
	"context"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// Stage message constants for registry operations.
const (
	MirrorStageTitle    = "Create mirror registry..."
	MirrorStageEmoji    = "🪞"
	MirrorStageActivity = "creating mirror registries"
	MirrorStageSuccess  = "mirror registries created"
	MirrorStageFailure  = "failed to setup registries"

	ConnectStageTitle    = "Connect registry..."
	ConnectStageEmoji    = "🔗"
	ConnectStageActivity = "connecting registries"
	ConnectStageSuccess  = "registries connected"
	ConnectStageFailure  = "failed to connect registries"
)

// Role represents the type of registry stage operation.
type Role int

const (
	// RoleMirror is the stage that creates mirror registries before cluster creation.
	RoleMirror Role = iota
	// RoleConnect is the stage that connects registries after cluster creation.
	RoleConnect
)

// Info contains display information for a registry stage.
type Info struct {
	Title         string
	Emoji         string
	Activity      string
	Success       string
	FailurePrefix string
}

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
	Info        Info
	KindAction  func(*Context) func(context.Context, client.APIClient) error
	K3dAction   func(*Context) func(context.Context, client.APIClient) error
	TalosAction func(*Context) func(context.Context, client.APIClient) error
}

// MirrorInfo returns the stage info for mirror registry creation.
//
//nolint:gochecknoglobals // Constant configuration for mirror registry stage.
var MirrorInfo = Info{
	Title:         MirrorStageTitle,
	Emoji:         MirrorStageEmoji,
	Activity:      MirrorStageActivity,
	Success:       MirrorStageSuccess,
	FailurePrefix: MirrorStageFailure,
}

// ConnectInfo returns the stage info for registry connection.
//
//nolint:gochecknoglobals // Constant configuration for registry connection stage.
var ConnectInfo = Info{
	Title:         ConnectStageTitle,
	Emoji:         ConnectStageEmoji,
	Activity:      ConnectStageActivity,
	Success:       ConnectStageSuccess,
	FailurePrefix: ConnectStageFailure,
}

