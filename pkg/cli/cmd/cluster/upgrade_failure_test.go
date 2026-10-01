package cluster_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v7/pkg/svc/versionresolver"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errPartialUpgrade    = errors.New("upgrade interrupted after changing the cluster")
	errUpgradeVersionAPI = errors.New("version API unavailable")
)

type upgradeRegistry struct{}

func (upgradeRegistry) ListVersions(context.Context, string) ([]versionresolver.Version, error) {
	return versionresolver.ParseTags([]string{"v1.35.0", "v1.36.0", "v1.37.0"}), nil
}

// failingRollingUpgrader changes live state before returning the failed step, as
// a partially completed rolling upgrade does. Only the external provisioner is a double.
type failingRollingUpgrader struct {
	versionUpgraderFake

	failStep       int
	steps          int
	reads          int
	readErr        error
	nilVersions    bool
	observed       *clusterupdate.VersionInfo
	stepErr        error
	cancel         context.CancelFunc
	readDeadline   time.Time
	readContextErr error
}

func (f *failingRollingUpgrader) UpgradeKubernetes(context.Context, string, string, string) error {
	return f.apply()
}

func (f *failingRollingUpgrader) UpgradeDistribution(
	context.Context,
	string,
	string,
	string,
) error {
	return f.apply()
}

func (f *failingRollingUpgrader) GetCurrentVersions(
	ctx context.Context, _ string,
) (*clusterupdate.VersionInfo, error) {
	f.reads++
	f.readDeadline, _ = ctx.Deadline()

	f.readContextErr = ctx.Err()
	if f.readContextErr != nil {
		return nil, f.readContextErr
	}

	if f.readErr != nil {
		return nil, f.readErr
	}

	// Model an incomplete provider response as well as a populated snapshot.
	var current *clusterupdate.VersionInfo
	if !f.nilVersions {
		current = &f.current
	}

	return current, f.readErr
}

func (f *failingRollingUpgrader) apply() error {
	f.steps++
	if f.steps == f.failStep {
		if f.observed != nil {
			f.current = *f.observed
		}
		if f.cancel != nil {
			f.cancel()
		}
		if f.stepErr != nil {
			return f.stepErr
		}
		return errPartialUpgrade
	}
	return nil
}

func TestFailedRollingUpgradeReportsObservedVersions(t *testing.T) {
	t.Parallel()

	for _, dimension := range []string{"Kubernetes", "distribution"} {
		for _, failStep := range []int{1, 3} {
			t.Run(fmt.Sprintf("%s_step_%d", dimension, failStep), func(t *testing.T) {
				t.Parallel()

				upgrader := &failingRollingUpgrader{
					failStep: failStep,
					observed: &clusterupdate.VersionInfo{
						KubernetesVersion: "v1.37.0", DistributionVersion: "v1.13.0",
					},
					versionUpgraderFake: versionUpgraderFake{current: clusterupdate.VersionInfo{
						KubernetesVersion: "v1.34.0", DistributionVersion: "v1.12.0",
					}},
				}
				cmd := &cobra.Command{Use: "update"}
				cmd.SetContext(t.Context())
				cmd.Flags().String("output", cluster.ExportOutputFormatJSON, "")

				var stdout, stderr bytes.Buffer
				cmd.SetOut(&stdout)
				cmd.SetErr(&stderr)

				recreated, err := cluster.ExportExecuteVersionUpgrade(
					cmd, upgrader, upgradeRegistry{}, dimension, "v1.34.0", false,
				)
				require.ErrorIs(t, err, errPartialUpgrade)
				assert.False(t, recreated)
				assert.Equal(t, failStep, upgrader.steps, "no later upgrade may run after failure")
				assert.Equal(t, 1, upgrader.reads)
				assert.Empty(t, stdout.String(), "failure diagnostics must not corrupt JSON output")

				observed := "observed versions: Kubernetes v1.37.0, distribution v1.13.0"
				assert.Contains(t, err.Error(), observed)
				assert.Contains(t, stderr.String(), observed)
				assert.Contains(
					t,
					stderr.String(),
					err.Error(),
					"warning and returned error must agree",
				)
				assert.NotContains(t, err.Error(), "cluster is running v1.36.0")
				assert.NotContains(t, err.Error(), "cluster is still running v1.34.0")
				require.False(t, upgrader.readDeadline.IsZero(), "diagnostics need a bounded read")
				assert.WithinDuration(
					t,
					time.Now().Add(10*time.Second),
					upgrader.readDeadline,
					time.Second,
				)
			})
		}
	}
}

func TestFailedRollingUpgradeReportsUnknownVersions(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name        string
		readErr     error
		nilVersions bool
		current     clusterupdate.VersionInfo
		want        string
	}{
		{name: "unreachable", readErr: errUpgradeVersionAPI, want: "running versions could not be determined"},
		{name: "nil_response", nilVersions: true, want: "running versions could not be determined"},
		{name: "empty_response", want: "running versions could not be determined"},
		{
			name: "partial_response", current: clusterupdate.VersionInfo{KubernetesVersion: "v1.37.0"},
			want: "observed versions: Kubernetes v1.37.0, distribution unknown",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			upgrader := &failingRollingUpgrader{
				failStep: 3, readErr: testCase.readErr, nilVersions: testCase.nilVersions,
				versionUpgraderFake: versionUpgraderFake{current: testCase.current},
			}
			cmd := &cobra.Command{Use: "update"}
			cmd.SetContext(t.Context())

			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)

			_, err := cluster.ExportExecuteVersionUpgrade(
				cmd, upgrader, upgradeRegistry{}, "Kubernetes", "v1.34.0", false,
			)
			require.ErrorIs(t, err, errPartialUpgrade)
			assert.Contains(t, err.Error(), testCase.want)
			assert.Contains(t, output.String(), testCase.want)
			assert.Equal(t, 1, upgrader.reads)
			assert.NotContains(t, err.Error(), "cluster is running")
		})
	}
}

func TestSuccessfulAndSkippedUpgradesDoNotReadFailureDiagnostics(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		dryRun bool
		err    error
	}{
		{name: "success"},
		{name: "dry_run", dryRun: true},
		{name: "skipped", err: clustererr.ErrUpgradeSkipped},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			upgrader := &failingRollingUpgrader{}
			if testCase.err != nil {
				upgrader.failStep = 1
				upgrader.stepErr = testCase.err
			}

			cmd := &cobra.Command{Use: "update"}
			cmd.SetContext(t.Context())

			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			_, err := cluster.ExportExecuteVersionUpgrade(
				cmd, upgrader, upgradeRegistry{}, "Kubernetes", "v1.34.0", testCase.dryRun,
			)
			require.NoError(t, err)
			assert.Zero(t, upgrader.reads)
			assert.NotContains(t, output.String(), "observed versions")
		})
	}
}

func TestFailedRollingUpgradeHonorsCancellationDuringDiagnostics(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	upgrader := &failingRollingUpgrader{failStep: 1, cancel: cancel}
	cmd := &cobra.Command{Use: "update"}
	cmd.SetContext(ctx)

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	_, err := cluster.ExportExecuteVersionUpgrade(
		cmd, upgrader, upgradeRegistry{}, "Kubernetes", "v1.34.0", false,
	)
	require.ErrorIs(t, err, errPartialUpgrade)
	require.ErrorIs(t, upgrader.readContextErr, context.Canceled)
	assert.Contains(t, err.Error(), "running versions could not be determined")
	assert.Contains(t, output.String(), "running versions could not be determined")
}
