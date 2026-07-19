package configmanager_test

import (
	"io"
	"os"
	"testing"

	configmanagerinterface "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The real config path is viper+mapstructure, not sigs.k8s.io/yaml — so the
// new field's key mapping and env-var expansion are pinned against that exact
// decoder, end to end from file bytes to the loaded spec. Not parallel:
// t.Chdir and t.Setenv isolate file-system and environment state.
func TestLoadConfigLoadsEKSLoadBalancerControllerServiceAccount(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)
	t.Setenv("KSAIL_TEST_LBC_SA", "irsa-lbc-sa")

	ksailConfig := "apiVersion: ksail.io/v1alpha1\n" +
		"kind: Cluster\n" +
		"spec:\n" +
		"  cluster:\n" +
		"    distribution: EKS\n" +
		"    loadBalancer: Enabled\n" +
		"    eks:\n" +
		"      experimentalAWSLoadBalancerController: true\n" +
		"      awsLoadBalancerControllerServiceAccount: ${KSAIL_TEST_LBC_SA}\n"
	require.NoError(t, os.WriteFile("ksail.yaml", []byte(ksailConfig), 0o600))

	manager := configmanager.NewConfigManager(io.Discard, "")
	manager.Viper.SetConfigFile("ksail.yaml")

	cfg, err := manager.Load(configmanagerinterface.LoadOptions{})
	require.NoError(t, err)

	assert.True(t, cfg.Spec.Cluster.EKS.ExperimentalAWSLoadBalancerController)
	assert.Equal(t, "irsa-lbc-sa",
		cfg.Spec.Cluster.EKS.AWSLoadBalancerControllerServiceAccount,
		"the pre-created SA name must survive the viper+mapstructure decode "+
			"path with env-var placeholders expanded")
}
