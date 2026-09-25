package cluster_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type imageUpgraderFake struct {
	versionUpgraderFake

	changed bool
	readErr error
	reads   int
}

func (f *imageUpgraderFake) DistributionImageChanged(context.Context, string) (bool, error) {
	f.reads++

	return f.changed, f.readErr
}

type distributionImageCase struct {
	name        string
	pin         string
	changed     bool
	dryRun      bool
	readErr     error
	upgradeErr  error
	wantRead    bool
	wantUpgrade bool
}

func distributionImageCases() []distributionImageCase {
	return []distributionImageCase{
		{
			name:        "changed image same version",
			pin:         "v1.13.10",
			changed:     true,
			wantRead:    true,
			wantUpgrade: true,
		},
		{name: "matching image", pin: "v1.13.10", wantRead: true},
		{name: "dry run", pin: "v1.13.10", changed: true, dryRun: true, wantRead: true},
		{
			name:        "missing v prefix",
			pin:         "1.13.10",
			changed:     true,
			wantRead:    true,
			wantUpgrade: true,
		},
		{
			name:     "unknown image fails closed",
			pin:      "v1.13.10",
			readErr:  io.ErrUnexpectedEOF,
			wantRead: true,
		},
		{
			name: "failed rollout is not success", pin: "v1.13.10", changed: true,
			upgradeErr: io.ErrClosedPipe, wantRead: true, wantUpgrade: true,
		},
		{name: "downgrade remains skipped", pin: "v1.13.9", changed: true},
	}
}

func TestReconcileDistributionImage(t *testing.T) {
	t.Parallel()

	for _, testCase := range distributionImageCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			prov := &imageUpgraderFake{
				versionUpgraderFake: versionUpgraderFake{
					current: clusterupdate.VersionInfo{
						DistributionVersion: "v1.13.10",
						KubernetesVersion:   "v1.35.0",
					},
					distributionPin: testCase.pin,
					upgradeErr:      testCase.upgradeErr,
				},
				changed: testCase.changed, readErr: testCase.readErr,
			}
			cmd := &cobra.Command{}
			cmd.SetContext(t.Context())

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.Flags().String("output", "json", "")

			_, err := cluster.ExportReconcileClusterVersions(
				cmd,
				&v1alpha1.Cluster{},
				prov,
				testCase.dryRun,
			)
			switch {
			case testCase.readErr != nil:
				require.ErrorIs(t, err, testCase.readErr)
			case testCase.upgradeErr != nil:
				require.ErrorIs(t, err, testCase.upgradeErr)
				assert.NotContains(t, stderr.String(), "Distribution image reconciled")
			default:
				require.NoError(t, err)
			}

			assert.Equal(t, testCase.wantRead, prov.reads > 0)
			assert.Empty(t, stdout.String(), "progress must not corrupt JSON output")

			if testCase.wantUpgrade {
				assert.Equal(t, []string{"distribution v1.13.10"}, prov.upgrades)
			} else {
				assert.Empty(t, prov.upgrades)
			}

			if testCase.dryRun {
				assert.Contains(t, stderr.String(), "Would reconcile distribution image")
			}
		})
	}
}

func TestDistributionImageDrift(t *testing.T) {
	t.Parallel()

	prov := &imageUpgraderFake{
		versionUpgraderFake: versionUpgraderFake{distributionPin: "v1.13.10"},
		changed:             true,
	}
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	result := cluster.ExportPlannedVersionDrift(cmd, &v1alpha1.Cluster{}, prov,
		&clusterupdate.VersionInfo{DistributionVersion: "v1.13.10"})
	require.Len(t, result.RebootRequired, 1)
	assert.Equal(t, "distribution.image", result.RebootRequired[0].Field)
	assert.Empty(t, prov.upgrades, "diff must not install an image")
}

func TestDistributionImageDriftUnavailable(t *testing.T) {
	t.Parallel()

	prov := &imageUpgraderFake{
		versionUpgraderFake: versionUpgraderFake{distributionPin: "v1.13.10"},
		readErr:             io.ErrUnexpectedEOF,
	}
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	cmd.SetErr(io.Discard)
	result := cluster.ExportPlannedVersionDrift(cmd, &v1alpha1.Cluster{}, prov,
		&clusterupdate.VersionInfo{DistributionVersion: "v1.13.10"})
	require.True(t, result.HasUnknownBaseline(), "unreadable image must not look clean")
	assert.Empty(t, result.RebootRequired)
	assert.Empty(t, prov.upgrades)
}
