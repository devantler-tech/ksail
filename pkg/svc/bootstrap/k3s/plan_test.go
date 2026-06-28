package k3sbootstrap_test

import (
	"testing"

	k3sbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/k3s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// roleAt is a tiny helper that returns the role of the node at index i, keeping
// the table assertions readable.
func roleAt(nodes []k3sbootstrap.Node, i int) k3sbootstrap.Role {
	return nodes[i].Config.Role
}

//nolint:funlen // Table-driven test enumerating each cluster topology.
func TestPlanRolesAndOrdering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     k3sbootstrap.PlanInput
		wantRoles []k3sbootstrap.Role
	}{
		{
			name: "single control-plane node, no agents",
			input: k3sbootstrap.PlanInput{
				Version:           "v1.30.2+k3s1",
				Token:             "secret",
				ControlPlaneCount: 1,
			},
			wantRoles: []k3sbootstrap.Role{k3sbootstrap.RoleServerInit},
		},
		{
			name: "ha control plane, no agents",
			input: k3sbootstrap.PlanInput{
				Version:           "v1.30.2+k3s1",
				Token:             "secret",
				ControlPlaneCount: 3,
				ServerURL:         "https://10.0.0.2:6443",
			},
			wantRoles: []k3sbootstrap.Role{
				k3sbootstrap.RoleServerInit,
				k3sbootstrap.RoleServer,
				k3sbootstrap.RoleServer,
			},
		},
		{
			name: "single control plane with agents",
			input: k3sbootstrap.PlanInput{
				Version:           "v1.30.2+k3s1",
				Token:             "secret",
				ControlPlaneCount: 1,
				AgentCount:        2,
				ServerURL:         "https://10.0.0.2:6443",
			},
			wantRoles: []k3sbootstrap.Role{
				k3sbootstrap.RoleServerInit,
				k3sbootstrap.RoleAgent,
				k3sbootstrap.RoleAgent,
			},
		},
		{
			name: "ha control plane with agents",
			input: k3sbootstrap.PlanInput{
				Version:           "v1.30.2+k3s1",
				Token:             "secret",
				ControlPlaneCount: 3,
				AgentCount:        2,
				ServerURL:         "https://10.0.0.2:6443",
			},
			wantRoles: []k3sbootstrap.Role{
				k3sbootstrap.RoleServerInit,
				k3sbootstrap.RoleServer,
				k3sbootstrap.RoleServer,
				k3sbootstrap.RoleAgent,
				k3sbootstrap.RoleAgent,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			nodes, err := k3sbootstrap.Plan(test.input)
			require.NoError(t, err)
			require.Len(t, nodes, len(test.wantRoles))

			for i, wantRole := range test.wantRoles {
				assert.Equalf(t, i, nodes[i].Index, "node %d index", i)
				assert.Equalf(t, wantRole, roleAt(nodes, i), "node %d role", i)
			}
		})
	}
}

func TestPlanJoinSettings(t *testing.T) {
	t.Parallel()

	nodes, err := k3sbootstrap.Plan(k3sbootstrap.PlanInput{
		Version:           "v1.30.2+k3s1",
		Token:             "secret",
		ControlPlaneCount: 2,
		AgentCount:        1,
		ServerURL:         "https://10.0.0.2:6443",
	})
	require.NoError(t, err)
	require.Len(t, nodes, 3)

	// The cluster-initialising server must not carry a ServerURL.
	assert.Empty(t, nodes[0].Config.ServerURL, "server-init must not have a server URL")
	// Joining nodes register against the provided endpoint.
	assert.Equal(t, "https://10.0.0.2:6443", nodes[1].Config.ServerURL, "additional server URL")
	assert.Equal(t, "https://10.0.0.2:6443", nodes[2].Config.ServerURL, "agent server URL")
}

func TestPlanServerOnlyOptions(t *testing.T) {
	t.Parallel()

	nodes, err := k3sbootstrap.Plan(k3sbootstrap.PlanInput{
		Version:             "v1.30.2+k3s1",
		Token:               "secret",
		ControlPlaneCount:   2,
		AgentCount:          1,
		ServerURL:           "https://10.0.0.2:6443",
		TLSSANs:             []string{"lb.example.com"},
		Disable:             []string{"traefik"},
		WriteKubeconfigMode: "0644",
	})
	require.NoError(t, err)

	// Both control-plane roles carry the server-only options.
	for _, i := range []int{0, 1} {
		assert.Equalf(t, []string{"lb.example.com"}, nodes[i].Config.TLSSANs, "node %d TLS SANs", i)
		assert.Equalf(t, []string{"traefik"}, nodes[i].Config.Disable, "node %d disables", i)
		assert.Equalf(t, "0644", nodes[i].Config.WriteKubeconfigMode, "node %d kubeconfig mode", i)
	}

	// The agent must not inherit server-only options (Render rejects them).
	agent := nodes[2].Config
	assert.Empty(t, agent.TLSSANs, "agent must not carry TLS SANs")
	assert.Empty(t, agent.Disable, "agent must not carry disables")
	assert.Empty(t, agent.WriteKubeconfigMode, "agent must not carry kubeconfig mode")
}

// TestPlanConfigsRender is the key invariant: every node a valid Plan produces
// must Render without error, since Plan promises Render-ready configs.
func TestPlanConfigsRender(t *testing.T) {
	t.Parallel()

	nodes, err := k3sbootstrap.Plan(k3sbootstrap.PlanInput{
		Version:             "v1.30.2+k3s1",
		Token:               "secret",
		ControlPlaneCount:   3,
		AgentCount:          2,
		ServerURL:           "https://10.0.0.2:6443",
		TLSSANs:             []string{"lb.example.com"},
		Disable:             []string{"servicelb"},
		WriteKubeconfigMode: "0644",
	})
	require.NoError(t, err)

	for _, node := range nodes {
		cmd, renderErr := k3sbootstrap.Render(node.Config)
		require.NoErrorf(t, renderErr, "node %d must render", node.Index)
		assert.NotEmptyf(t, cmd, "node %d command", node.Index)
	}
}

func TestPlanErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   k3sbootstrap.PlanInput
		wantErr error
	}{
		{
			name:    "missing version",
			input:   k3sbootstrap.PlanInput{Token: "secret", ControlPlaneCount: 1},
			wantErr: k3sbootstrap.ErrMissingVersion,
		},
		{
			name:    "missing token",
			input:   k3sbootstrap.PlanInput{Version: "v1.30.2+k3s1", ControlPlaneCount: 1},
			wantErr: k3sbootstrap.ErrMissingToken,
		},
		{
			name: "zero control-plane nodes",
			input: k3sbootstrap.PlanInput{
				Version: "v1.30.2+k3s1", Token: "secret", ControlPlaneCount: 0,
			},
			wantErr: k3sbootstrap.ErrInvalidControlPlaneCount,
		},
		{
			name: "negative agent count",
			input: k3sbootstrap.PlanInput{
				Version: "v1.30.2+k3s1", Token: "secret", ControlPlaneCount: 1, AgentCount: -1,
			},
			wantErr: k3sbootstrap.ErrInvalidAgentCount,
		},
		{
			name: "additional servers without a server URL",
			input: k3sbootstrap.PlanInput{
				Version: "v1.30.2+k3s1", Token: "secret", ControlPlaneCount: 3,
			},
			wantErr: k3sbootstrap.ErrMissingServerURL,
		},
		{
			name: "agents without a server URL",
			input: k3sbootstrap.PlanInput{
				Version: "v1.30.2+k3s1", Token: "secret", ControlPlaneCount: 1, AgentCount: 2,
			},
			wantErr: k3sbootstrap.ErrMissingServerURL,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			nodes, err := k3sbootstrap.Plan(test.input)
			require.ErrorIs(t, err, test.wantErr)
			assert.Nil(t, nodes, "no plan on error")
		})
	}
}
