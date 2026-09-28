package cluster

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An explicit region selects one exact record. A damaged sibling cannot redirect that target,
// while a name-only command must still refuse the incomplete regional listing.
func TestExplicitEKSRegionKeepsValidOwnershipDespiteDamagedSibling(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const (
		name   = "region-qualified-ownership"
		region = "eu-north-1"
	)
	require.NoError(t, state.SaveEKSOwnershipState(name, region, &state.EKSOwnershipState{
		Version:     state.EKSOwnershipStateVersion,
		ClusterName: name,
		Region:      region,
		AccountID:   "123456789012",
		ClusterARN:  "arn:aws:eks:" + region + ":123456789012:cluster/" + name,
		CreatedAt:   time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC),
		AWSOptions:  credentials.AWSOptionsWithDefaults(v1alpha1.OptionsAWS{}),
	}))
	sibling := filepath.Join(home, ".ksail", "clusters", name, "eks-ownership-us-west-2.json")
	require.NoError(t, os.WriteFile(sibling, []byte("{"), 0o600))

	resolved := &lifecycle.ResolvedClusterInfo{
		ClusterName: name,
		Provider:    v1alpha1.ProviderAWS,
		AWSRegion:   region,
	}
	assert.True(t, hasLocalKSailEKSTargetEvidence(resolved),
		"the exact valid region must remain usable")

	resolved.AWSRegion = ""
	assert.False(t, hasLocalKSailEKSTargetEvidence(resolved),
		"name-only target selection must still refuse a damaged sibling")
}
