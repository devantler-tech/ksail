package cluster_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	dockerpkg "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// remnantCmd returns a command wired with a Docker invoker that reports the given registry
// containers, so the remnant evidence check can be driven entirely offline.
func remnantCmd(t *testing.T, summaries []container.Summary) *cobra.Command {
	t.Helper()

	mockClient := dockerpkg.NewMockAPIClient(t)
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return(summaries, nil).
		Maybe()

	t.Cleanup(cluster.SetDockerClientInvokerForTests(
		func(_ *cobra.Command, fn func(dockerpkg.Client) error) error {
			return fn(mockClient)
		},
	))

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	return cmd
}

func dockerCluster(name string) *lifecycle.ResolvedClusterInfo {
	return &lifecycle.ResolvedClusterInfo{
		ClusterName: name,
		Provider:    v1alpha1.ProviderDocker,
	}
}

// TestHasClusterRegistryRemnant is the decision guard for #6286.
//
// When every node container is gone, KSail's ownership discovery reports the cluster as
// unmanaged and `cluster delete` refuses — leaving the user no supported way to remove the
// registry containers still holding host ports. Surviving KSail-owned registries are the
// evidence that KSail did provision the cluster, and this check is what reads that evidence.
//
// The false cases are the safety property: they are what keeps the relaxation from becoming a
// blanket bypass of the unmanaged-cluster guard.
func TestHasClusterRegistryRemnant(t *testing.T) {
	t.Parallel()

	for _, testCase := range remnantCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := remnantCmd(t, testCase.summaries)

			resolved := dockerCluster(testCase.cluster)
			resolved.Provider = testCase.provider

			got := cluster.ExportHasClusterRegistryRemnant(cmd, resolved)

			assert.Equal(t, testCase.want, got, testCase.reason)
		})
	}
}

type remnantCase struct {
	name      string
	cluster   string
	provider  v1alpha1.Provider
	summaries []container.Summary
	want      bool
	reason    string
}

func remnantCases() []remnantCase {
	ksailOwned := func(name string) container.Summary {
		return container.Summary{
			ID:     name,
			Names:  []string{"/" + name},
			Labels: map[string]string{dockerpkg.RegistryLabelKey: name},
		}
	}

	return []remnantCase{
		{
			name:      "ksail-owned registry for this cluster is evidence",
			cluster:   "spike",
			provider:  v1alpha1.ProviderDocker,
			summaries: []container.Summary{ksailOwned("spike-local-registry")},
			want:      true,
			reason:    "KSail created this container, so it provisioned the cluster",
		},
		{
			name:     "registry KSail does not own is NOT evidence",
			cluster:  "spike",
			provider: v1alpha1.ProviderDocker,
			summaries: []container.Summary{{
				ID:    "spike-local-registry",
				Names: []string{"/spike-local-registry"},
				// No KSail label: a name collision must never authorise a delete.
			}},
			want:   false,
			reason: "an unlabelled container is not proof KSail owns the cluster",
		},
		{
			name:      "another cluster's registry is NOT evidence",
			cluster:   "spike",
			provider:  v1alpha1.ProviderDocker,
			summaries: []container.Summary{ksailOwned("other-cluster-local-registry")},
			want:      false,
			reason:    "evidence must be scoped to the targeted cluster",
		},
		{
			name:      "no registries at all is NOT evidence",
			cluster:   "spike",
			provider:  v1alpha1.ProviderDocker,
			summaries: []container.Summary{},
			want:      false,
			reason:    "with nothing left behind the guard's normal refusal must stand",
		},
		{
			name:      "non-Docker provider is NOT evidence",
			cluster:   "spike",
			provider:  v1alpha1.ProviderAWS,
			summaries: []container.Summary{ksailOwned("spike-local-registry")},
			want:      false,
			reason:    "only the Docker provider creates these containers",
		},
	}
}
