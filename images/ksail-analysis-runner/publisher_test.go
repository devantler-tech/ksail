package analysisrunner_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const imageRepository = "ghcr.io/devantler-tech/ksail-analysis-runner"

func TestPublisherRequiresVerifiedMainPush(t *testing.T) {
	t.Parallel()

	workflow := readWorkflow(t)
	require.Empty(t, value(t, workflow, "permissions"))
	require.Equal(t, imageRepository, value(t, workflow, "env", "IMAGE"))
	require.Equal(t, "${{ github.sha }}", value(t, workflow, "env", "IMAGE_TAG"))

	publish := value(t, workflow, "jobs", "publish")
	require.Equal(
		t,
		"github.event_name == 'push' && github.ref == 'refs/heads/main'",
		value(t, publish, "if"),
	)
	require.Equal(t, "verify", value(t, publish, "needs"))
	require.Equal(t, map[string]any{
		"contents": "read", "packages": "write", "id-token": "write",
	}, value(t, publish, "permissions"))
	require.Equal(
		t,
		map[string]any{"contents": "read"},
		value(t, workflow, "jobs", "verify", "permissions"),
	)

	pinnedAction := regexp.MustCompile(`@[0-9a-f]{40}$`)
	jobs, ok := value(t, workflow, "jobs").(map[string]any)
	require.True(t, ok)

	for _, job := range jobs {
		for _, step := range steps(t, job) {
			if uses, found := step["uses"]; found {
				require.Regexp(t, pinnedAction, uses)
			}

			if with, found := step["with"].(map[string]any); found {
				if _, checkout := with["persist-credentials"]; checkout {
					require.Equal(t, false, with["persist-credentials"])
				}
			}
		}
	}
}

func TestVerificationExercisesRestrictedExecutableMounts(t *testing.T) {
	t.Parallel()

	verify := value(t, readWorkflow(t), "jobs", "verify")

	var smoke string

	for _, step := range steps(t, verify) {
		if run, ok := step["run"].(string); ok && strings.Contains(run, "docker run") {
			require.Empty(t, smoke, "exactly one image smoke run")
			smoke = run
		}
	}

	require.NotEmpty(t, smoke)

	for _, required := range []string{
		"--user 1001:1001", "--read-only", "--cap-drop ALL",
		"--security-opt no-new-privileges", "--memory 4g", "--cpus 2",
		"--env HOME=/runner-data",
		"--tmpfs /runner-data:rw,exec,nosuid,nodev,uid=1001,gid=1001,size=2g",
		"--tmpfs /tmp:rw,exec,nosuid,nodev,uid=1001,gid=1001,size=1g",
		"ksail-analysis:verify /usr/local/bin/ksail-analysis-smoke",
	} {
		require.Contains(t, smoke, required)
	}

	require.NotContains(t, smoke, "--privileged")
	require.NotContains(t, smoke, "--volume")
}

func TestPublicationSignsAndVerifiesExactDigestAndIdentity(t *testing.T) {
	t.Parallel()

	publish := value(t, readWorkflow(t), "jobs", "publish")

	var build, signature map[string]any

	for _, step := range steps(t, publish) {
		if step["id"] == "build" {
			build = step
		}

		if step["name"] == "Sign and verify published digest" {
			signature = step
		}
	}

	require.NotNil(t, build)
	require.Equal(t, true, value(t, build, "with", "push"))
	require.Equal(t, "${{ env.IMAGE }}:${{ env.IMAGE_TAG }}", value(t, build, "with", "tags"))
	require.Equal(t, "mode=max", value(t, build, "with", "provenance"))
	require.Equal(t, true, value(t, build, "with", "sbom"))
	require.NotNil(t, signature)
	require.Equal(t, "${{ steps.build.outputs.digest }}", value(t, signature, "env", "DIGEST"))
	run := value(t, signature, "run")
	require.Contains(t, run, `cosign sign --yes "${IMAGE}@${DIGEST}"`)
	require.Contains(t, run, "cosign verify")
	require.Contains(
		t,
		run,
		`--certificate-identity "https://github.com/devantler-tech/ksail/`+
			`.github/workflows/publish-ksail-analysis-runner.yaml@refs/heads/main"`,
	)
	require.Contains(
		t,
		run,
		`--certificate-oidc-issuer "https://token.actions.githubusercontent.com"`,
	)
}

func readWorkflow(t *testing.T) map[string]any {
	t.Helper()

	content, err := os.ReadFile("../../.github/workflows/publish-ksail-analysis-runner.yaml")
	require.NoError(t, err)

	var workflow map[string]any
	require.NoError(t, yaml.Unmarshal(content, &workflow))

	return workflow
}

func value(t *testing.T, mapping any, keys ...string) any {
	t.Helper()

	for _, key := range keys {
		object, ok := mapping.(map[string]any)
		require.True(t, ok, "expected object containing %s", key)
		mapping, ok = object[key]
		require.True(t, ok, "missing %s", key)
	}

	return mapping
}

func steps(t *testing.T, job any) []map[string]any {
	t.Helper()

	raw, ok := value(t, job, "steps").([]any)
	require.True(t, ok)

	result := make([]map[string]any, 0, len(raw))
	for _, step := range raw {
		mapping, valid := step.(map[string]any)
		require.True(t, valid)

		result = append(result, mapping)
	}

	return result
}
