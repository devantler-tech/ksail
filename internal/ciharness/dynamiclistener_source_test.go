package ciharness_test

import "testing"

// Direct Git access cannot retrieve the selected tag or its original commit.
// Authenticate every copied byte so analysis retains the complete published
// implementation without depending on the unavailable repository source.
func TestDynamicListenerUsesAuthenticatedEquivalentSource(t *testing.T) {
	t.Parallel()

	verifyAnalysisSource(t, authenticatedDynamicListenerSource())
}
