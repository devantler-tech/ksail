package setup_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/stretchr/testify/assert"
)

//nolint:funlen // Table-driven test with comprehensive test cases.
func TestBuildArgoCDEnsureOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		clusterCfg         *v1alpha1.Cluster
		clusterName        string
		registryHost       string
		wantTargetRevision string
	}{
		{
			name: "local registry with default tag",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						LocalRegistry: v1alpha1.LocalRegistry{},
					},
					Workload: v1alpha1.WorkloadSpec{
						SourceDirectory: "k8s",
					},
				},
			},
			clusterName:        "test-cluster",
			wantTargetRevision: "dev",
		},
		{
			name: "local registry with custom workload tag",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						LocalRegistry: v1alpha1.LocalRegistry{},
					},
					Workload: v1alpha1.WorkloadSpec{
						SourceDirectory: "k8s",
						Tag:             "latest",
					},
				},
			},
			clusterName:        "test-cluster",
			wantTargetRevision: "latest",
		},
		{
			name: "external registry with default tag",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						LocalRegistry: v1alpha1.LocalRegistry{
							Registry: "ghcr.io/example/repo",
						},
					},
					Workload: v1alpha1.WorkloadSpec{
						SourceDirectory: "k8s",
					},
				},
			},
			clusterName:        "test-cluster",
			wantTargetRevision: "dev",
		},
		{
			name: "external registry with registry-embedded tag",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						LocalRegistry: v1alpha1.LocalRegistry{
							Registry: "ghcr.io/example/repo:custom",
						},
					},
					Workload: v1alpha1.WorkloadSpec{
						SourceDirectory: "k8s",
					},
				},
			},
			clusterName:        "test-cluster",
			wantTargetRevision: "custom",
		},
		{
			name: "workload tag takes priority over registry-embedded tag",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						LocalRegistry: v1alpha1.LocalRegistry{
							Registry: "ghcr.io/example/repo:custom",
						},
					},
					Workload: v1alpha1.WorkloadSpec{
						SourceDirectory: "k8s",
						Tag:             "v1.0",
					},
				},
			},
			clusterName:        "test-cluster",
			wantTargetRevision: "v1.0",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			opts := setup.BuildArgoCDEnsureOptions(
				testCase.clusterCfg,
				testCase.clusterName,
				testCase.registryHost,
			)

			assert.Equal(t, testCase.wantTargetRevision, opts.TargetRevision)
			assert.Equal(t, "ksail", opts.ApplicationName)
			assert.Equal(t, ".", opts.SourcePath)
		})
	}
}
