package credentials //nolint:testpackage // directly exercises the platform-neutral name normalizer.

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNormalizeEnvironmentNameHonorsPlatformCaseSensitivity verifies filtering
// follows the host's environment-name semantics.
func TestNormalizeEnvironmentNameHonorsPlatformCaseSensitivity(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "aws_profile", normalizeEnvironmentName("aws_profile", false))
	assert.Equal(t, "AWS_PROFILE", normalizeEnvironmentName("aws_profile", true))
	assert.Equal(t, "AWS_PROFILE", normalizeEnvironmentName("AwS_PrOfIlE", true))
}
