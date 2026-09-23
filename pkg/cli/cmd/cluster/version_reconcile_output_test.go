package cluster_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// versionUpgraderFake is a provisioner that reconciles versions in place, so the
// update orchestrator's version-reconciliation output can be observed without a
// real cluster.
type versionUpgraderFake struct {
	fakeProvisioner

	current         clusterupdate.VersionInfo
	distributionPin string
	kubernetesImage string
	upgradeErr      error
	upgrades        []string
}

func (f *versionUpgraderFake) UpgradeKubernetes(_ context.Context, _, _, to string) error {
	f.upgrades = append(f.upgrades, "Kubernetes "+to)

	return f.upgradeErr
}

func (f *versionUpgraderFake) UpgradeDistribution(_ context.Context, _, _, to string) error {
	f.upgrades = append(f.upgrades, "distribution "+to)

	return f.upgradeErr
}

func (f *versionUpgraderFake) GetCurrentVersions(
	context.Context, string,
) (*clusterupdate.VersionInfo, error) {
	current := f.current

	return &current, nil
}

func (f *versionUpgraderFake) KubernetesImageRef() string { return f.kubernetesImage }

func (*versionUpgraderFake) DistributionImageRef() string { return "" }

func (f *versionUpgraderFake) PinnedDistributionVersion() string { return f.distributionPin }

func (*versionUpgraderFake) PinnedKubernetesVersion() string { return "" }

func (*versionUpgraderFake) VersionSuffix() string { return "" }

func (*versionUpgraderFake) PrepareConfigForVersion(string, string) error { return nil }

type pinnedUpgradeOutputCase struct {
	name            string
	distributionPin string
	kubernetesPin   string
	upgradeErr      error
	dryRun          bool
	wantMessage     string
	wantUpgrades    []string
}

func pinnedUpgradeOutputCases() []pinnedUpgradeOutputCase {
	return []pinnedUpgradeOutputCase{
		{
			name:            "distribution_upgrade",
			distributionPin: "v1.13.0",
			wantMessage:     "distribution upgraded to pinned version v1.13.0",
			wantUpgrades:    []string{"distribution v1.13.0"},
		},
		{
			name:          "kubernetes_upgrade",
			kubernetesPin: "1.35.0",
			wantMessage:   "Kubernetes upgraded to pinned version v1.35.0",
			wantUpgrades:  []string{"Kubernetes v1.35.0"},
		},
		{
			name:            "distribution_dry_run",
			distributionPin: "v1.13.0",
			dryRun:          true,
			wantMessage:     "Would upgrade distribution to pinned version v1.13.0",
		},
		{
			name:          "kubernetes_dry_run",
			kubernetesPin: "v1.35.0",
			dryRun:        true,
			wantMessage:   "Would upgrade Kubernetes to pinned version v1.35.0",
		},
		{
			name:            "distribution_downgrade_guard",
			distributionPin: "v1.11.0",
			wantMessage:     "newer than pinned version v1.11.0; skipping downgrade",
		},
		{
			name:            "distribution_upgrade_skipped",
			distributionPin: "v1.13.0",
			upgradeErr:      clustererr.ErrUpgradeSkipped,
			wantMessage:     "distribution upgrade skipped",
			wantUpgrades:    []string{"distribution v1.13.0"},
		},
	}
}

// runPinnedUpgrade reconciles versions for testCase with the given --output
// format and returns what the run wrote to stdout and stderr. In JSON mode it
// also renders the change summary afterwards, exactly as the update command does
// once version reconciliation has finished.
func runPinnedUpgrade(
	t *testing.T,
	testCase pinnedUpgradeOutputCase,
	format string,
) (string, string) {
	t.Helper()

	provisioner := &versionUpgraderFake{
		current: clusterupdate.VersionInfo{
			KubernetesVersion:   "v1.34.0",
			DistributionVersion: "v1.12.0",
		},
		distributionPin: testCase.distributionPin,
		upgradeErr:      testCase.upgradeErr,
	}

	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.KubernetesVersion = testCase.kubernetesPin

	cmd := &cobra.Command{Use: "update"}
	cmd.SetContext(t.Context())
	cmd.Flags().String("output", format, "")

	var stdout, stderr bytes.Buffer

	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	recreated, err := cluster.ExportReconcileClusterVersions(cmd, cfg, provisioner, testCase.dryRun)
	require.NoError(t, err)
	assert.False(t, recreated)
	assert.Equal(t, testCase.wantUpgrades, provisioner.upgrades)

	if format == cluster.ExportOutputFormatJSON {
		diff := clusterupdate.NewEmptyUpdateResult()
		diff.InPlaceChanges = []clusterupdate.Change{
			{Field: "cluster.workers", OldValue: "1", NewValue: "2"},
		}

		cluster.ExportDisplayChangesSummary(cmd, diff)
	}

	return stdout.String(), stderr.String()
}

// With --output json stdout carries the machine-readable change summary, so the
// pinned-version reconciliation that runs before it must keep its human-readable
// progress and confirmation text off that stream.
func TestPinnedUpgradeKeepsJSONOutputValid(t *testing.T) {
	t.Parallel()

	for _, testCase := range pinnedUpgradeOutputCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := runPinnedUpgrade(t, testCase, cluster.ExportOutputFormatJSON)

			var decoded map[string]any

			require.NoError(t, json.Unmarshal([]byte(stdout), &decoded),
				"stdout must be a single JSON document: %q", stdout)
			assert.Contains(t, stderr, testCase.wantMessage,
				"the human-readable outcome must still reach the user")
		})
	}
}

// The counterpart: text mode keeps reporting the outcome on stdout.
func TestPinnedUpgradeReportedInTextMode(t *testing.T) {
	t.Parallel()

	for _, testCase := range pinnedUpgradeOutputCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr := runPinnedUpgrade(t, testCase, cluster.ExportOutputFormatText)

			assert.Contains(t, stdout, testCase.wantMessage)
			assert.Empty(t, stderr)
		})
	}
}

// Version discovery (no pin set) runs on every update of a distribution that
// follows the latest release, so its progress text must also stay off stdout in
// JSON mode. A cancelled context stops discovery before any registry is
// contacted, after the progress line has been written.
func TestVersionDiscoveryProgressFollowsOutputFormat(t *testing.T) {
	t.Parallel()

	for _, format := range []string{cluster.ExportOutputFormatJSON, cluster.ExportOutputFormatText} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()

			provisioner := &versionUpgraderFake{
				current:         clusterupdate.VersionInfo{KubernetesVersion: "v1.34.0"},
				kubernetesImage: "registry.invalid/ksail/node",
			}

			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			cmd := &cobra.Command{Use: "update"}
			cmd.SetContext(ctx)
			cmd.Flags().String("output", format, "")

			var stdout, stderr bytes.Buffer

			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			_, err := cluster.ExportReconcileClusterVersions(
				cmd, &v1alpha1.Cluster{}, provisioner, false,
			)
			require.Error(t, err)
			assert.Empty(t, provisioner.upgrades)

			progress := "discovering available Kubernetes versions from registry.invalid/ksail/node"

			if format == cluster.ExportOutputFormatJSON {
				assert.Empty(t, stdout.String())
				assert.Contains(t, stderr.String(), progress)
			} else {
				assert.Contains(t, stdout.String(), progress)
				assert.Empty(t, stderr.String())
			}
		})
	}
}
