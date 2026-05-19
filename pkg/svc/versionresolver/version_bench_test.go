package versionresolver_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/versionresolver"
)

func BenchmarkVersionString(b *testing.B) {
	versions := []versionresolver.Version{
		{Major: 1, Minor: 35, Patch: 3},
		{Major: 1, Minor: 35, Patch: 3, PreRelease: "rc.1"},
		{Major: 1, Minor: 35, Patch: 3, Suffix: "k3s1"},
		{Major: 1, Minor: 35, Patch: 3, PreRelease: "beta.1", Suffix: "k3s1"},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		for _, v := range versions {
			_ = v.String()
		}
	}
}

// BenchmarkParseTags_MixedTags benchmarks ParseTags with a realistic mix of tags
// that includes semver versions, non-semver labels, and special tags such as
// "latest" and "sha256:..." references — representative of a real ghcr.io repository.
func BenchmarkParseTags_MixedTags(b *testing.B) {
	tags := []string{
		"v1.35.3", "v1.35.2", "v1.35.1", "v1.35.0",
		"v1.34.5", "v1.34.4", "v1.34.3", "v1.34.2", "v1.34.1", "v1.34.0",
		"v1.35.3-rc.1", "v1.35.2-beta.1", "v1.35.1-alpha.1",
		"v1.35.3-k3s1", "v1.35.2-k3s1", "v1.35.1-k3s1", "v1.35.0-k3s1",
		"latest", "latest-slim", "", "main", "develop", "nightly",
		"sha256:abc123def456", "20240101", "edge",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = versionresolver.ParseTags(tags)
	}
}

// BenchmarkFilterStable_LargeInput benchmarks FilterStable on a slice of 200 parsed
// versions — a realistic upper bound for a Kubernetes node image repository.
// This exercises the hot path where IsStable() is called for every version.
func BenchmarkFilterStable_LargeInput(b *testing.B) {
	// Build 200 tags: 150 stable, 30 release-candidates, 20 alpha/beta.
	tags := make([]string, 0, 200)
	for minor := 0; minor < 15; minor++ {
		for patch := 0; patch < 10; patch++ {
			tags = append(tags, "v1."+string(rune('0'+minor))+"."+string(rune('0'+patch)))
		}
	}
	for i := 0; i < 30; i++ {
		tags = append(tags, "v1.35."+string(rune('0'+i%10))+"-rc.1")
	}
	for i := 0; i < 20; i++ {
		tags = append(tags, "v1.35."+string(rune('0'+i%10))+"-alpha.1")
	}

	versions := versionresolver.ParseTags(tags)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = versionresolver.FilterStable(versions)
	}
}

