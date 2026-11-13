package cluster_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errCalicoRolloutStalled = errors.New("calico-node rollout stalled")
	errCalicoProbeFailed    = errors.New("calico release unreadable")
)

// cniUpgradeEvents records the order in which the CNI and the distribution
// upgrade touch the cluster.
type cniUpgradeEvents struct {
	events []string
}

// recordingCalicoInstaller reports a fixed values-drift verdict and records the
// probe, the install and the rollout wait.
type recordingCalicoInstaller struct {
	log        *cniUpgradeEvents
	drifted    bool
	probeErr   error
	rolloutErr error
}

func (r *recordingCalicoInstaller) Install(context.Context) error {
	r.log.events = append(r.log.events, "calico install")

	return nil
}

func (*recordingCalicoInstaller) Uninstall(context.Context) error { return nil }

func (*recordingCalicoInstaller) Images(context.Context) ([]string, error) { return nil, nil }

func (r *recordingCalicoInstaller) ValuesDrifted(context.Context) (bool, error) {
	r.log.events = append(r.log.events, "calico probe")

	return r.drifted, r.probeErr
}

func (r *recordingCalicoInstaller) WaitForRollout(context.Context) error {
	r.log.events = append(r.log.events, "calico rollout")

	return r.rolloutErr
}

// inPlaceDistributionUpgrader is a Talos-like upgrader that records each
// distribution step and reports whether it upgrades nodes in place.
type inPlaceDistributionUpgrader struct {
	versionUpgraderFake

	log     *cniUpgradeEvents
	inPlace bool
}

func (u *inPlaceDistributionUpgrader) UpgradeDistribution(
	_ context.Context,
	_, _, to string,
) error {
	u.log.events = append(u.log.events, "distribution "+to)

	return nil
}

func (u *inPlaceDistributionUpgrader) UpgradesDistributionInPlace() bool { return u.inPlace }

var _ clusterupdate.InPlaceDistributionUpgrader = (*inPlaceDistributionUpgrader)(nil)

func newCalicoHetznerTalosConfig() *v1alpha1.Cluster {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.Provider = v1alpha1.ProviderHetzner
	cfg.Spec.Cluster.CNI = v1alpha1.CNICalico

	return cfg
}

func newCNIUpgradeCmd() (*cobra.Command, *bytes.Buffer) {
	var output bytes.Buffer

	cmd := &cobra.Command{Use: "update"}
	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	return cmd, &output
}

type cniBeforeUpgradeCase struct {
	name       string
	inPlace    bool
	drifted    bool
	dryRun     bool
	probeErr   error
	rolloutErr error
	wantEvents []string
	wantErrIs  error
	wantErr    string
}

func cniBeforeUpgradeCases() []cniBeforeUpgradeCase {
	return []cniBeforeUpgradeCase{
		{
			name:    "drifted Calico rolls out before the first upgrade step",
			inPlace: true, drifted: true,
			wantEvents: []string{
				"calico probe", "calico install", "calico rollout", "distribution v1.14.0",
			},
		},
		{
			name:       "Calico without drift is only compared",
			inPlace:    true,
			wantEvents: []string{"calico probe", "distribution v1.14.0"},
		},
		{
			name:    "a dry run never touches Calico",
			inPlace: true, drifted: true, dryRun: true,
		},
		{
			name:       "an upgrade that does not roll nodes in place skips the CNI step",
			drifted:    true,
			wantEvents: []string{"distribution v1.14.0"},
		},
		{
			name:    "a stalled rollout stops the update before any node is upgraded",
			inPlace: true, drifted: true, rolloutErr: errCalicoRolloutStalled,
			wantEvents: []string{"calico probe", "calico install", "calico rollout"},
			wantErrIs:  errCalicoRolloutStalled,
			wantErr:    "no node was upgraded",
		},
		{
			name:    "Calico that cannot be compared stops the update before any node is upgraded",
			inPlace: true, probeErr: errCalicoProbeFailed,
			wantEvents: []string{"calico probe"},
			wantErr:    "could not be compared with this KSail version",
		},
	}
}

func runPinnedCNIUpgrade(t *testing.T, testCase cniBeforeUpgradeCase) ([]string, string, error) {
	t.Helper()

	log := &cniUpgradeEvents{}
	calico := &recordingCalicoInstaller{
		log:        log,
		drifted:    testCase.drifted,
		probeErr:   testCase.probeErr,
		rolloutErr: testCase.rolloutErr,
	}

	t.Cleanup(cluster.SetCalicoInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return calico, nil },
	))

	upgrader := &inPlaceDistributionUpgrader{
		current: clusterupdate.VersionInfo{
			KubernetesVersion: "v1.35.0", DistributionVersion: "v1.13.12",
		},
		distributionPin: "v1.14.0",
		log:             log,
		inPlace:         testCase.inPlace,
	}

	cmd, output := newCNIUpgradeCmd()

	recreated, err := cluster.ExportReconcileClusterVersions(
		cmd, newCalicoHetznerTalosConfig(), upgrader, testCase.dryRun,
	)
	assert.False(t, recreated)

	return log.events, output.String(), err
}

// ksail#8281: Talos v1.14 removed the FlexVolume host path, so a node upgraded
// from v1.13 with the old calico-node spec never reports Ready. Update must
// reconcile drifted Calico, and wait for its rollout, before the first step.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcileClusterVersionsUpdatesCNIBeforeDistributionUpgrade(t *testing.T) {
	for _, testCase := range cniBeforeUpgradeCases() {
		t.Run(testCase.name, func(t *testing.T) {
			events, output, err := runPinnedCNIUpgrade(t, testCase)

			assert.Equal(t, testCase.wantEvents, events)

			if testCase.wantErr != "" {
				require.ErrorContains(t, err, testCase.wantErr)

				if testCase.wantErrIs != nil {
					require.ErrorIs(t, err, testCase.wantErrIs)
				}

				return
			}

			require.NoError(t, err)

			if testCase.inPlace && testCase.drifted && !testCase.dryRun {
				assert.Contains(t, output, "CNI updated before the distribution upgrade")
			}
		})
	}
}

// A discovered upgrade path reconciles the CNI once, before its first step,
// rather than before every step.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestDiscoveredDistributionUpgradeUpdatesCNIOnceBeforeFirstStep(t *testing.T) {
	log := &cniUpgradeEvents{}
	calico := &recordingCalicoInstaller{log: log, drifted: true}

	t.Cleanup(cluster.SetCalicoInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return calico, nil },
	))

	upgrader := &inPlaceDistributionUpgrader{log: log, inPlace: true}
	cmd, _ := newCNIUpgradeCmd()

	recreated, err := cluster.ExportExecuteDistributionUpgrade(
		cmd, newCalicoHetznerTalosConfig(), upgrader, upgradeRegistry{}, "v1.35.0",
	)

	require.NoError(t, err)
	assert.False(t, recreated)
	assert.Equal(t, []string{
		"calico probe", "calico install", "calico rollout",
		"distribution v1.36.0", "distribution v1.37.0",
	}, log.events)
}

// unreadableCiliumInstaller is a Cilium installer whose release cannot be read.
type unreadableCiliumInstaller struct {
	installs int
}

func (u *unreadableCiliumInstaller) Install(context.Context) error {
	u.installs++

	return nil
}

func (*unreadableCiliumInstaller) Uninstall(context.Context) error { return nil }

func (*unreadableCiliumInstaller) Images(context.Context) ([]string, error) { return nil, nil }

func (*unreadableCiliumInstaller) ValuesDrifted(context.Context) (bool, error) {
	return false, errCalicoProbeFailed
}

// A Cilium release that cannot be compared stops the update before the first
// node reboots, as an unreadable Calico release does.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestUnreadableCiliumStopsInPlaceDistributionUpgrade(t *testing.T) {
	cilium := &unreadableCiliumInstaller{}

	t.Cleanup(cluster.SetCiliumInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return cilium, nil },
	))

	log := &cniUpgradeEvents{}
	upgrader := &inPlaceDistributionUpgrader{log: log, inPlace: true}
	upgrader.current = clusterupdate.VersionInfo{
		KubernetesVersion: "v1.35.0", DistributionVersion: "v1.13.12",
	}
	upgrader.distributionPin = "v1.14.0"

	cfg := newCalicoHetznerTalosConfig()
	cfg.Spec.Cluster.CNI = v1alpha1.CNICilium

	cmd, _ := newCNIUpgradeCmd()

	_, err := cluster.ExportReconcileClusterVersions(cmd, cfg, upgrader, false)

	require.ErrorContains(t, err, "could not be compared with this KSail version")
	assert.Empty(t, log.events, "no node may be upgraded")
	assert.Zero(t, cilium.installs)
}
