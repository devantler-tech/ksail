package cluster_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // The real command reads the process environment and working directory.
func TestStandaloneEKSLifecycleUsesSelectedContextWithoutConfig(t *testing.T) {
	for _, testCase := range standaloneEKSLifecycleCases() {
		if testCase.name == "delete" {
			continue
		}

		t.Run(testCase.name, func(t *testing.T) {
			const clusterName = "eks-context-6226"
			markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
			require.NoError(t, os.Remove("ksail.yaml"))
			require.NoError(t, os.Remove("eks.yaml"))
			require.NoError(t, state.SaveClusterSpec(clusterName, &v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionEKS,
				Provider:     v1alpha1.ProviderAWS,
			}))
			ownership, err := state.LoadEKSOwnershipState(clusterName, "ap-southeast-2")
			require.NoError(t, err)
			ownership.AWSOptions = v1alpha1.OptionsAWS{
				ProfileEnvVar:         "KSAIL_PROFILE",
				RegionEnvVar:          "KSAIL_REGION",
				AccessKeyIDEnvVar:     "KSAIL_ACCESS",
				SecretAccessKeyEnvVar: "KSAIL_SECRET",
				SessionTokenEnvVar:    "KSAIL_SESSION",
			}
			require.NoError(
				t,
				state.SaveEKSOwnershipState(clusterName, "ap-southeast-2", ownership),
			)
			t.Setenv("AWS_REGION", "us-east-1")
			t.Setenv("KSAIL_REGION", "")
			writeStandaloneEKSKubeconfigContexts(t, clusterName, []string{
				"operator@" + clusterName + ".ap-southeast-2.eksctl.io",
				clusterName + ".us-west-2.eksctl.io",
			})
			kubeconfigPath, err := filepath.Abs("kubeconfig")
			require.NoError(t, err)
			t.Setenv("KUBECONFIG", kubeconfigPath)
			configureStandaloneEKSNodegroupAction(t, testCase.name)

			runStandaloneEKSCommand(t, testCase.newCommand)

			assert.Equal(
				t,
				testCase.expectedCalls(clusterName),
				readStandaloneEKSCalls(t, markerPath),
			)
		})
	}
}
