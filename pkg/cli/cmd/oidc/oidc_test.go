package oidc_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/oidc"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOIDCCmd(t *testing.T) {
	t.Parallel()

	cmd := oidc.NewOIDCCmd()

	require.NotNil(t, cmd)
	assert.Equal(t, "oidc", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
}

func TestNewOIDCCmd_ExcludeAnnotation(t *testing.T) {
	t.Parallel()

	cmd := oidc.NewOIDCCmd()

	val, ok := cmd.Annotations[annotations.AnnotationExclude]
	assert.True(t, ok, "expected ai.toolgen.exclude annotation to be set")
	assert.Equal(t, "true", val)
}

func TestNewOIDCCmd_HasGetTokenSubcommand(t *testing.T) {
	t.Parallel()

	cmd := oidc.NewOIDCCmd()
	sub := findSubcommand(cmd, "get-token")

	require.NotNil(t, sub, "expected get-token subcommand to exist")
	assert.NotEmpty(t, sub.Short)
	assert.NotEmpty(t, sub.Long)
}

func TestGetTokenCmd_RequiredFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		expectError bool
	}{
		{
			name:        "missing both required flags",
			args:        []string{"get-token"},
			expectError: true,
		},
		{
			name:        "missing client-id",
			args:        []string{"get-token", "--issuer-url=https://dex.example.com"},
			expectError: true,
		},
		{
			name:        "missing issuer-url",
			args:        []string{"get-token", "--client-id=kubectl"},
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cmd := oidc.NewOIDCCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(tc.args)

			err := cmd.Execute()
			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetTokenCmd_OptionalFlags(t *testing.T) {
	t.Parallel()

	cmd := oidc.NewOIDCCmd()
	sub := findSubcommand(cmd, "get-token")
	require.NotNil(t, sub)

	assert.NotNil(t, sub.Flags().Lookup("extra-scope"), "expected extra-scope flag")
	assert.NotNil(t, sub.Flags().Lookup("ca-file"), "expected ca-file flag")
}

func TestOIDCCmd_Help(t *testing.T) {
	t.Parallel()

	cmd := oidc.NewOIDCCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, out.String(), "oidc")
}

// findSubcommand searches for a subcommand by name.
func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, sub := range parent.Commands() {
		if sub.Name() == name {
			return sub
		}
	}

	return nil
}
