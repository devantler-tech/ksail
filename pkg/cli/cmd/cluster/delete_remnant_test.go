package cluster_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/confirm"
	dockerpkg "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/docker/docker/api/types/container"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	v1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
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

// recordingProvisioner fails the test if the cluster is ever deleted. It exists so the
// unmanaged path can assert on what did NOT happen.
type recordingProvisioner struct{ deleteCalled *bool }

func (*recordingProvisioner) Create(context.Context, string) error { return nil }

func (p *recordingProvisioner) Delete(context.Context, string) error {
	*p.deleteCalled = true

	return nil
}

func (*recordingProvisioner) Start(context.Context, string) error    { return nil }
func (*recordingProvisioner) Stop(context.Context, string) error     { return nil }
func (*recordingProvisioner) List(context.Context) ([]string, error) { return nil, nil }
func (*recordingProvisioner) Exists(context.Context, string) (bool, error) {
	// The cluster DOES exist — this is the dangerous case. A foreign cluster of the same name
	// is running, and only the guard stands between it and deletion.
	return true, nil
}

type recordingFactory struct{ deleteCalled *bool }

func (f recordingFactory) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	return &recordingProvisioner{deleteCalled: f.deleteCalled}, &v1alpha4.Cluster{Name: "t"}, nil
}

// TestDelete_UnmanagedWithRegistryRemnant_NeverDeletesTheCluster is the safety property behind
// the remnant exception (#6286), and the reason that exception does not simply wave the
// unmanaged-cluster guard through.
//
// A surviving KSail-owned registry proves KSail created that CONTAINER. It does not prove the
// kubeconfig target is KSail's cluster. Cluster discovery skips a distribution whose listing
// fails WITHOUT reporting the result as incomplete, so a foreign cluster of the same name can be
// refused as unmanaged while an old registry remnant is still present — and that foreign cluster
// may be very much alive, as Exists returns true here.
//
// Under those conditions the registries may be removed, and the cluster must not be touched.
func TestDelete_UnmanagedWithRegistryRemnant_NeverDeletesTheCluster(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	kubeconfigPath := writeKubeconfigWithContext(t, workingDir, "kind-my-cluster")
	t.Setenv("KUBECONFIG", kubeconfigPath)

	deleteCalled := false
	defer cluster.SetProvisionerFactoryForTests(recordingFactory{deleteCalled: &deleteCalled})()

	// A KSail-owned registry for "my-cluster" is still running.
	mockClient := dockerpkg.NewMockAPIClient(t)
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{{
			ID:     "reg",
			Names:  []string{"/my-cluster-local-registry"},
			Labels: map[string]string{dockerpkg.RegistryLabelKey: "my-cluster-local-registry"},
		}}, nil).
		Maybe()
	mockClient.EXPECT().ContainerInspect(mock.Anything, mock.Anything).
		Return(container.InspectResponse{}, assert.AnError).Maybe()
	mockClient.EXPECT().ContainerStop(mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Maybe()
	mockClient.EXPECT().ContainerRemove(mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Maybe()
	mockClient.EXPECT().
		NetworkDisconnect(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).
		Maybe()

	defer cluster.SetDockerClientInvokerForTests(
		func(_ *cobra.Command, fn func(dockerpkg.Client) error) error { return fn(mockClient) },
	)()

	defer confirm.SetTTYCheckerForTests(func() bool { return false })()

	// The real guard against an empty managed set: "my-cluster" is unmanaged.
	defer cluster.ExportSetDeleteUnmanagedGuard(
		func(ctx context.Context, resolved *lifecycle.ResolvedClusterInfo) error {
			return cluster.ExportEnsureClusterManaged(ctx, resolved, map[string]struct{}{}, true)
		},
	)()

	cmd := cluster.NewDeleteCmd()

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	_ = cmd.Execute()

	assert.False(t, deleteCalled,
		"an unmanaged cluster must never be deleted, however many KSail registry containers "+
			"happen to share its name — the remnant only authorises removing those containers")
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
//
// Subtests deliberately run SERIALLY. remnantCmd installs a package-global Docker invoker and
// restores it via t.Cleanup; running the cases in parallel would let one case's mock answer
// another case's query, so the guard could report the wrong verdict — or restore a stale
// invoker and destabilise unrelated tests.
//
//nolint:paralleltest // shared global Docker invoker; parallel subtests would race (see above)
func TestHasClusterRegistryRemnant(t *testing.T) {
	for _, testCase := range remnantCases() {
		t.Run(testCase.name, func(t *testing.T) {
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

func ksailOwned(name string) container.Summary {
	return container.Summary{
		ID:     name,
		Names:  []string{"/" + name},
		Labels: map[string]string{dockerpkg.RegistryLabelKey: name},
	}
}

// remnantCases covers what does and does not count as evidence. The false cases are the safety
// property: each one is a way the remnant exception could otherwise authorise touching containers
// it has no business touching.
//
//nolint:funlen // One table of scenarios reads better whole than split across helpers.
func remnantCases() []remnantCase {
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
		{
			// Registry names are "<cluster>-<host>", so "foo-bar-docker.io" is ambiguous:
			// cluster "foo" with host "bar-docker.io", or cluster "foo-bar" with host
			// "docker.io"? A prefix test claims it for BOTH. Here "foo-bar" demonstrably
			// exists — it has its own local registry — so the registries are its, and
			// tearing down "foo" must not touch them.
			name:     "another cluster's longer name is NOT evidence for this one",
			cluster:  "foo",
			provider: v1alpha1.ProviderDocker,
			summaries: []container.Summary{
				ksailOwned("foo-bar-local-registry"),
				ksailOwned("foo-bar-docker.io"),
			},
			want:   false,
			reason: "those registries belong to the live cluster foo-bar, not to foo",
		},
		{
			// The same shape, but no cluster "foo-bar" exists, so "foo-bar-docker.io" really
			// is cluster "foo" with host "bar-docker.io" and must still be recognised.
			name:     "an unclaimed longer-looking name IS this cluster's",
			cluster:  "foo",
			provider: v1alpha1.ProviderDocker,
			summaries: []container.Summary{
				ksailOwned("foo-local-registry"),
				ksailOwned("foo-bar-docker.io"),
			},
			want:   true,
			reason: "no cluster foo-bar exists, so the registry is foo's",
		},
	}
}
