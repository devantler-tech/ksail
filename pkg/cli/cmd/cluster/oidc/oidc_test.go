package oidc_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster/oidc"
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

const getTokenSubcmd = "get-token"

func TestNewOIDCCmd_HasGetTokenSubcommand(t *testing.T) {
	t.Parallel()

	cmd := oidc.NewOIDCCmd()
	sub := findSubcommand(cmd, getTokenSubcmd)

	require.NotNil(t, sub, "expected get-token subcommand to exist")
	assert.NotEmpty(t, sub.Short)
	assert.NotEmpty(t, sub.Long)
}

func TestGetTokenCmd_RequiredFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		args           []string
		errMustContain []string
	}{
		{
			name:           "missing both required flags",
			args:           []string{getTokenSubcmd},
			errMustContain: []string{`required flag(s)`, "issuer-url"},
		},
		{
			name:           "missing client-id",
			args:           []string{getTokenSubcmd, "--issuer-url=https://dex.example.com"},
			errMustContain: []string{`required flag(s)`, "client-id"},
		},
		{
			name:           "missing issuer-url",
			args:           []string{getTokenSubcmd, "--client-id=kubectl"},
			errMustContain: []string{`required flag(s)`, "issuer-url"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := oidc.NewOIDCCmd()

			var out bytes.Buffer

			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(testCase.args)

			err := cmd.Execute()
			require.Error(t, err)

			for _, fragment := range testCase.errMustContain {
				assert.ErrorContains(t, err, fragment)
			}
		})
	}
}

func TestGetTokenCmd_OptionalFlags(t *testing.T) {
	t.Parallel()

	cmd := oidc.NewOIDCCmd()
	sub := findSubcommand(cmd, getTokenSubcmd)
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
