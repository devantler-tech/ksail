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

	onNetwork := []dockerclient.RegistryInfo{
		reg("foo-local-registry"),
		reg("foo-ghcr.io"),
		reg("foo-bar-local-registry"),
		reg("foo-bar-ghcr.io"),
	}

	// "foo-bar" is a real cluster, as reported by cluster discovery.
	got := mirrorregistry.FilterRegistriesByClusterName(onNetwork, "foo", []string{"foo-bar"})

	assert.ElementsMatch(t, []string{"foo-local-registry", "foo-ghcr.io"}, names(got),
		"tearing down foo must take only foo's registries; foo-bar is a live cluster")

	// And the longer-named cluster still gets everything of its own.
	gotBar := mirrorregistry.FilterRegistriesByClusterName(onNetwork, "foo-bar", []string{"foo"})

	assert.ElementsMatch(t, []string{"foo-bar-local-registry", "foo-bar-ghcr.io"}, names(gotBar),
		"tearing down foo-bar must still find its own; a SHORTER rival never claims them")
}

// TestFilterRegistriesByClusterName_KeepsRegistriesWhenNoRivalCluster verifies the narrowing does
// not over-correct, and is why the rival set comes from cluster discovery rather than from names.
//
// A cluster "foo" may legitimately configure a mirror host called "bar-local-registry", producing
// a container "foo-bar-local-registry" that LOOKS like evidence of a cluster "foo-bar". No such
// cluster exists, so those registries are foo's and must be cleaned up rather than orphaned.
func TestFilterRegistriesByClusterName_KeepsRegistriesWhenNoRivalCluster(t *testing.T) {
	t.Parallel()

	onNetwork := []dockerclient.RegistryInfo{
		reg("foo-local-registry"),
		reg("foo-bar-local-registry"),
		reg("foo-bar-ghcr.io"),
	}

	got := mirrorregistry.FilterRegistriesByClusterName(onNetwork, "foo", []string{})

	assert.ElementsMatch(t,
		[]string{"foo-local-registry", "foo-bar-local-registry", "foo-bar-ghcr.io"},
		names(got),
		"no cluster foo-bar exists, so these are foo's mirrors and must not be left behind")
}

func labelled(name, cluster string) dockerclient.RegistryInfo {
	info := reg(name)
	info.ClusterName = cluster

	return info
}

// TestFilterRegistriesByClusterName_LabelDecidesOwnership covers the case name attribution cannot:
// a cluster "foo-bar" that has mirror registries but no local registry and is not in the rival set.
// Its "foo-bar-ghcr.io" mirror is indistinguishable by name from a "foo" mirror for host
// "bar-ghcr.io", so only the owning-cluster label keeps "foo" from deleting it.
func TestFilterRegistriesByClusterName_LabelDecidesOwnership(t *testing.T) {
	t.Parallel()

	onNetwork := []dockerclient.RegistryInfo{
		labelled("foo-ghcr.io", "foo"),
		labelled("foo-bar-ghcr.io", "foo-bar"),
	}

	got := mirrorregistry.FilterRegistriesByClusterName(onNetwork, "foo", []string{})

	assert.ElementsMatch(t, []string{"foo-ghcr.io"}, names(got),
		"foo must not claim a registry labelled as belonging to foo-bar")
}

// TestFilterRegistriesByClusterName_LabelDoesNotOverrideLiveLongerRival is the other direction:
// cluster "foo" created "foo-bar-ghcr.io" for its mirror host "bar-ghcr.io", and a live cluster
// "foo-bar" asking for its "ghcr.io" mirror resolves to the same container name. Registry creation
// reuses an existing container without recording a second owner, so the label cannot show whether
// foo-bar shares it. Deleting foo must leave it for foo-bar rather than break a live cluster.
func TestFilterRegistriesByClusterName_LabelDoesNotOverrideLiveLongerRival(t *testing.T) {
	t.Parallel()

	onNetwork := []dockerclient.RegistryInfo{
		labelled("foo-ghcr.io", "foo"),
		labelled("foo-bar-ghcr.io", "foo"),
		labelled("foo-bar-docker.io", "foo-bar"),
	}

	got := mirrorregistry.FilterRegistriesByClusterName(onNetwork, "foo", []string{"foo-bar"})

	assert.ElementsMatch(t, []string{"foo-ghcr.io"}, names(got),
		"a container a live foo-bar would reuse must not be deleted with foo")
}

// TestFilterRegistriesByClusterName_LabelClaimsNameWhenNoLongerRivalExists keeps the label's
// purpose when nothing else could share the container: foo's "bar-ghcr.io" mirror is removed with
// foo once no cluster "foo-bar" exists.
func TestFilterRegistriesByClusterName_LabelClaimsNameWhenNoLongerRivalExists(t *testing.T) {
	t.Parallel()

	onNetwork := []dockerclient.RegistryInfo{
		labelled("foo-bar-ghcr.io", "foo"),
	}

	got := mirrorregistry.FilterRegistriesByClusterName(onNetwork, "foo", []string{"baz"})

	assert.ElementsMatch(t, []string{"foo-bar-ghcr.io"}, names(got))
}

// TestFilterRegistriesByClusterName_UnlabelledFallsBackToName keeps registries created before the
// label existed working, alongside labelled ones on the same network.
func TestFilterRegistriesByClusterName_UnlabelledFallsBackToName(t *testing.T) {
	t.Parallel()

	onNetwork := []dockerclient.RegistryInfo{
		reg("foo-local-registry"),
		labelled("foo-ghcr.io", "foo"),
		reg("foo-bar-quay.io"),
	}

	got := mirrorregistry.FilterRegistriesByClusterName(onNetwork, "foo", []string{"foo-bar"})

	assert.ElementsMatch(t, []string{"foo-local-registry", "foo-ghcr.io"}, names(got))
}

// TestFilterRegistriesByClusterName_IgnoresUnrelatedClusters keeps the ordinary case honest.
func TestFilterRegistriesByClusterName_IgnoresUnrelatedClusters(t *testing.T) {
	t.Parallel()

	onNetwork := []dockerclient.RegistryInfo{
		reg("alpha-local-registry"),
		reg("beta-local-registry"),
		reg("beta-quay.io"),
	}

	got := mirrorregistry.FilterRegistriesByClusterName(onNetwork, "beta", []string{"alpha"})

	assert.ElementsMatch(t, []string{"beta-local-registry", "beta-quay.io"}, names(got))
}
