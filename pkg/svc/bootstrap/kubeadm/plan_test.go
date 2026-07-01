package kubeadmbootstrap_test

import (
	"testing"

	kubeadmbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/kubeadm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// joinPlanInput returns a PlanInput with the run-time discovery fields populated,
// the baseline for the joining-node cases.
func joinPlanInput() kubeadmbootstrap.PlanInput {
	return kubeadmbootstrap.PlanInput{
		Token:             "abcdef.0123456789abcdef",
		ControlPlaneCount: 1,
		AgentCount:        0,
		APIServerEndpoint: "10.0.0.2:6443",
		CACertHashes:      []string{validCACertHash},
	}
}

func TestPlanSingleControlPlane(t *testing.T) {
	t.Parallel()

	nodes, err := kubeadmbootstrap.Plan(kubeadmbootstrap.PlanInput{
		Token:             "abcdef.0123456789abcdef",
		KubernetesVersion: "v1.31.0",
		ControlPlaneCount: 1,
	})
	require.NoError(t, err)
	require.Len(t, nodes, 1)

	assert.Equal(t, 0, nodes[0].Index)
	assert.Equal(t, kubeadmbootstrap.RoleServerInit, nodes[0].Config.Role)
	assert.Equal(t, "v1.31.0", nodes[0].Config.KubernetesVersion)
	// The cluster-initialising control plane joins nothing.
	assert.Empty(t, nodes[0].Config.APIServerEndpoint)
	assert.Empty(t, nodes[0].Config.CACertHashes)
}

func TestPlanControlPlaneAndAgents(t *testing.T) {
	t.Parallel()

	input := joinPlanInput()
	input.ControlPlaneCount = 2
	input.AgentCount = 3

	nodes, err := kubeadmbootstrap.Plan(input)
	require.NoError(t, err)
	require.Len(t, nodes, 5)

	wantRoles := []kubeadmbootstrap.Role{
		kubeadmbootstrap.RoleServerInit,
		kubeadmbootstrap.RoleServer,
		kubeadmbootstrap.RoleAgent,
		kubeadmbootstrap.RoleAgent,
		kubeadmbootstrap.RoleAgent,
	}

	for index, node := range nodes {
		assert.Equal(t, index, node.Index, "node index must match bootstrap order")
		assert.Equal(t, wantRoles[index], node.Config.Role)
	}

	// Joining nodes carry the discovery fields; the server-init node does not.
	assert.Empty(t, nodes[0].Config.APIServerEndpoint)

	for _, node := range nodes[1:] {
		assert.Equal(t, "10.0.0.2:6443", node.Config.APIServerEndpoint)
		assert.Equal(t, []string{validCACertHash}, node.Config.CACertHashes)
		// Joining nodes never carry cluster-wide options.
		assert.Empty(t, node.Config.KubernetesVersion)
		assert.Empty(t, node.Config.ControlPlaneEndpoint)
	}
}

// TestPlanProducesRenderableConfigs pins Plan's central contract: every Config it
// returns passes validation, so Render never fails for a planned node.
func TestPlanProducesRenderableConfigs(t *testing.T) {
	t.Parallel()

	input := joinPlanInput()
	input.KubernetesVersion = "v1.31.0"
	input.ControlPlaneEndpoint = "cluster.example:6443"
	input.CertSANs = []string{"cluster.example"}
	input.PodSubnet = "10.244.0.0/16"
	input.ControlPlaneCount = 3
	input.AgentCount = 2

	nodes, err := kubeadmbootstrap.Plan(input)
	require.NoError(t, err)

	for _, node := range nodes {
		_, renderErr := kubeadmbootstrap.Render(node.Config)
		require.NoErrorf(t, renderErr, "Render must not fail for planned node %d", node.Index)
	}
}

// TestPlanClonesSlices guards against slice-header aliasing: mutating a planned
// node's CertSANs or CACertHashes must not corrupt the caller's input or a
// sibling node.
func TestPlanClonesSlices(t *testing.T) {
	t.Parallel()

	sans := []string{"a.example", "b.example"}
	hashes := []string{validCACertHash}

	input := joinPlanInput()
	input.CertSANs = sans
	input.ControlPlaneCount = 2
	input.AgentCount = 1
	input.CACertHashes = hashes

	nodes, err := kubeadmbootstrap.Plan(input)
	require.NoError(t, err)

	nodes[0].Config.CertSANs[0] = "mutated"
	nodes[1].Config.CACertHashes[0] = "mutated"

	assert.Equal(t, "a.example", sans[0], "input CertSANs must be unaffected")
	assert.Equal(t, validCACertHash, hashes[0], "input CACertHashes must be unaffected")
	assert.Equal(t, validCACertHash, nodes[2].Config.CACertHashes[0], "sibling node unaffected")
}

// invalidPlanCase is one rejected-input fixture: a mutation of the join baseline
// and the sentinel error Plan must report for it.
type invalidPlanCase struct {
	mutate  func(*kubeadmbootstrap.PlanInput)
	wantErr error
}

// invalidPlanCases returns the rejected-input fixtures, extracted from the test
// body so the table-driven test stays within the function-length budget.
func invalidPlanCases() map[string]invalidPlanCase {
	return map[string]invalidPlanCase{
		"missing token": {
			mutate:  func(in *kubeadmbootstrap.PlanInput) { in.Token = "" },
			wantErr: kubeadmbootstrap.ErrMissingToken,
		},
		"zero control planes": {
			mutate:  func(in *kubeadmbootstrap.PlanInput) { in.ControlPlaneCount = 0 },
			wantErr: kubeadmbootstrap.ErrInvalidControlPlaneCount,
		},
		"negative agents": {
			mutate:  func(in *kubeadmbootstrap.PlanInput) { in.AgentCount = -1 },
			wantErr: kubeadmbootstrap.ErrInvalidAgentCount,
		},
		"joining node missing endpoint": {
			mutate: func(in *kubeadmbootstrap.PlanInput) {
				in.AgentCount = 1
				in.APIServerEndpoint = ""
			},
			wantErr: kubeadmbootstrap.ErrMissingAPIServerEndpoint,
		},
		"joining node invalid endpoint": {
			mutate: func(in *kubeadmbootstrap.PlanInput) {
				in.AgentCount = 1
				in.APIServerEndpoint = "no-port"
			},
			wantErr: kubeadmbootstrap.ErrInvalidAPIServerEndpoint,
		},
		"joining node missing CA hash": {
			mutate: func(in *kubeadmbootstrap.PlanInput) {
				in.AgentCount = 1
				in.CACertHashes = nil
			},
			wantErr: kubeadmbootstrap.ErrMissingCACertHashes,
		},
		"joining node invalid CA hash": {
			mutate: func(in *kubeadmbootstrap.PlanInput) {
				in.AgentCount = 1
				in.CACertHashes = []string{"sha256:deadbeef"}
			},
			wantErr: kubeadmbootstrap.ErrInvalidCACertHash,
		},
	}
}

func TestPlanRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	for name, test := range invalidPlanCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			input := joinPlanInput()
			test.mutate(&input)

			nodes, err := kubeadmbootstrap.Plan(input)
			require.Error(t, err)
			assert.Nil(t, nodes)
			assert.ErrorIs(t, err, test.wantErr)
		})
	}
}

// TestPlanAllowsValidJoinDiscovery confirms a single-control-plane plan needs no
// discovery fields, so the no-join baseline is never rejected for missing them.
func TestPlanAllowsNoDiscoveryWhenNoJoiners(t *testing.T) {
	t.Parallel()

	nodes, err := kubeadmbootstrap.Plan(kubeadmbootstrap.PlanInput{
		Token:             "abcdef.0123456789abcdef",
		ControlPlaneCount: 1,
		AgentCount:        0,
	})
	require.NoError(t, err)
	assert.Len(t, nodes, 1)
}
