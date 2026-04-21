package versionresolver_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/versionresolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errVersionResolverRegistryUnreachable = errors.New("registry unreachable")

// TestParseTags_SkipsLatestAndEmpty verifies that "latest" and empty tags are skipped.
func TestParseTags_SkipsLatestAndEmpty(t *testing.T) {
	t.Parallel()

	tags := []string{"latest", "", "v1.0.0", "latest", "", "v2.0.0"}

	versions := versionresolver.ParseTags(tags)

	require.Len(t, versions, 2)
	assert.Equal(t, "v1.0.0", versions[0].Original)
	assert.Equal(t, "v2.0.0", versions[1].Original)
}

// TestParseTags_SkipsUnparseableTags verifies unparseable tags are silently skipped.
func TestParseTags_SkipsUnparseableTags(t *testing.T) {
	t.Parallel()

	tags := []string{"not-a-version", "abc123", "v1.0.0", "random-text"}

	versions := versionresolver.ParseTags(tags)

	require.Len(t, versions, 1)
	assert.Equal(t, "v1.0.0", versions[0].Original)
}

// TestParseTags_AllUnparseable returns empty slice for all unparseable tags.
func TestParseTags_AllUnparseable(t *testing.T) {
	t.Parallel()

	tags := []string{"latest", "", "not-valid"}

	versions := versionresolver.ParseTags(tags)

	assert.Empty(t, versions)
}

// TestComputeUpgradePath_InvalidCurrentTag verifies error when current tag is invalid.
func TestComputeUpgradePath_InvalidCurrentTag(t *testing.T) {
	t.Parallel()

	resolver := &mockResolver{
		versions: parseTags([]string{"v1.0.0"}),
	}

	_, err := versionresolver.ComputeUpgradePath(
		context.Background(), resolver, "kindest/node", "not-a-version", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing current version")
}

// TestComputeUpgradePath_ResolverError verifies error propagation from resolver.
func TestComputeUpgradePath_ResolverError(t *testing.T) {
	t.Parallel()

	resolver := &mockResolver{
		err: errVersionResolverRegistryUnreachable,
	}

	_, err := versionresolver.ComputeUpgradePath(
		context.Background(), resolver, "kindest/node", "v1.0.0", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing versions")
}

// TestComputeUpgradePath_NoStableVersions verifies error when all versions are pre-release.
func TestComputeUpgradePath_NoStableVersions(t *testing.T) {
	t.Parallel()

	resolver := &mockResolver{
		versions: parseTags([]string{
			"v1.0.0-alpha.1", "v1.0.0-beta.1", "v1.0.0-rc.1",
		}),
	}

	_, err := versionresolver.ComputeUpgradePath(
		context.Background(), resolver, "kindest/node", "v0.9.0", "")

	require.Error(t, err)
	assert.ErrorIs(t, err, versionresolver.ErrNoVersionsFound)
}

// TestComputeUpgradePath_SuffixFilterEmpty verifies error when suffix filter produces empty set.
func TestComputeUpgradePath_SuffixFilterEmpty(t *testing.T) {
	t.Parallel()

	resolver := &mockResolver{
		versions: parseTags([]string{"v1.0.0", "v1.1.0", "v1.2.0"}),
	}

	_, err := versionresolver.ComputeUpgradePath(
		context.Background(), resolver, "rancher/k3s", "v1.0.0-k3s1", "k3s")

	require.Error(t, err)
	require.ErrorIs(t, err, versionresolver.ErrNoVersionsFound)
	assert.Contains(t, err.Error(), "suffix")
}

// TestVersion_String verifies the string formatting of versions.
func TestVersion_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tag  string
		want string
	}{
		{name: "plain", tag: "v1.35.1", want: "v1.35.1"},
		{name: "with pre-release", tag: "v1.13.0-beta.1", want: "v1.13.0-beta.1"},
		{name: "with suffix", tag: "v1.35.3-k3s1", want: "v1.35.3-k3s1"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			v, err := versionresolver.ParseVersion(testCase.tag)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, v.String())
		})
	}
}

// TestVersion_Equal verifies equal comparison of versions.
func TestVersion_Equal(t *testing.T) {
	t.Parallel()

	versionOne, err := versionresolver.ParseVersion("v1.35.3-k3s1")
	require.NoError(t, err)

	versionTwo, err := versionresolver.ParseVersion("v1.35.3-k3s1")
	require.NoError(t, err)

	versionThree, err := versionresolver.ParseVersion("v1.35.3-k3s2")
	require.NoError(t, err)

	assert.True(t, versionOne.Equal(versionTwo))
	assert.False(t, versionOne.Equal(versionThree))
}

// TestVersion_IsStable verifies stable detection.
func TestVersion_IsStable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tag    string
		stable bool
	}{
		{name: "stable", tag: "v1.0.0", stable: true},
		{name: "alpha", tag: "v1.0.0-alpha.1", stable: false},
		{name: "beta", tag: "v1.0.0-beta.1", stable: false},
		{name: "rc", tag: "v1.0.0-rc.1", stable: false},
		{name: "suffix stable", tag: "v1.0.0-k3s1", stable: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			v, err := versionresolver.ParseVersion(testCase.tag)
			require.NoError(t, err)
			assert.Equal(t, testCase.stable, v.IsStable())
		})
	}
}

// TestParseVersion_PreReleaseInSuffix tests that pre-release indicators in suffix
// are promoted to the PreRelease field.
func TestParseVersion_PreReleaseInSuffix(t *testing.T) {
	t.Parallel()

	v, err := versionresolver.ParseVersion("v1.0.0-dev")
	require.NoError(t, err)

	assert.Equal(t, "dev", v.PreRelease)
	assert.Empty(t, v.Suffix)
	assert.False(t, v.IsStable())
}

// TestSuffixNum_EdgeCases tests suffix number extraction edge cases
// by exercising the Less method with suffixes that have no trailing numbers.
func TestSuffixNum_EdgeCases(t *testing.T) {
	t.Parallel()

	// suffixes with no trailing number like "abc" should sort equal
	versionOne, err := versionresolver.ParseVersion("v1.0.0-abc")
	require.NoError(t, err)

	versionTwo, err := versionresolver.ParseVersion("v1.0.0-xyz")
	require.NoError(t, err)

	// Both have suffixNum=0, so Less returns false in both directions
	assert.False(t, versionOne.Less(versionTwo))
	assert.False(t, versionTwo.Less(versionOne))
}

// TestLess_PreReleaseOrdering tests pre-release vs stable ordering.
func TestLess_PreReleaseOrdering(t *testing.T) {
	t.Parallel()

	preRelease, err := versionresolver.ParseVersion("v1.35.1-rc.1")
	require.NoError(t, err)

	stable, err := versionresolver.ParseVersion("v1.35.1")
	require.NoError(t, err)

	assert.True(t, preRelease.Less(stable), "pre-release should be less than stable")
	assert.False(t, stable.Less(preRelease), "stable should not be less than pre-release")
}

// TestLess_MajorMinorPrecedence tests major and minor version ordering.
func TestLess_MajorMinorPrecedence(t *testing.T) {
	t.Parallel()

	versionOne, err := versionresolver.ParseVersion("v1.0.0")
	require.NoError(t, err)

	versionTwo, err := versionresolver.ParseVersion("v2.0.0")
	require.NoError(t, err)

	assert.True(t, versionOne.Less(versionTwo))
	assert.False(t, versionTwo.Less(versionOne))
}

// TestNewOCIResolver tests the OCIResolver constructor.
func TestNewOCIResolver(t *testing.T) {
	t.Parallel()

	resolver := versionresolver.NewOCIResolver()
	require.NotNil(t, resolver)
}

// TestParseVersion_NumericOverflow tests that extremely large version numbers
// are rejected.
func TestParseVersion_NumericOverflow(t *testing.T) {
	t.Parallel()

	_, err := versionresolver.ParseVersion("v99999999999999999999.0.0")
	require.Error(t, err)
	require.ErrorIs(t, err, versionresolver.ErrInvalidVersion)
	assert.Contains(t, err.Error(), "numeric overflow")
}
