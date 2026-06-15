package cluster_test

import (
	"bytes"
	"context"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	cluster "github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	k3dtypes "github.com/k3d-io/k3d/v5/pkg/config/types"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// newFullyPopulatedFactoryContext builds a localregistry.Context with every
// distribution config field set, mirroring what config loading provides at
// runtime. Used to prove the factory construction covers all distributions.
func newFullyPopulatedFactoryContext() *localregistry.Context {
	const name = "factory-coverage"

	return &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{},
		KindConfig: &kindv1alpha4.Cluster{Name: name},
		K3dConfig: &k3dv1alpha5.SimpleConfig{
			ObjectMeta: k3dtypes.ObjectMeta{Name: name},
		},
		TalosConfig:    &talosconfigmanager.Configs{Name: name},
		VClusterConfig: &clusterprovisioner.VClusterConfig{Name: name},
		KWOKConfig:     &clusterprovisioner.KWOKConfig{Name: name},
		EKSConfig: &clusterprovisioner.EKSConfig{
			Name:       name,
			Region:     "us-east-1",
			ConfigPath: "/tmp/eksctl.yaml",
		},
	}
}

// TestDefaultProvisionerFactory_CoversAllDistributions is a regression test for
// the field-drift bug where update.go hand-built a DefaultFactory that omitted
// the EKS config, so `ksail cluster update` on an EKS cluster could not
// construct a provisioner. defaultProvisionerFactory is now the single
// construction point (used by create, update, diff, and recreate via
// newProvisionerFactory); this test asserts it can construct a provisioner for
// every Distribution enum value, so adding a distribution without wiring its
// config here fails loudly.
func TestDefaultProvisionerFactory_CoversAllDistributions(t *testing.T) {
	t.Parallel()

	ctx := newFullyPopulatedFactoryContext()
	factory := cluster.ExportDefaultProvisionerFactory(ctx)

	distribution := v1alpha1.DistributionVanilla
	for _, value := range distribution.ValidValues() {
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			clusterCfg := &v1alpha1.Cluster{}
			clusterCfg.Spec.Cluster.Distribution = v1alpha1.Distribution(value)

			provisioner, _, err := factory.Create(context.Background(), clusterCfg)
			require.NotErrorIs(t, err, clusterprovisioner.ErrMissingDistributionConfig,
				"defaultProvisionerFactory must populate the %s distribution config", value)
			require.NoError(t, err)
			require.NotNil(t, provisioner)
		})
	}
}

// TestCreateAndVerifyProvisioner_HonorsFactoryOverride asserts that the update
// command's provisioner construction goes through newProvisionerFactory and
// therefore honors the test override — previously it hand-built a raw
// DefaultFactory that bypassed the override (and dropped the EKS config).
//
//nolint:paralleltest // mutates the global provisioner-factory override
func TestCreateAndVerifyProvisioner_HonorsFactoryOverride(t *testing.T) {
	restore := cluster.SetProvisionerFactoryForTests(fakeFactory{})
	defer restore()

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	ctx := newFullyPopulatedFactoryContext()

	provisioner, err := cluster.ExportCreateAndVerifyProvisioner(cmd, ctx, "factory-coverage")
	require.NoError(t, err)

	_, isFake := provisioner.(*fakeProvisioner)
	require.True(t, isFake,
		"createAndVerifyProvisioner must build its provisioner via newProvisionerFactory "+
			"(override-aware), got %T", provisioner)
}
