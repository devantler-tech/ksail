package cluster_test

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/confirm"
	dockerpkg "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	mirrorRegistryID        = "mirror-registry-id"
	mirrorRegistryName      = "my-cluster-docker.io"
	otherClusterRegistryID  = "other-cluster-registry-id"
	otherClusterRegistryRef = "other-cluster-docker.io"
	kindNetworkName         = "kind"
)

// registryState is what Docker knows about one registry container.
type registryState struct {
	name      string
	connected bool
	removed   bool
}

// mirrorRegistryDocker is a Docker mock holding two mirror registries on the shared "kind"
// network: one of the Kind cluster "my-cluster" and one of another cluster. It remembers
// whether each registry is still attached and whether it was removed, as Docker would.
type mirrorRegistryDocker struct {
	mu         sync.Mutex
	registries map[string]*registryState
	order      []string
	client     *dockerpkg.MockAPIClient
}

func newMirrorRegistryDocker(t *testing.T) *mirrorRegistryDocker {
	t.Helper()

	docker := &mirrorRegistryDocker{
		registries: map[string]*registryState{
			mirrorRegistryID:       {name: mirrorRegistryName, connected: true},
			otherClusterRegistryID: {name: otherClusterRegistryRef, connected: true},
		},
		order:  []string{mirrorRegistryID, otherClusterRegistryID},
		client: dockerpkg.NewMockAPIClient(t),
	}

	docker.client.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		RunAndReturn(docker.listContainers).
		Maybe()
	docker.client.EXPECT().
		ContainerInspect(mock.Anything, mock.Anything).
		RunAndReturn(docker.inspectRegistry).
		Maybe()
	docker.client.EXPECT().
		NetworkDisconnect(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(docker.disconnectRegistry).
		Maybe()
	docker.client.EXPECT().
		ContainerStop(mock.Anything, mock.Anything, mock.Anything).
		Return(nil).
		Maybe()
	docker.client.EXPECT().
		ContainerRemove(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(docker.removeRegistry).
		Maybe()
	docker.client.EXPECT().
		VolumeRemove(mock.Anything, mock.Anything, mock.Anything).
		Return(nil).
		Maybe()
	docker.client.EXPECT().
		NetworkList(mock.Anything, mock.Anything).
		Return([]network.Summary{}, nil).
		Maybe()
	docker.client.EXPECT().Close().Return(nil).Maybe()

	t.Cleanup(cluster.SetDockerClientInvokerForTests(
		func(_ *cobra.Command, operation func(dockerpkg.Client) error) error {
			return operation(docker.client)
		},
	))

	return docker
}

// listContainers lists every registry that has not been removed and that matches the request's
// name filters, so a lookup by name finds only that registry, as it would in Docker.
func (d *mirrorRegistryDocker) listContainers(
	_ context.Context,
	options container.ListOptions,
) ([]container.Summary, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	nameFilters := options.Filters.Get("name")
	summaries := make([]container.Summary, 0, len(d.order))

	for _, containerID := range d.order {
		registry := d.registries[containerID]
		if registry.removed || !matchesNameFilters("/"+registry.name, nameFilters) {
			continue
		}

		summaries = append(summaries, container.Summary{
			ID:     containerID,
			Names:  []string{"/" + registry.name},
			Image:  dockerpkg.RegistryImageName,
			State:  container.StateRunning,
			Labels: map[string]string{dockerpkg.RegistryLabelKey: registry.name},
		})
	}

	return summaries, nil
}

// matchesNameFilters reports whether a container name matches any of Docker's name filters, each
// a regular expression matched against the name with its leading slash. No filter matches all.
func matchesNameFilters(name string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}

	for _, pattern := range patterns {
		matched, err := regexp.MatchString(pattern, name)
		if err == nil && matched {
			return true
		}
	}

	return false
}

// inspectRegistry shows the registry on the "kind" network while it is attached.
func (d *mirrorRegistryDocker) inspectRegistry(
	_ context.Context,
	containerID string,
) (container.InspectResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	registry := d.registries[containerID]

	networks := map[string]*network.EndpointSettings{}
	if registry.connected {
		networks[kindNetworkName] = &network.EndpointSettings{}
	}

	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			ID:    containerID,
			Name:  "/" + registry.name,
			State: &container.State{Status: container.StateRunning, Running: true},
		},
		Config:          &container.Config{Labels: map[string]string{}},
		NetworkSettings: &container.NetworkSettings{Networks: networks},
	}, nil
}

// disconnectRegistry detaches the registry from the network.
func (d *mirrorRegistryDocker) disconnectRegistry(
	_ context.Context,
	_, containerID string,
	_ bool,
) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.registries[containerID].connected = false

	return nil
}

// removeRegistry removes the registry container.
func (d *mirrorRegistryDocker) removeRegistry(
	_ context.Context,
	containerID string,
	_ container.RemoveOptions,
) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.registries[containerID].removed = true

	return nil
}

func (d *mirrorRegistryDocker) state(containerID string) (bool, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.registries[containerID].connected, d.registries[containerID].removed
}

func runDelete(t *testing.T, args []string, stdin string) (string, error) {
	t.Helper()

	if stdin != "" {
		t.Cleanup(confirm.SetStdinReaderForTests(strings.NewReader(stdin)))
	}

	cmd := cluster.NewDeleteCmd()
	cmd.SetArgs(args)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	err := cmd.Execute()

	return out.String(), err
}

// requireOtherClusterRegistryKept checks that the other cluster's registry on the shared
// network is still attached and was not removed.
func requireOtherClusterRegistryKept(t *testing.T, docker *mirrorRegistryDocker, step string) {
	t.Helper()

	connected, removed := docker.state(otherClusterRegistryID)
	require.True(t, connected, "%s must leave another cluster's registry on the network", step)
	require.False(t, removed, "%s must not remove another cluster's registry", step)
}

// TestDelete_DeclinedThenForced_RemovesMirrorRegistries reproduces #8353: a delete that the
// user declined detached the cluster's mirror registries from its network before asking, so a
// later forced delete, which finds registries on that network, missed them and left them
// running. A declined delete must change nothing, the forced delete must remove them, and
// neither may touch another cluster's registry on the same network.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() and t.Setenv() in helper
func TestDelete_DeclinedThenForced_RemovesMirrorRegistries(t *testing.T) {
	// Registered first so it runs last, after the restores below (t.Cleanup is LIFO).
	t.Cleanup(setupContextBasedTest(t, "kind-my-cluster", true, nil))

	docker := newMirrorRegistryDocker(t)

	t.Cleanup(confirm.SetTTYCheckerForTests(func() bool { return true }))

	output, err := runDelete(t, nil, "no\n")
	require.ErrorIs(t, err, confirm.ErrDeletionCancelled)
	require.Contains(t, output, mirrorRegistryName, "the prompt lists the mirror registry")

	connected, removed := docker.state(mirrorRegistryID)
	require.True(t, connected, "a declined delete must leave the registry on the cluster network")
	require.False(t, removed, "a declined delete must not remove the registry")
	requireOtherClusterRegistryKept(t, docker, "a declined delete")

	output, err = runDelete(t, []string{"--force"}, "")
	require.NoError(t, err)
	require.Contains(t, output, "cluster deleted")
	require.Contains(
		t,
		output,
		"registries deleted",
		"the forced delete must say it removed the registries",
	)

	_, removed = docker.state(mirrorRegistryID)
	require.True(t, removed, "the forced delete must remove the mirror registry:\n%s", output)
	requireOtherClusterRegistryKept(t, docker, "the forced delete")
}
