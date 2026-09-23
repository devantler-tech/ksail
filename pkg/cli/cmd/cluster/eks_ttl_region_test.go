package cluster_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	eksTTLRegionNorth = "eu-north-1"
	eksTTLRegionEast  = "us-east-1"
)

// staticClusterLister reports a fixed set of cluster names, standing in for EKS discovery.
type staticClusterLister []string

func (l staticClusterLister) ListAllClusters(context.Context) ([]string, error) {
	return l, nil
}

// seedSameNamedEKSTTLs records a one-hour TTL for clusterName in eu-north-1 and a three-hour TTL
// for a same-named cluster in us-east-1.
func seedSameNamedEKSTTLs(t *testing.T, clusterName string) {
	t.Helper()

	require.NoError(t, state.SaveEKSClusterTTL(clusterName, eksTTLRegionNorth, time.Hour))
	require.NoError(t, state.SaveEKSClusterTTL(clusterName, eksTTLRegionEast, 3*time.Hour))

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })
}

// `ksail cluster create --ttl` records an EKS cluster's TTL for its own region (#7039), so it
// never overwrites the TTL of a same-named cluster in another region.
//
//nolint:paralleltest // shares the package-isolated cluster-state HOME
func TestSaveClusterTTLScopesEKSByRegion(t *testing.T) {
	const clusterName = "create-region-ttl"

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	require.NoError(t, state.SaveEKSClusterTTL(clusterName, eksTTLRegionEast, 3*time.Hour))

	require.NoError(t, cluster.ExportSaveClusterTTL(
		clusterName,
		standaloneEKSTTLClusterConfig(),
		&clusterprovisioner.EKSConfig{Name: clusterName, Region: eksTTLRegionNorth},
		time.Hour,
	))

	north, err := state.LoadEKSClusterTTL(clusterName, eksTTLRegionNorth)
	require.NoError(t, err)
	assert.Equal(t, "1h0m0s", north.Duration)

	east, err := state.LoadEKSClusterTTL(clusterName, eksTTLRegionEast)
	require.NoError(t, err)
	assert.Equal(t, "3h0m0s", east.Duration, "another region's TTL must be left alone")
}

// TTL-driven auto-delete cleans up only its own region's TTL (#7039).
//
//nolint:paralleltest // shares the package-isolated cluster-state HOME
func TestTTLAutoDeleteKeepsOtherRegionsTTL(t *testing.T) {
	const clusterName = "auto-delete-region-ttl"

	seedSameNamedEKSTTLs(t, clusterName)

	require.NoError(t, cluster.ExportDeleteTTLClusterState(
		clusterName,
		standaloneEKSTTLClusterConfig(),
		&clusterprovisioner.EKSConfig{Name: clusterName, Region: eksTTLRegionNorth},
	))

	_, err := state.LoadEKSClusterTTL(clusterName, eksTTLRegionNorth)
	require.ErrorIs(t, err, state.ErrTTLNotSet)

	east, err := state.LoadEKSClusterTTL(clusterName, eksTTLRegionEast)
	require.NoError(t, err)
	assert.Equal(t, "3h0m0s", east.Duration)
}

// `ksail cluster list` lists EKS clusters from one region and shows that region's TTL (#7039).
// Not parallel: it sets AWS_REGION.
func TestListShowsTheListedRegionsEKSTTL(t *testing.T) {
	const clusterName = "list-region-ttl"

	seedSameNamedEKSTTLs(t, clusterName)
	t.Setenv("AWS_REGION", eksTTLRegionEast)

	cmd, buf := newListCmdWithJSONOutput(t)

	require.NoError(t, cluster.HandleListRunE(cmd, v1alpha1.ProviderAWS, cluster.ListDeps{
		AWSLister: staticClusterLister{clusterName},
	}))

	var rows []jsonListRow

	require.NoError(t, json.Unmarshal(buf.Bytes(), &rows))
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].TTL)
	// The remaining time is truncated to the minute, so assert only what tells the regions apart:
	// us-east-1's three-hour TTL shows two-hours-and-some, eu-north-1's one-hour TTL minutes only.
	assert.True(t, strings.HasPrefix(*rows[0].TTL, "2h "),
		"the us-east-1 cluster's own TTL must be shown, got %q", *rows[0].TTL)
}

// `ksail cluster info` shows the TTL recorded for the resolved cluster's region (#7039).
//
//nolint:paralleltest // shares the package-isolated cluster-state HOME
func TestInfoShowsTheResolvedRegionsEKSTTL(t *testing.T) {
	const clusterName = "info-region-ttl"

	seedSameNamedEKSTTLs(t, clusterName)

	for region, want := range map[string]string{
		eksTTLRegionNorth: "set to 1h0m0s",
		eksTTLRegionEast:  "set to 3h0m0s",
	} {
		var buf bytes.Buffer

		cluster.ExportDisplayRegionTTLInfo(&buf, clusterName, v1alpha1.ProviderAWS, region)

		assert.Contains(t, buf.String(), want, "region %s", region)
	}
}

// `ksail cluster delete` of an EKS cluster removes only its own region's TTL (#7039). The real
// delete command runs end to end against the eksctl and AWS identity fixtures in ap-southeast-2,
// and the TTL of a same-named cluster in us-east-1 survives it.
//
//nolint:paralleltest // the EKS fixture sets the environment and changes directory
func TestEKSDeleteCommandKeepsOtherRegionsTTL(t *testing.T) {
	const (
		clusterName   = "delete-command-region-ttl"
		fixtureRegion = "ap-southeast-2"
	)

	setupStandaloneEKSLifecycleFixture(t, clusterName)
	require.NoError(t, state.SaveEKSClusterTTL(clusterName, fixtureRegion, time.Hour))
	require.NoError(t, state.SaveEKSClusterTTL(clusterName, eksTTLRegionEast, 3*time.Hour))

	runStandaloneEKSCommand(
		t, cluster.NewDeleteCmd, "--name", clusterName, "--provider", "AWS", "--force",
	)

	_, err := state.LoadEKSClusterTTL(clusterName, fixtureRegion)
	require.ErrorIs(t, err, state.ErrTTLNotSet, "the deleted cluster's TTL must be removed")

	east, err := state.LoadEKSClusterTTL(clusterName, eksTTLRegionEast)
	require.NoError(t, err, "a same-named cluster's TTL in another region must survive the delete")
	assert.Equal(t, "3h0m0s", east.Duration)
}
