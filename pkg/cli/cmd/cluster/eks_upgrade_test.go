package cluster_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const eksUpgradeStartingVersion = "1.34"

type eksUpgradeCase struct {
	name, target                                     string
	enabled, dryRun, wantMutation, wantErr, fromFlag bool
}

//nolint:paralleltest // Test helpers change process credentials and endpoints with t.Setenv.
func TestEKSExplicitUpgradeOrchestration(t *testing.T) {
	// Not parallel: exercises freezing then changing process credentials and endpoints.
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "credentials"))

	for _, testCase := range []eksUpgradeCase{
		{name: "disabled", target: "1.35"},
		{name: "yaml_minor", target: "1.35", enabled: true, wantMutation: true},
		{name: "flag_overrides_yaml", target: "1.35", enabled: true, wantMutation: true, fromFlag: true},
		{name: "zero_patch", target: "v1.35.0", enabled: true, wantMutation: true},
		{name: "dry_run", target: "1.35", enabled: true, dryRun: true},
		{name: "same_minor", target: eksUpgradeStartingVersion, enabled: true},
		{name: "no_target", enabled: true},
		{name: "dry_run_jump", target: "1.36", enabled: true, dryRun: true, wantErr: true},
		{name: "dry_run_downgrade", target: "1.33", enabled: true, dryRun: true, wantErr: true},
		{name: "dry_run_patch", target: "1.35.1", enabled: true, dryRun: true, wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) { exerciseEKSUpgrade(t, testCase) })
	}
}

func exerciseEKSUpgrade(t *testing.T, testCase eksUpgradeCase) {
	t.Helper()

	mutations := 0
	factory := eksUpgradeFactory(t, &mutations)
	cmd, cfg := loadEKSUpgradeConfig(t, testCase)
	provisioner, _, err := factory.Create(t.Context(), cfg)
	require.NoError(t, err)

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	if upgrader, ok := provisioner.(clusterupdate.Upgrader); ok {
		current, readErr := upgrader.GetCurrentVersions(t.Context(), "demo")
		require.NoError(t, readErr)

		drift := cluster.ExportPlannedVersionDrift(cmd, cfg, upgrader, current)
		assert.Empty(t, drift.RecreateRequired)
		assert.Equal(
			t,
			!testCase.wantErr && testCase.target != "" &&
				testCase.target != eksUpgradeStartingVersion,
			drift.HasInPlaceChanges(),
		)
	}

	recreated, err := cluster.ExportReconcileClusterVersions(cmd, cfg, provisioner, testCase.dryRun)
	if testCase.wantErr {
		require.Error(t, err)
	} else {
		require.NoError(t, err)
	}

	assert.False(t, recreated)
	assert.Equal(t, testCase.wantMutation, mutations == 1)

	if testCase.dryRun && !testCase.wantErr {
		assert.Contains(t, output.String(), "Would upgrade")
	}

	if testCase.wantMutation {
		assert.Contains(t, output.String(), "upgraded to pinned version v1.35.0")
	}
}

func loadEKSUpgradeConfig(
	t *testing.T,
	testCase eksUpgradeCase,
) (*cobra.Command, *v1alpha1.Cluster) {
	t.Helper()

	cmd := &cobra.Command{Use: "update"}
	cmd.SetContext(t.Context())
	manager := cluster.ExportSetupMutationCmdFlags(cmd)

	target := testCase.target
	if testCase.fromFlag {
		target = eksUpgradeStartingVersion
	}

	manager.Viper.SetConfigType("yaml")
	require.NoError(t, manager.Viper.ReadConfig(strings.NewReader(fmt.Sprintf(
		"spec:\n  cluster:\n    distribution: EKS\n    kubernetesVersion: %q\n"+
			"    eks:\n      experimentalControlPlaneUpgrade: %t\n",
		target,
		testCase.enabled,
	))))

	if testCase.fromFlag {
		require.NoError(t, cmd.ParseFlags([]string{"--kubernetes-version", testCase.target}))
	}

	cfg, err := manager.Load(configmanager.LoadOptions{
		Silent: true, IgnoreConfigFile: true, SkipValidation: true, SkipDistributionConfig: true,
	})
	require.NoError(t, err)
	require.Equal(t, testCase.target, cfg.Spec.Cluster.KubernetesVersion)

	return cmd, cfg
}

func eksUpgradeFactory(t *testing.T, mutations *int) clusterprovisioner.DefaultFactory {
	t.Helper()
	t.Setenv("AWS_ENDPOINT_URL_EKS", eksUpgradeHTTPFixture(t, mutations))
	t.Setenv("AWS_IGNORE_CONFIGURED_ENDPOINT_URLS", "false")
	frozen, err := credentials.FreezeAWS(t.Context(), credentials.AWSResolution{
		AccessKeyID: "FROZENUPGRADE", SecretAccessKey: "secret",
	}, "us-east-1")
	require.NoError(t, err)
	t.Setenv("AWS_ACCESS_KEY_ID", "CHANGEDAMBIENT")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "changed-secret")
	t.Setenv("AWS_ENDPOINT_URL_EKS", "http://127.0.0.1:1")

	return clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			EKS: &clusterprovisioner.EKSConfig{Name: "demo", Region: "us-east-1"},
		},
		AWSResolution: &frozen, AWSOwnershipVerifier: func(context.Context) error { return nil },
	}
}

func eksUpgradeHTTPFixture(t *testing.T, mutations *int) string {
	t.Helper()

	version := eksUpgradeStartingVersion
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			assert.Contains(t, request.Header.Get("Authorization"), "Credential=FROZENUPGRADE/")

			if request.Method == http.MethodPost {
				*mutations++

				var body map[string]any

				request.Body = http.MaxBytesReader(writer, request.Body, 1024)
				assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
				assert.Equal(t, "1.35", body["version"])
				assert.NotEmpty(t, body["clientRequestToken"])
				assert.NotContains(t, body, "force")

				version = "1.35"
			}

			if strings.Contains(request.URL.Path, "/updates") {
				_, _ = writer.Write([]byte(`{"update":{"id":"upgrade-id","type":"VersionUpdate",` +
					`"status":"Successful","params":[{"type":"Version","value":"1.35"}]}}`))

				return
			}

			_, _ = writer.Write(
				[]byte(
					`{"cluster":{"name":"demo","arn":"arn:aws:eks:us-east-1:123456789012:cluster/demo",` +
						`"createdAt":1700000000,"status":"ACTIVE","version":"` + version + `"}}`,
				),
			)
		}),
	)
	t.Cleanup(server.Close)

	return server.URL
}

//nolint:paralleltest // The HTTP fixture changes AWS endpoint environment.
func TestEKSUpdateChecksFullDiffBeforeUpgrade(t *testing.T) {
	mutations := 0
	factory := eksUpgradeFactory(t, &mutations)
	cmd, cfg := loadEKSUpgradeConfig(t, eksUpgradeCase{target: "1.35", enabled: true})
	p, _, err := factory.Create(t.Context(), cfg)
	require.NoError(t, err)

	upgrader, ok := p.(*eksprovisioner.UpgradableProvisioner)
	require.True(t, ok)

	wantErr := assert.AnError
	pipeline := &eksUpdatePipeline{
		UpgradableProvisioner: upgrader,
		current:               cfg.Spec.Cluster,
		diffErr:               wantErr,
	}

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	require.ErrorIs(t, cluster.ExportRunVerifiedUpdate(cmd, cfg, pipeline, false), wantErr)
	assert.Zero(
		t,
		mutations,
		"an unreadable full diff must abort before the control-plane mutation",
	)
}

//nolint:paralleltest // The HTTP fixture changes AWS endpoint environment.
func TestEKSDryRunReportsVersionInFinalDiff(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			mutations := 0
			factory := eksUpgradeFactory(t, &mutations)
			cmd, cfg := loadEKSUpgradeConfig(t, eksUpgradeCase{target: "1.35", enabled: true})
			cmd.Flags().String("output", format, "")

			p, _, err := factory.Create(t.Context(), cfg)
			require.NoError(t, err)

			upgrader, ok := p.(*eksprovisioner.UpgradableProvisioner)
			require.True(t, ok)

			pipeline := &eksUpdatePipeline{
				UpgradableProvisioner: upgrader,
				current:               cfg.Spec.Cluster,
			}

			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&bytes.Buffer{})
			require.NoError(t, cluster.ExportRunVerifiedUpdate(cmd, cfg, pipeline, true))
			assert.Zero(t, mutations)
			assert.NotContains(t, output.String(), "No changes detected")

			if format == "json" {
				var diff cluster.DiffJSONOutput
				require.NoError(t, json.Unmarshal(output.Bytes(), &diff))
				require.Len(t, diff.InPlaceChanges, 1)
				assert.Equal(t, "kubernetes.version", diff.InPlaceChanges[0].Field)
			} else {
				assert.Contains(t, output.String(), "kubernetes.version")
			}
		})
	}
}

type eksUpdatePipeline struct {
	*eksprovisioner.UpgradableProvisioner

	current  v1alpha1.ClusterSpec
	diffErr  error
	recreate bool
	unknown  bool
}

func (p *eksUpdatePipeline) GetCurrentConfig(
	context.Context,
	string,
) (*v1alpha1.ClusterSpec, *v1alpha1.ProviderSpec, error) {
	if p.unknown {
		current := p.current
		clusterupdate.MarkComponentsUnknown(&current)

		return &current, nil, nil
	}

	return &p.current, &v1alpha1.ProviderSpec{}, nil
}

func (p *eksUpdatePipeline) DiffConfig(
	context.Context,
	string,
	*v1alpha1.ClusterSpec,
	*v1alpha1.ClusterSpec,
) (*clusterupdate.UpdateResult, error) {
	result := clusterupdate.NewEmptyUpdateResult()
	if p.recreate {
		result.RecreateRequired = []clusterupdate.Change{
			{
				Field:    "eks.managedNodeGroups[workers].instanceType",
				Category: clusterupdate.ChangeCategoryRecreateRequired,
			},
		}
	}

	return result, p.diffErr
}

//nolint:paralleltest // The HTTP fixture changes AWS endpoint environment.
func TestEKSUpgradeRejectsConcurrentRecreation(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry_run_%t", dryRun), func(t *testing.T) {
			mutations := 0
			factory := eksUpgradeFactory(t, &mutations)
			cmd, cfg := loadEKSUpgradeConfig(t, eksUpgradeCase{target: "1.35", enabled: true})
			p, _, err := factory.Create(t.Context(), cfg)
			require.NoError(t, err)

			upgrader, ok := p.(*eksprovisioner.UpgradableProvisioner)
			require.True(t, ok)

			pipeline := &eksUpdatePipeline{
				UpgradableProvisioner: upgrader,
				current:               cfg.Spec.Cluster,
				recreate:              true,
			}

			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetIn(strings.NewReader("n\n"))
			err = cluster.ExportRunVerifiedUpdate(cmd, cfg, pipeline, dryRun)
			require.ErrorContains(t, err, "separate")
			assert.Zero(t, mutations)
		})
	}
}

//nolint:paralleltest // The HTTP fixture changes AWS endpoint environment.
func TestEKSUpgradeReportsSuccessWithUnknownComponentBaseline(t *testing.T) {
	mutations := 0
	factory := eksUpgradeFactory(t, &mutations)
	cmd, cfg := loadEKSUpgradeConfig(t, eksUpgradeCase{target: "1.35", enabled: true})
	cfg.Spec.Cluster.CNI = v1alpha1.CNICilium
	p, _, err := factory.Create(t.Context(), cfg)
	require.NoError(t, err)

	upgrader, ok := p.(*eksprovisioner.UpgradableProvisioner)
	require.True(t, ok)

	pipeline := &eksUpdatePipeline{
		UpgradableProvisioner: upgrader,
		current:               cfg.Spec.Cluster,
		unknown:               true,
	}

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	require.NoError(t, cluster.ExportRunVerifiedUpdate(cmd, cfg, pipeline, false))
	assert.Equal(t, 1, mutations)
	assert.Contains(t, output.String(), "upgraded to pinned version")
	assert.Contains(t, output.String(), "Unknown")
	assert.NotContains(t, output.String(), "No changes applied")
	assert.NotContains(t, output.String(), "No changes detected")
}

// The recreation guard exists to stop a recreation discarding the cluster an
// upgrade is being applied to. Once the control plane already sits at the
// pinned version there is no upgrade to conflict with, so a recreation must be
// admissible without first removing the pin or disabling the feature — the
// documented two-step workflow depends on it.
//
//nolint:paralleltest // The HTTP fixture changes AWS endpoint environment.
func TestEKSRecreationAllowedWhenPinnedVersionIsReached(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry_run_%t", dryRun), func(t *testing.T) {
			mutations := 0
			factory := eksUpgradeFactory(t, &mutations)
			cmd, cfg := loadEKSUpgradeConfig(t, eksUpgradeCase{
				target:  eksUpgradeStartingVersion, // already at the pin
				enabled: true,
			})
			p, _, err := factory.Create(t.Context(), cfg)
			require.NoError(t, err)

			upgrader, ok := p.(*eksprovisioner.UpgradableProvisioner)
			require.True(t, ok)

			pipeline := &eksUpdatePipeline{
				UpgradableProvisioner: upgrader,
				current:               cfg.Spec.Cluster,
				recreate:              true,
			}

			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetIn(strings.NewReader("n\n"))
			err = cluster.ExportRunVerifiedUpdate(cmd, cfg, pipeline, dryRun)
			require.NotErrorIs(t, err, cluster.ErrEKSUpgradeWithRecreation,
				"a satisfied pin must not block a recreation-required change")
			assert.Zero(t, mutations, "no control-plane upgrade is planned at the pinned version")
		})
	}
}

// displayChangesSummary writes the machine-readable document to stdout in JSON
// mode. The upgrade confirmation shares that stream, so emitting it there would
// leave stdout unparseable for the documented CI/MCP output mode.
func TestEKSUpgradeNotificationKeepsJSONOutputValid(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = []clusterupdate.Change{
		{Field: "kubernetes.version", OldValue: "1.34", NewValue: "1.35"},
	}

	cmd := &cobra.Command{Use: "update"}
	cmd.Flags().String("output", cluster.ExportOutputFormatJSON, "")

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})

	cluster.ExportDisplayChangesSummary(cmd, diff)
	cluster.ExportReportEKSUpgraded(cmd, "1.35")

	var decoded map[string]any

	require.NoError(t, json.Unmarshal(out.Bytes(), &decoded),
		"stdout must stay valid JSON after the upgrade confirmation: %q", out.String())
}

// The counterpart: text mode still confirms the upgrade to the user.
func TestEKSUpgradeNotificationReportedInTextMode(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "update"}
	cmd.Flags().String("output", cluster.ExportOutputFormatText, "")

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})

	cluster.ExportReportEKSUpgraded(cmd, "1.35")

	assert.Contains(t, out.String(), "upgraded to pinned version 1.35")
}
