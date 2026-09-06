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
		AccessKeyID: "FROZENUPGRADE", SecretAccessKey: "secret", SessionToken: "session",
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
