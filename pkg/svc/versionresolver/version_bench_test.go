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
