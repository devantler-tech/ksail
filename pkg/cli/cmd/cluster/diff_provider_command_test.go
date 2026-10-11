package cluster_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/require"
)

type diffDesiredSpecFactory struct {
	updatableUpgraderFactory

	seen *v1alpha1.ClusterSpec
}

func (f *diffDesiredSpecFactory) Create(
	ctx context.Context, cfg *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	spec := cfg.Spec.Cluster
	f.seen = &spec

	return f.updatableUpgraderFactory.Create(ctx, cfg)
}

// The real command must render the same provider/node defaults as update.
// A missing provider renders Calico without Kind; missing controlPlanes makes
// a two-worker cluster render the non-HA chart despite its third node.
//
//nolint:paralleltest // changes project directory and overrides the provisioner factory.
func TestDiffCommandDefaultsMatchUpdateDesiredCluster(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		fields   string
		provider v1alpha1.Provider
		workers  int32
	}{
		{name: "omitted provider", provider: v1alpha1.ProviderDocker},
		{name: "explicit Docker", fields: "    provider: Docker\n", provider: v1alpha1.ProviderDocker},
		{name: "explicit Hetzner", fields: "    provider: Hetzner\n", provider: v1alpha1.ProviderHetzner},
		{
			name: "two workers and omitted control planes", fields: "    workers: 2\n",
			provider: v1alpha1.ProviderDocker, workers: 2,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			setUpDiffProject(t)

			const configPath = "ksail.yaml"

			config, err := os.ReadFile(configPath)
			require.NoError(t, err)

			config = []byte(strings.Replace(string(config), "    distribution: Vanilla\n",
				"    distribution: Vanilla\n"+testCase.fields, 1))
			//nolint:gosec // G703: fixed fixture filename in the isolated t.Chdir directory.
			require.NoError(t, os.WriteFile(configPath, config, 0o600))

			factory := &diffDesiredSpecFactory{updatableUpgraderFactory: updatableUpgraderFactory{
				provisioner: &updatableUpgraderFake{},
			}}
			t.Cleanup(cluster.SetProvisionerFactoryForTests(factory))

			cmd := cluster.NewDiffCmd()
			cmd.SetContext(t.Context())
			cmd.SetArgs([]string{"--output", "json"})

			var stdout, stderr bytes.Buffer

			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			require.NoError(t, cmd.Execute(), "stderr: %s", stderr.String())
			require.NotNil(t, factory.seen)
			require.Equal(t, testCase.provider, factory.seen.Provider)
			require.Equal(t, int32(1), factory.seen.ControlPlanes)
			require.Equal(t, testCase.workers, factory.seen.Workers)

			after, err := os.ReadFile(configPath)
			require.NoError(t, err)
			require.Equal(t, config, after, "diff must leave the desired file unchanged")
		})
	}
}
