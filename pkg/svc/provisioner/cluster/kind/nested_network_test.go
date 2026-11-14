package kindprovisioner_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/system"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errNestedDockerUnavailable = errors.New("nested Docker unavailable")
	errUnexpectedNetwork       = errors.New("unexpected network")
)

// fakeNestedDocker answers the nested network check from fixed state and records removals.
type fakeNestedDocker struct {
	pools      []system.NetworkAddressPool
	infoErr    error
	network    network.Inspect
	inspectErr error
	removed    []string
}

func (f *fakeNestedDocker) Info(context.Context) (system.Info, error) {
	return system.Info{DefaultAddressPools: f.pools}, f.infoErr
}

func (f *fakeNestedDocker) NetworkInspect(
	_ context.Context, networkID string, _ network.InspectOptions,
) (network.Inspect, error) {
	if networkID != "kind" {
		return network.Inspect{}, fmt.Errorf("%w %q", errUnexpectedNetwork, networkID)
	}

	return f.network, f.inspectErr
}

func (f *fakeNestedDocker) NetworkRemove(_ context.Context, networkID string) error {
	f.removed = append(f.removed, networkID)

	return nil
}

func plannedPool() []system.NetworkAddressPool {
	return []system.NetworkAddressPool{{Base: "198.18.0.0/16", Size: 24}}
}

func kindNetwork(containers int, subnets ...string) network.Inspect {
	inspect := network.Inspect{
		ID:         "kind-network-id",
		Name:       "kind",
		Containers: make(map[string]network.EndpointResource),
	}

	for _, subnet := range subnets {
		inspect.IPAM.Config = append(inspect.IPAM.Config, network.IPAMConfig{Subnet: subnet})
	}

	for index := range containers {
		inspect.Containers[fmt.Sprint("node-", index)] = network.EndpointResource{}
	}

	return inspect
}

// An unused network left with a subnet outside the planned pool is removed so Kind recreates it
// from the pool.
func TestReconcileNestedKindNetworkRemovesUnusedNetworkOutsidePool(t *testing.T) {
	t.Parallel()

	for name, subnets := range map[string][]string{
		"overlapping host range": {"fc00:f853:ccd:e793::/64", "172.18.0.0/16"},
		"wider than the pool":    {"198.18.0.0/15"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			docker := &fakeNestedDocker{pools: plannedPool(), network: kindNetwork(0, subnets...)}

			require.NoError(
				t,
				kindprovisioner.ReconcileNestedKindNetworkForTest(t.Context(), docker),
			)
			assert.Equal(t, []string{"kind-network-id"}, docker.removed)
		})
	}
}

// A network outside the pool that still has containers attached is refused, because removing it
// would cut those containers off; the user is asked to delete the persisted state.
func TestReconcileNestedKindNetworkRefusesAttachedNetworkOutsidePool(t *testing.T) {
	t.Parallel()

	docker := &fakeNestedDocker{
		pools:   plannedPool(),
		network: kindNetwork(2, "172.18.0.0/16"),
	}

	err := kindprovisioner.ReconcileNestedKindNetworkForTest(t.Context(), docker)
	require.ErrorIs(t, err, kindprovisioner.ErrNestedKindNetworkInUse)
	assert.Contains(t, err.Error(), "delete the cluster's persisted DinD state")
	assert.Empty(t, docker.removed)
}

// A network inside the pool, an absent network, and a daemon without a planned IPv4 pool are left
// alone.
func TestReconcileNestedKindNetworkKeepsNetworksItNeedNotReplace(t *testing.T) {
	t.Parallel()

	for name, docker := range map[string]*fakeNestedDocker{
		"inside pool": {
			pools:   plannedPool(),
			network: kindNetwork(3, "198.18.4.0/24", "fc00:f853:ccd:e793::/64"),
		},
		"absent network": {
			pools:      plannedPool(),
			inspectErr: fmt.Errorf("no such network: %w", cerrdefs.ErrNotFound),
		},
		"no planned pool": {
			network: kindNetwork(0, "172.18.0.0/16"),
		},
		"ipv6 pool only": {
			pools:   []system.NetworkAddressPool{{Base: "fd00::/48", Size: 64}},
			network: kindNetwork(0, "172.18.0.0/16"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.NoError(
				t,
				kindprovisioner.ReconcileNestedKindNetworkForTest(t.Context(), docker),
			)
			assert.Empty(t, docker.removed)
		})
	}
}

// Read failures stop creation instead of letting Kind reuse an unchecked network.
func TestReconcileNestedKindNetworkFailsClosedOnReadErrors(t *testing.T) {
	t.Parallel()

	for name, docker := range map[string]*fakeNestedDocker{
		"info":    {infoErr: errNestedDockerUnavailable},
		"inspect": {pools: plannedPool(), inspectErr: errNestedDockerUnavailable},
		"pool":    {pools: []system.NetworkAddressPool{{Base: "not-a-prefix"}}},
		"subnet":  {pools: plannedPool(), network: kindNetwork(0, "not-a-subnet")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Error(t, kindprovisioner.ReconcileNestedKindNetworkForTest(t.Context(), docker))
			assert.Empty(t, docker.removed)
		})
	}
}
