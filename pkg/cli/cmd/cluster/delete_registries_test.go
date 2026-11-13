package cluster_test

import (
	"bytes"
	"context"
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
	mirrorRegistryID   = "mirror-registry-id"
	mirrorRegistryName = "my-cluster-docker.io"
	kindNetworkName    = "kind"
)

// mirrorRegistryDocker is a Docker mock holding one mirror registry of the Kind cluster
// "my-cluster", attached to the "kind" network. It remembers whether the registry is
// still attached and whether it was removed, as Docker would.
type mirrorRegistryDocker struct {
	mu        sync.Mutex
	connected bool
	removed   bool
	client    *dockerpkg.MockAPIClient
}

func newMirrorRegistryDocker(t *testing.T) *mirrorRegistryDocker {
	t.Helper()

	docker := &mirrorRegistryDocker{
		connected: true,
		client:    dockerpkg.NewMockAPIClient(t),
	}

	docker.client.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		RunAndReturn(docker.listContainers).
		Maybe()
	docker.client.EXPECT().
		ContainerInspect(mock.Anything, mirrorRegistryID).
		RunAndReturn(docker.inspectRegistry).
		Maybe()
	docker.client.EXPECT().
		NetworkDisconnect(mock.Anything, mock.Anything, mirrorRegistryID, mock.Anything).
		RunAndReturn(docker.disconnectRegistry).
		Maybe()
	docker.client.EXPECT().
		ContainerStop(mock.Anything, mirrorRegistryID, mock.Anything).
		Return(nil).
		Maybe()
	docker.client.EXPECT().
		ContainerRemove(mock.Anything, mirrorRegistryID, mock.Anything).
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

// listContainers lists the mirror registry until it is removed.
func (d *mirrorRegistryDocker) listContainers(
	context.Context,
	container.ListOptions,
) ([]container.Summary, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.removed {
		return []container.Summary{}, nil
	}

	return []container.Summary{{
		ID:     mirrorRegistryID,
		Names:  []string{"/" + mirrorRegistryName},
		Image:  dockerpkg.RegistryImageName,
		State:  container.StateRunning,
		Labels: map[string]string{dockerpkg.RegistryLabelKey: mirrorRegistryName},
	}}, nil
}

// inspectRegistry shows the registry on the "kind" network while it is attached.
func (d *mirrorRegistryDocker) inspectRegistry(
	context.Context,
	string,
) (container.InspectResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	networks := map[string]*network.EndpointSettings{}
	if d.connected {
		networks[kindNetworkName] = &network.EndpointSettings{}
	}

	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			ID:    mirrorRegistryID,
			Name:  "/" + mirrorRegistryName,
			State: &container.State{Status: container.StateRunning, Running: true},
		},
		Config:          &container.Config{Labels: map[string]string{}},
		NetworkSettings: &container.NetworkSettings{Networks: networks},
	}, nil
}

// disconnectRegistry detaches the registry from the network.
func (d *mirrorRegistryDocker) disconnectRegistry(context.Context, string, string, bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.connected = false

	return nil
}

// removeRegistry removes the registry container.
func (d *mirrorRegistryDocker) removeRegistry(
	context.Context,
	string,
	container.RemoveOptions,
) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.removed = true

	return nil
}

func (d *mirrorRegistryDocker) state() (bool, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.connected, d.removed
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

// TestDelete_DeclinedThenForced_RemovesMirrorRegistries reproduces #8353: a delete that the
// user declined detached the cluster's mirror registries from its network before asking, so a
// later forced delete, which finds registries on that network, missed them and left them
// running. A declined delete must change nothing, and the forced delete must remove them.
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

	connected, removed := docker.state()
	require.True(t, connected, "a declined delete must leave the registry on the cluster network")
	require.False(t, removed, "a declined delete must not remove the registry")

	output, err = runDelete(t, []string{"--force"}, "")
	require.NoError(t, err)
	require.Contains(t, output, "cluster deleted")
	require.Contains(
		t,
		output,
		"registries deleted",
		"the forced delete must say it removed the registries",
	)

	_, removed = docker.state()
	require.True(t, removed, "the forced delete must remove the mirror registry:\n%s", output)
}
