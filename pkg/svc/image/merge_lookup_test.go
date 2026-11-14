package image_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/image"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

const fixtureDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// A content repair can re-pull a tag@digest name by its tag, so the archive
// entry is found under that tag and published under the name it holds.
func TestExportedDescriptorFindsRepairedTag(t *testing.T) {
	t.Parallel()

	tagged := ocispec.Descriptor{Digest: "sha256:aa"}
	byName := map[string]ocispec.Descriptor{"docker.io/library/nginx:1.25": tagged}

	name, descriptor, found := image.ExportedDescriptorForTest(
		byName, "docker.io/library/nginx:1.25@"+fixtureDigest)
	require.True(t, found)
	require.Equal(t, "docker.io/library/nginx:1.25", name)
	require.Equal(t, tagged, descriptor)
}

// An exact entry wins over a repair candidate, and a digest-only name never
// falls back to an unrelated tag of the same repository.
func TestExportedDescriptorKeepsCoverageStrict(t *testing.T) {
	t.Parallel()

	exact := ocispec.Descriptor{Digest: "sha256:bb"}
	byName := map[string]ocispec.Descriptor{
		"docker.io/library/nginx:1.25@" + fixtureDigest: exact,
		"docker.io/library/nginx:1.25":                  {Digest: "sha256:aa"},
		"docker.io/library/nginx:latest":                {Digest: "sha256:cc"},
	}

	name, descriptor, found := image.ExportedDescriptorForTest(
		byName, "docker.io/library/nginx:1.25@"+fixtureDigest)
	require.True(t, found)
	require.Equal(t, "docker.io/library/nginx:1.25@"+fixtureDigest, name)
	require.Equal(t, exact, descriptor)

	_, _, found = image.ExportedDescriptorForTest(byName, "docker.io/library/nginx@"+fixtureDigest)
	require.False(t, found)
}

// A combined archive is complete for one platform only, so a node on another
// platform is refused rather than merged into an archive it cannot serve.
func TestRequireSamePlatformRefusesMixedNodes(t *testing.T) {
	t.Parallel()

	amd64 := ocispec.Platform{OS: "linux", Architecture: "amd64"}
	arm64 := ocispec.Platform{OS: "linux", Architecture: "arm64"}

	require.NoError(t, image.RequireSamePlatformForTest(nil, arm64))
	require.NoError(t, image.RequireSamePlatformForTest([]ocispec.Platform{amd64}, amd64))
	require.ErrorIs(t,
		image.RequireSamePlatformForTest([]ocispec.Platform{amd64}, arm64),
		image.ErrIncompleteExport)

	armV6 := ocispec.Platform{OS: "linux", Architecture: "arm", Variant: "v6"}
	armV7 := ocispec.Platform{OS: "linux", Architecture: "arm", Variant: "v7"}
	require.ErrorIs(t,
		image.RequireSamePlatformForTest([]ocispec.Platform{armV7}, armV6),
		image.ErrIncompleteExport)
}
