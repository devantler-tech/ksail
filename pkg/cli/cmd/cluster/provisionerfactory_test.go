package cluster_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	cluster "github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	k3dtypes "github.com/k3d-io/k3d/v5/pkg/config/types"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
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
		GKEConfig: &clusterprovisioner.GKEConfig{
			Name:     name,
			Project:  "test-project",
			Location: "europe-north1",
		},
		AKSConfig: &clusterprovisioner.AKSConfig{
			Name:           name,
			SubscriptionID: "test-subscription",
			ResourceGroup:  "test-rg",
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
//
//nolint:paralleltest // t.Setenv-injected fake ADC must stay set while the subtests run
func TestDefaultProvisionerFactory_CoversAllDistributions(t *testing.T) {
	// No t.Parallel() (incl. subtests): the GKE path dials the SDK client via
	// Application Default Credentials, so a fake ADC file is injected with
	// t.Setenv, which is incompatible with parallel tests.
	credPath := filepath.Join(t.TempDir(), "adc.json")
	require.NoError(t, os.WriteFile(credPath, []byte(
		`{"type":"authorized_user","client_id":"test","client_secret":"test","refresh_token":"test"}`,
	), 0o600))
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credPath)

	ctx := newFullyPopulatedFactoryContext()
	factory := cluster.ExportDefaultProvisionerFactory(ctx)

	distribution := v1alpha1.DistributionVanilla
	for _, value := range distribution.ValidValues() {
		t.Run(value, func(t *testing.T) {
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

// TestCreateAndVerifyProvisioner_EKSBindsExactContext proves component clients
// never inherit an unrelated ambient kubeconfig context when EKS context is
// implicit in ksail.yaml.
//
//nolint:paralleltest // mutates the global provisioner-factory override
func TestCreateAndVerifyProvisioner_EKSBindsExactContext(t *testing.T) {
	const (
		clusterName   = "factory-coverage"
		region        = "us-east-1"
		ambient       = "ambient-other-cluster"
		targetContext = "arn:aws:iam::123456789012:role/ci@factory-coverage.us-east-1.eksctl.io"
	)

	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	config := clientcmdapi.NewConfig()
	config.CurrentContext = ambient
	config.Clusters[ambient] = &clientcmdapi.Cluster{Server: "https://127.0.0.1:6443"}
	config.AuthInfos[ambient] = &clientcmdapi.AuthInfo{Token: "ambient-token"}
	config.Contexts[ambient] = &clientcmdapi.Context{Cluster: ambient, AuthInfo: ambient}
	config.Clusters[targetContext] = &clientcmdapi.Cluster{Server: "https://127.0.0.1:6444"}
	config.AuthInfos[targetContext] = &clientcmdapi.AuthInfo{Token: "target-token"}
	config.Contexts[targetContext] = &clientcmdapi.Context{
		Cluster:  targetContext,
		AuthInfo: targetContext,
	}
	require.NoError(t, clientcmd.WriteToFile(*config, kubeconfigPath))

	restore := cluster.SetProvisionerFactoryForTests(fakeFactory{})
	defer restore()

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	ctx := newFullyPopulatedFactoryContext()
	ctx.ClusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
	ctx.ClusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
	ctx.ClusterCfg.Spec.Cluster.Connection.Kubeconfig = kubeconfigPath
	ctx.EKSConfig.Region = region

	_, err := cluster.ExportCreateAndVerifyProvisioner(cmd, ctx, clusterName)
	require.NoError(t, err)
	assert.Equal(t, targetContext, ctx.ClusterCfg.Spec.Cluster.Connection.Context)
}
