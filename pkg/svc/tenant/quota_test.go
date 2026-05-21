package tenant_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenerateResourceQuotaManifests_Disabled(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateResourceQuotaManifests(tenant.Options{
		Name:       "team",
		Namespaces: []string{"team"},
	})
	require.NoError(t, err)
	require.Nil(t, result)
}

func TestGenerateResourceQuotaManifests_Defaults(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateResourceQuotaManifests(tenant.Options{
		Name:       "team-alpha",
		Namespaces: []string{"team-alpha"},
		WithQuota:  true,
	})
	require.NoError(t, err)
	require.Contains(t, result, "resourcequota.yaml")
	require.Contains(t, result["resourcequota.yaml"], "kind: ResourceQuota")
	require.Contains(t, result["resourcequota.yaml"], `requests.cpu: "4"`)
	snaps.MatchSnapshot(t, result["resourcequota.yaml"])
}

func TestGenerateResourceQuotaManifests_CustomAndMultiNamespace(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateResourceQuotaManifests(tenant.Options{
		Name:        "team-beta",
		Namespaces:  []string{"ns-a", "ns-b"},
		WithQuota:   true,
		QuotaCPU:    "8",
		QuotaMemory: "16Gi",
	})
	require.NoError(t, err)

	docs := strings.Split(result["resourcequota.yaml"], "---\n")
	require.Len(t, docs, 2)
	snaps.MatchSnapshot(t, result["resourcequota.yaml"])
}

func TestGenerateResourceQuotaManifests_InvalidQuantity(t *testing.T) {
	t.Parallel()

	_, err := tenant.GenerateResourceQuotaManifests(tenant.Options{
		Name:       "team",
		Namespaces: []string{"team"},
		WithQuota:  true,
		QuotaCPU:   "not-a-quantity",
	})
	require.ErrorIs(t, err, tenant.ErrInvalidQuantity)
}
