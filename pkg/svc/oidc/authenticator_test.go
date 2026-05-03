package oidc_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/oidc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecCredentialJSON(t *testing.T) {
	t.Parallel()

	t.Run("produces valid ExecCredential", func(t *testing.T) {
		t.Parallel()

		expiry := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

		data, err := oidc.ExecCredentialJSON("my-id-token", expiry)
		require.NoError(t, err)

		var parsed map[string]any
		require.NoError(t, json.Unmarshal(data, &parsed))

		assert.Equal(t, "client.authentication.k8s.io/v1", parsed["apiVersion"])
		assert.Equal(t, "ExecCredential", parsed["kind"])

		status, ok := parsed["status"].(map[string]any)
		require.True(t, ok, "status should be a map")
		assert.Equal(t, "my-id-token", status["token"])
		assert.Equal(t, "2026-06-15T12:00:00Z", status["expirationTimestamp"])
	})
}
