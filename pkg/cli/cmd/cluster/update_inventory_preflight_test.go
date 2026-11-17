package cluster_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type inventoryFailureUpgrader struct {
	updatableUpgraderFake

	inventoryErr error
}

var errFixtureNodeInventoryUnknownRole = errors.New("fixture inventory has an unknown role")

func (f *inventoryFailureUpgrader) GetCurrentConfig(context.Context, string) (
	*v1alpha1.ClusterSpec, *v1alpha1.ProviderSpec, error,
) {
	return nil, nil, f.inventoryErr
}

func TestUpdateRejectsUnknownInventoryBeforeVersionMutation(t *testing.T) {
	t.Parallel()

	provisioner := &inventoryFailureUpgrader{
		inventoryErr: errFixtureNodeInventoryUnknownRole,
	}
	provisioner.current = clusterupdate.VersionInfo{DistributionVersion: "v1.12.0"}
	provisioner.distributionPin = "v1.13.0"

	cmd := &cobra.Command{Use: "update"}
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.Flags().String("output", "text", "")

	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos

	err := cluster.ExportRunVerifiedUpdate(cmd, cfg, provisioner, false)
	require.ErrorIs(t, err, errFixtureNodeInventoryUnknownRole)
	assert.Empty(t, provisioner.upgrades, "capacity rejection must precede every version mutation")
}
