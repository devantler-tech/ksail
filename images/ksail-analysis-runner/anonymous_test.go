package analysisrunner_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type anonymousCase struct {
	name, digest, denyPull, denySignature, wantTrace string
	wantSuccess                                      bool
}

func TestPublicPublicationRejectsPrivateOrUnverifiableDigest(t *testing.T) {
	t.Parallel()

	var public map[string]any

	signedIndex, publicIndex := -1, -1

	for index, step := range steps(t, value(t, readWorkflow(t), "jobs", "publish")) {
		switch step["name"] {
		case "Sign and verify published digest":
			signedIndex = index
		case "Verify anonymous image and signature access":
			public, publicIndex = step, index
		}
	}

	require.NotNil(t, public, "public consumers need credential-free acceptance")
	require.Greater(t, publicIndex, signedIndex)
	require.NotContains(t, public, "continue-on-error")
	require.NotContains(t, public, "if")
	require.Equal(t, "${{ steps.build.outputs.digest }}", value(t, public, "env", "DIGEST"))
	require.Equal(t, ".github/workflows/publish-ksail-analysis-runner.yaml",
		value(t, public, "env", "SIGNING_WORKFLOW"))
	run, ok := value(t, public, "run").(string)
	require.True(t, ok)

	for _, test := range []anonymousCase{
		{"public", "sha256:" + strings.Repeat("a", 64), "0", "0", "pull\nverify\n", true},
		{"private image", "sha256:" + strings.Repeat("a", 64), "1", "0", "pull\n", false},
		{"private or invalid signature", "sha256:" + strings.Repeat("a", 64), "0", "1", "pull\nverify\n", false},
		{"mutable reference", "latest", "0", "0", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runAnonymousCase(t, run, test)
		})
	}
}

func runAnonymousCase(t *testing.T, script string, test anonymousCase) {
	t.Helper()

	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })
	require.NoError(t, root.Mkdir("publisher", 0o700))
	require.NoError(t, root.WriteFile("publisher/config.json",
		[]byte(`{"auths":{"ghcr.io":{"auth":"publisher-fixture"}}}`), 0o600))
	require.NoError(t, root.WriteFile("trace", nil, 0o600))
	anonymousTools(t, dir)

	command := exec.CommandContext(t.Context(), "/bin/bash")
	command.Stdin = strings.NewReader(script)
	command.Env = anonymousEnvironment(dir, test)

	output, err := command.CombinedOutput()
	if test.wantSuccess {
		require.NoError(t, err, "%s", output)
	} else {
		require.Error(t, err, "%s", output)
	}

	calls, err := root.ReadFile("trace")
	require.NoError(t, err)
	require.Equal(t, test.wantTrace, string(calls))

	if test.wantTrace != "" {
		config, readErr := root.ReadFile("config-path")
		require.NoError(t, readErr)

		name, relErr := filepath.Rel(dir, string(config))
		require.NoError(t, relErr)

		_, statErr := root.Stat(name)
		require.ErrorIs(
			t, statErr, os.ErrNotExist,
			"temporary credential-free configuration survives",
		)
	}

	original, err := root.ReadFile("publisher/config.json")
	require.NoError(t, err)
	require.Contains(t, string(original), "publisher-fixture")
}

func anonymousEnvironment(dir string, test anonymousCase) []string {
	return []string{
		"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + dir,
		"TMPDIR=" + dir,
		"DOCKER_CONFIG=" + filepath.Join(dir, "publisher"),
		"PUBLISHER_CONFIG=" + filepath.Join(dir, "publisher"),
		"TRACE=" + filepath.Join(dir, "trace"),
		"CONFIG_CAPTURE=" + filepath.Join(dir, "config-path"),
		"IMAGE=" + imageRepository,
		"DIGEST=" + test.digest,
		"GITHUB_SERVER_URL=https://github.com",
		"GITHUB_REPOSITORY=devantler-tech/ksail",
		"SIGNING_WORKFLOW=.github/workflows/publish-ksail-analysis-runner.yaml",
		"DENY_PULL=" + test.denyPull,
		"DENY_SIGNATURE=" + test.denySignature,
	}
}

func anonymousTools(t *testing.T, dir string) {
	t.Helper()

	assertAnonymous := `test "$DOCKER_CONFIG" != "$PUBLISHER_CONFIG"
test "$(cat "$DOCKER_CONFIG/config.json")" = '{"auths":{}}'
printf '%s' "$DOCKER_CONFIG" > "$CONFIG_CAPTURE"
`

	fixtures := map[string]string{
		"timeout": "case \"$1\" in 90s|180s) ;; *) exit 99 ;; esac\nshift\nexec \"$@\"\n",
		"docker": assertAnonymous + `test "$#" -eq 2
test "$1" = pull
test "$2" = "$IMAGE@$DIGEST"
printf 'pull\n' >> "$TRACE"
test "$DENY_PULL" = 0
`,
		"cosign": assertAnonymous + `identity="$GITHUB_SERVER_URL/$GITHUB_REPOSITORY/$SIGNING_WORKFLOW@refs/heads/main"
test "$#" -eq 6
test "$1" = verify
test "$2" = --certificate-identity
test "$3" = "$identity"
test "$4" = --certificate-oidc-issuer
test "$5" = https://token.actions.githubusercontent.com
test "$6" = "$IMAGE@$DIGEST"
printf 'verify\n' >> "$TRACE"
test "$DENY_SIGNATURE" = 0
`,
	}
	for name, body := range fixtures {
		writeExecutable(t, filepath.Join(dir, name), body)
	}
}
