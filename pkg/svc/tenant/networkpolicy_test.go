package tenant_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenerateNetworkPolicyManifests_Disabled(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateNetworkPolicyManifests(tenant.Options{
		Name:       "team",
		Namespaces: []string{"team"},
	})
	require.NoError(t, err)
	require.Empty(t, result)
}

func TestGenerateNetworkPolicyManifests_Native(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateNetworkPolicyManifests(tenant.Options{
		Name:                "team-alpha",
		Namespaces:          []string{"team-alpha"},
		WithNetworkPolicy:   true,
		NetworkPolicyEngine: tenant.NetworkPolicyEngineNative,
	})
	require.NoError(t, err)
	require.Contains(t, result, "networkpolicy.yaml")

	content := result["networkpolicy.yaml"]
	require.Contains(t, content, "kind: NetworkPolicy")
	require.Contains(t, content, "name: default-deny")
	require.Contains(t, content, "name: allow-dns")
	require.Contains(t, content, "name: allow-intra-namespace")

	// Three policies per namespace.
	docs := strings.Split(content, "---\n")
	require.Len(t, docs, 3)
	snaps.MatchSnapshot(t, content)
}

func TestGenerateNetworkPolicyManifests_NativeDefaultsWhenEngineEmpty(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateNetworkPolicyManifests(tenant.Options{
		Name:              "team",
		Namespaces:        []string{"team"},
		WithNetworkPolicy: true,
	})
	require.NoError(t, err)
	require.Contains(t, result["networkpolicy.yaml"], "apiVersion: networking.k8s.io/v1")
}

func TestGenerateNetworkPolicyManifests_Cilium(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateNetworkPolicyManifests(tenant.Options{
		Name:                "team-alpha",
		Namespaces:          []string{"team-alpha"},
		WithNetworkPolicy:   true,
		NetworkPolicyEngine: tenant.NetworkPolicyEngineCilium,
	})
	require.NoError(t, err)

	content := result["networkpolicy.yaml"]
	require.Contains(t, content, "kind: CiliumNetworkPolicy")
	require.Contains(t, content, "apiVersion: cilium.io/v2")
	snaps.MatchSnapshot(t, content)
}

func TestGenerateNetworkPolicyManifests_CiliumMultiNamespace(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateNetworkPolicyManifests(tenant.Options{
		Name:                "team-beta",
		Namespaces:          []string{"ns-a", "ns-b"},
		WithNetworkPolicy:   true,
		NetworkPolicyEngine: tenant.NetworkPolicyEngineCilium,
	})
	require.NoError(t, err)

	docs := strings.Split(result["networkpolicy.yaml"], "---\n")
	require.Len(t, docs, 2)
}
