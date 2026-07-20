package mirrorregistry_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/mirrorregistry"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/stretchr/testify/assert"
)

func reg(name string) dockerclient.RegistryInfo {
	return dockerclient.RegistryInfo{Name: name, ID: name, IsKSailOwned: true}
}

func names(registries []dockerclient.RegistryInfo) []string {
	out := make([]string, 0, len(registries))
	for _, r := range registries {
		out = append(out, r.Name)
	}

	return out
}

// TestFilterRegistriesByClusterName_DoesNotClaimAnotherClustersRegistries is the safety property
// for teardown on a SHARED network.
//
// Every Kind cluster sits on the single "kind" network, so this filter sees other clusters'
// registries too — and whatever it returns is then deleted. Registry names are "<cluster>-<host>",
// which a bare prefix test cannot decode: "foo-bar-ghcr.io" is either cluster "foo" with host
// "bar-ghcr.io", or cluster "foo-bar" with host "ghcr.io". Claiming it for "foo" destroys a live
// cluster's mirrors.
func TestFilterRegistriesByClusterName_DoesNotClaimAnotherClustersRegistries(t *testing.T) {
	t.Parallel()

	// Both clusters are live on the shared network, and each has its own local registry.
	onNetwork := []dockerclient.RegistryInfo{
		reg("foo-local-registry"),
		reg("foo-ghcr.io"),
		reg("foo-bar-local-registry"),
		reg("foo-bar-ghcr.io"),
	}

	got := mirrorregistry.FilterRegistriesByClusterName(onNetwork, "foo")

	assert.ElementsMatch(t, []string{"foo-local-registry", "foo-ghcr.io"}, names(got),
		"tearing down foo must take only foo's registries; foo-bar is a live cluster")

	// And the longer-named cluster still gets everything of its own.
	gotBar := mirrorregistry.FilterRegistriesByClusterName(onNetwork, "foo-bar")

	assert.ElementsMatch(t, []string{"foo-bar-local-registry", "foo-bar-ghcr.io"}, names(gotBar),
		"tearing down foo-bar must still find its own registries")
}

// TestFilterRegistriesByClusterName_KeepsAmbiguousNameWhenNoRivalExists verifies the narrowing
// does not over-correct: with no cluster "foo-bar" present, "foo-bar-ghcr.io" really is cluster
// "foo" with a host called "bar-ghcr.io", and must still be cleaned up.
func TestFilterRegistriesByClusterName_KeepsAmbiguousNameWhenNoRivalExists(t *testing.T) {
	t.Parallel()

	onNetwork := []dockerclient.RegistryInfo{
		reg("foo-local-registry"),
		reg("foo-bar-ghcr.io"),
	}

	got := mirrorregistry.FilterRegistriesByClusterName(onNetwork, "foo")

	assert.ElementsMatch(t, []string{"foo-local-registry", "foo-bar-ghcr.io"}, names(got),
		"no cluster foo-bar exists, so the registry belongs to foo and must not be orphaned")
}

// TestFilterRegistriesByClusterName_IgnoresUnrelatedClusters keeps the ordinary case honest.
func TestFilterRegistriesByClusterName_IgnoresUnrelatedClusters(t *testing.T) {
	t.Parallel()

	onNetwork := []dockerclient.RegistryInfo{
		reg("alpha-local-registry"),
		reg("beta-local-registry"),
		reg("beta-quay.io"),
	}

	got := mirrorregistry.FilterRegistriesByClusterName(onNetwork, "beta")

	assert.ElementsMatch(t, []string{"beta-local-registry", "beta-quay.io"}, names(got))
}
