package flux_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/flux"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

const podinfoGitURL = "--url=https://github.com/stefanprodan/podinfo"

func exportSourceGit(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var outBuf bytes.Buffer

	createCmd := setupFluxCommand(&outBuf)
	createCmd.SetArgs(
		append([]string{"source", "git", "podinfo", podinfoGitURL, "--export"}, args...),
	)

	err := createCmd.Execute()

	return outBuf.String(), err
}

// TestCreateGitRepository_VerifySignedCommits snapshots the opt-in spec.verify block and
// checks each manifest against the Flux GitRepository v1 schema.
func TestCreateGitRepository_VerifySignedCommits(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
	}{
		{name: "head-default", args: []string{"--branch=master", "--verify-secret-ref=git-signing-keys"}},
		{
			name: "tag-and-head",
			args: []string{"--tag=6.6.2", "--verify-secret-ref=git-signing-keys", "--verify-mode=TagAndHEAD"},
		},
		{
			name: "tag-semver",
			args: []string{"--tag-semver=>=6.0.0", "--verify-secret-ref=git-signing-keys", "--verify-mode=Tag"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			output, err := exportSourceGit(t, testCase.args...)
			require.NoError(t, err)
			require.Contains(t, output, "verify:")
			snaps.MatchSnapshot(t, output)

			// --export omits apiVersion and kind today, so name them to select the Flux
			// GitRepository v1 schema the spec.verify block must satisfy.
			manifest := filepath.Join(t.TempDir(), "gitrepository.yaml")
			require.NoError(t, os.WriteFile(manifest, []byte(
				"apiVersion: source.toolkit.fluxcd.io/v1\nkind: GitRepository\n"+output), 0o600))
			require.NoError(t, kubeconform.NewClient().ValidateFile(
				context.Background(),
				manifest, &kubeconform.ValidationOptions{SkipKinds: []string{}},
			),
			)
		})
	}
}

func TestCreateGitRepository_WithoutVerifyIsUnchanged(t *testing.T) {
	t.Parallel()

	output, err := exportSourceGit(t, "--branch=master")
	require.NoError(t, err)
	require.NotContains(t, output, "verify")
}

func TestCreateGitRepository_RejectsInvalidVerification(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		args    []string
		wantErr error
	}{
		{
			name:    "mode-without-secret",
			args:    []string{"--branch=master", "--verify-mode=HEAD"},
			wantErr: flux.ErrVerifyModeWithoutSecret,
		},
		{
			name:    "unknown-mode",
			args:    []string{"--branch=master", "--verify-secret-ref=keys", "--verify-mode=commit"},
			wantErr: flux.ErrInvalidVerifyMode,
		},
		{
			name:    "tag-mode-on-branch",
			args:    []string{"--branch=master", "--verify-secret-ref=keys", "--verify-mode=Tag"},
			wantErr: flux.ErrVerifyTagModeWithoutTag,
		},
		{
			// --branch wins over --tag, so the written reference has no tag to verify.
			name: "tag-mode-with-branch-and-tag",
			args: []string{
				"--branch=master", "--tag=6.6.2", "--verify-secret-ref=keys", "--verify-mode=TagAndHEAD",
			},
			wantErr: flux.ErrVerifyTagModeWithoutTag,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			output, err := exportSourceGit(t, testCase.args...)
			require.ErrorIs(t, err, testCase.wantErr)
			require.NotContains(
				t,
				output,
				"spec:",
				"a refused verification must not write a manifest",
			)
		})
	}
}
