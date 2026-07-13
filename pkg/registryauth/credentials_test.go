package registryauth_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/registryauth"
	"github.com/stretchr/testify/assert"
)

func TestPullPassword(t *testing.T) {
	t.Setenv(registryauth.GHCRPullTokenEnvVar, "pull-token")

	assert.Equal(t, "pull-token", registryauth.PullPassword("ghcr.io", "push-token"))
	assert.Equal(t, "pull-token", registryauth.PullPassword("GHCR.IO", "push-token"))
	assert.Equal(
		t,
		"custom-token",
		registryauth.PullPassword("registry.example.com", "custom-token"),
	)
}

func TestPullPassword_EmptyPullTokenFallsBack(t *testing.T) {
	t.Setenv(registryauth.GHCRPullTokenEnvVar, "")

	assert.Equal(t, "push-token", registryauth.PullPassword("ghcr.io", "push-token"))
}

func TestPullEnvLookup_FallsBackToLegacyToken(t *testing.T) {
	t.Setenv(registryauth.GHCRPullTokenEnvVar, "")
	t.Setenv(registryauth.GHCRTokenEnvVar, "legacy-token")

	value, exists := registryauth.PullEnvLookup(registryauth.GHCRPullTokenEnvVar)

	assert.True(t, exists)
	assert.Equal(t, "legacy-token", value)
}

func TestPullEnvLookup_PrefersDedicatedPullToken(t *testing.T) {
	t.Setenv(registryauth.GHCRPullTokenEnvVar, "pull-token")
	t.Setenv(registryauth.GHCRTokenEnvVar, "push-token")

	value, exists := registryauth.PullEnvLookup(registryauth.GHCRPullTokenEnvVar)

	assert.True(t, exists)
	assert.Equal(t, "pull-token", value)
}
