package v1alpha1_test

import (
	"slices"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestDefaultHetznerFallbackLocations(t *testing.T) {
	t.Parallel()

	got := v1alpha1.DefaultHetznerFallbackLocations()

	// Two distinct fallbacks that exclude the primary location: otherwise the
	// location-fallback retry path would have nothing meaningful to try.
	assert.Len(t, got, 2)
	assert.NotContains(t, got, v1alpha1.DefaultHetznerLocation)

	// Each call must return an independent slice so a caller that retains and
	// mutates the result cannot corrupt the package-level default.
	want := slices.Clone(got)
	got[0] = "mutated"

	assert.Equal(t, want, v1alpha1.DefaultHetznerFallbackLocations())
}
