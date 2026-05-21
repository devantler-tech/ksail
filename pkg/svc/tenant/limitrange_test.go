package tenant_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenerateLimitRangeManifests_Disabled(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateLimitRangeManifests(tenant.Options{
		Name:       "team",
		Namespaces: []string{"team"},
	})
	require.NoError(t, err)
	require.Nil(t, result)
}

func TestGenerateLimitRangeManifests_Defaults(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateLimitRangeManifests(tenant.Options{
		Name:           "team-alpha",
		Namespaces:     []string{"team-alpha"},
		WithLimitRange: true,
	})
	require.NoError(t, err)
	require.Contains(t, result, "limitrange.yaml")
	require.Contains(t, result["limitrange.yaml"], "kind: LimitRange")
	require.Contains(t, result["limitrange.yaml"], "type: Container")
	snaps.MatchSnapshot(t, result["limitrange.yaml"])
}

func TestGenerateLimitRangeManifests_MultiNamespace(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateLimitRangeManifests(tenant.Options{
		Name:            "team-beta",
		Namespaces:      []string{"ns-a", "ns-b"},
		WithLimitRange:  true,
		LimitDefaultCPU: "1",
	})
	require.NoError(t, err)

	docs := strings.Split(result["limitrange.yaml"], "---\n")
	require.Len(t, docs, 2)
	snaps.MatchSnapshot(t, result["limitrange.yaml"])
}

func TestGenerateLimitRangeManifests_InvalidQuantity(t *testing.T) {
	t.Parallel()

	_, err := tenant.GenerateLimitRangeManifests(tenant.Options{
		Name:            "team",
		Namespaces:      []string{"team"},
		WithLimitRange:  true,
		LimitDefaultCPU: "bad",
	})
	require.ErrorIs(t, err, tenant.ErrInvalidQuantity)
}
