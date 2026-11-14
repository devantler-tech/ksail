package talosprovisioner_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
)

// Update prepares the running cluster for a distribution upgrade only when that
// upgrade reboots its nodes in place (ksail#8281). The answer must follow the
// same routing as UpgradeDistribution.
func TestUpgradesDistributionInPlaceFollowsUpgradeRouting(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		provisioner *talosprovisioner.Provisioner
		want        bool
	}{
		{
			name:        "Docker recreates the cluster",
			provisioner: talosprovisioner.NewProvisioner(nil, nil),
		},
		{
			name: "Hetzner rolls its nodes in place",
			provisioner: talosprovisioner.NewProvisioner(nil, nil).
				WithHetznerOptions(v1alpha1.OptionsHetzner{}),
			want: true,
		},
		{
			name: "Omni manages its own upgrades",
			provisioner: talosprovisioner.NewProvisioner(nil, nil).
				WithOmniOptions(v1alpha1.OptionsOmni{}),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, testCase.provisioner.UpgradesDistributionInPlace())
		})
	}
}
