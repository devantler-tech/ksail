package ciharness_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEKSReadOnlyAuthPreflightCannotProvision(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/system-test-eks.yaml")
	job, found := workflow.Jobs["auth-preflight"]
	require.True(t, found, "read-only authentication must have an independent job")
	assert.Contains(t, job.If, "inputs.preflight_only")
	assert.LessOrEqual(t, job.TimeoutMinutes, 15)
	assert.Equal(t, "write", job.Permissions["id-token"])

	for _, name := range []string{"build-artifact", "smoke-test"} {
		assert.Contains(t, workflow.Jobs[name].If, "!inputs.preflight_only",
			"authentication-only dispatch must not enter %s", name)
	}

	auth := findHarnessStep(t, job.Steps, "🔐 Check existing AWS OIDC role")
	assert.Equal(t, 7200, auth.With["role-duration-seconds"])
	assert.Equal(t, true, auth.With["output-credentials"])
	assert.Equal(t, true, auth.With["mask-aws-account-id"])
	policy, ok := auth.With["inline-session-policy"].(string)
	require.True(t, ok)

	var permission struct {
		Statement []struct {
			Effect   string   `json:"effect"`
			Action   []string `json:"action"`
			Resource string   `json:"resource"`
		} `json:"statement"`
	}
	require.NoError(t, json.Unmarshal([]byte(policy), &permission))
	require.Len(t, permission.Statement, 1)
	assert.Equal(t, "Allow", permission.Statement[0].Effect)
	assert.Equal(t, []string{"sts:GetCallerIdentity"}, permission.Statement[0].Action)
	assert.Equal(t, "*", permission.Statement[0].Resource)

	validate := findHarnessStep(t, job.Steps, "🔎 Verify OIDC identity and actual expiration")
	assert.Equal(t, "${{ steps.oidc.outputs.aws-expiration }}",
		validate.Env["AWS_SESSION_EXPIRATION"])
	assert.Contains(t, validate.Run, "aws sts get-caller-identity")
	assert.Contains(t, validate.Run, "go run ./internal/ciharness/cmd/eksauthpreflight")
}
