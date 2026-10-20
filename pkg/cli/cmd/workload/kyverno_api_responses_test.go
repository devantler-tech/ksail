package workload //nolint:testpackage // Exercises the private fixture parser and safe-path boundary.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/kyvernopolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadKyvernoAPICallResponses verifies safe fixture loading and method normalization.
func TestLoadKyvernoAPICallResponses(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fixture := filepath.Join(root, "api-responses.yaml")
	require.NoError(t, os.WriteFile(fixture, []byte(`
apiVersion: ksail.dev/v1alpha1
kind: KyvernoAPICallResponses
responses:
  - method: get
    urlPath: /api/v1/namespaces/arc-runners
    body:
      metadata:
        annotations:
          state: baseline
`), 0o600))

	responses, err := loadKyvernoAPICallResponses(root, "api-responses.yaml")
	require.NoError(t, err)

	body, found := responses[kyvernopolicy.APICallRequest{
		Method: "GET", URLPath: "/api/v1/namespaces/arc-runners",
	}]
	require.True(t, found)
	assert.JSONEq(t, `{"metadata":{"annotations":{"state":"baseline"}}}`, string(body))
}

// TestKyvernoAPICallFixtureAppliesOnlyToKyvernoDirectoryRuns pins when a fixture is consumed.
func TestKyvernoAPICallFixtureAppliesOnlyToKyvernoDirectoryRuns(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	file := filepath.Join(root, "workload.yaml")
	require.NoError(t, os.WriteFile(file, []byte("apiVersion: v1\nkind: ConfigMap\n"), 0o600))

	applies, err := kyvernoAPICallFixtureApplies(true, root)
	require.NoError(t, err)
	assert.True(t, applies, "a directory run with Kyverno policies reads the fixture")

	applies, err = kyvernoAPICallFixtureApplies(false, root)
	require.NoError(t, err)
	assert.False(t, applies, "with Kyverno policies off nothing reads the fixture")

	applies, err = kyvernoAPICallFixtureApplies(true, file)
	require.NoError(t, err)
	assert.False(t, applies, "a single-file run has no Kyverno pass")

	_, err = kyvernoAPICallFixtureApplies(true, filepath.Join(root, "missing"))
	require.Error(t, err)
}

// TestExcludeFixtureFileMatchesASymlinkedSpelling excludes every canonical spelling of the fixture.
func TestExcludeFixtureFileMatchesASymlinkedSpelling(t *testing.T) {
	t.Parallel()

	realDir := t.TempDir()
	fixture := filepath.Join(realDir, "kyverno-api-responses.yaml")
	require.NoError(t, os.WriteFile(fixture, []byte(`
apiVersion: ksail.dev/v1alpha1
kind: KyvernoAPICallResponses
responses: []
`), 0o600))

	link := filepath.Join(t.TempDir(), "linked")
	require.NoError(t, os.Symlink(realDir, link))

	loaded, err := loadKyvernoAPICallFixture(link, "kyverno-api-responses.yaml")
	require.NoError(t, err)

	files := []string{filepath.Join(link, "kyverno-api-responses.yaml"), fixture}
	assert.Empty(t, excludeFixtureFile(files, loaded.path),
		"both spellings name the fixture, so neither is validated as a workload")
}

// TestLoadKyvernoAPICallResponsesRejectsDuplicateRequest rejects ambiguous normalized requests.
func TestLoadKyvernoAPICallResponsesRejectsDuplicateRequest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fixture := filepath.Join(root, "duplicates.yaml")
	require.NoError(t, os.WriteFile(fixture, []byte(`
apiVersion: ksail.dev/v1alpha1
kind: KyvernoAPICallResponses
responses:
  - method: GET
    urlPath: /api/v1/namespaces/arc-runners
    body: {metadata: {name: first}}
  - method: get
    urlPath: /api/v1/namespaces/arc-runners
    body: {metadata: {name: second}}
`), 0o600))

	_, err := loadKyvernoAPICallResponses(root, fixture)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate response for GET /api/v1/namespaces/arc-runners")
}

// TestLoadKyvernoAPICallResponsesRejectsPathEscape keeps fixture reads inside the project root.
func TestLoadKyvernoAPICallResponsesRejectsPathEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	require.NoError(t, os.WriteFile(outside, []byte(`
apiVersion: ksail.dev/v1alpha1
kind: KyvernoAPICallResponses
responses: []
`), 0o600))

	_, err := loadKyvernoAPICallResponses(root, outside)
	require.ErrorIs(t, err, fsutil.ErrPathOutsideBase)
}

// TestLoadKyvernoAPICallResponsesRejectsUnsupportedShape rejects invalid methods, paths and bodies.
func TestLoadKyvernoAPICallResponsesRejectsUnsupportedShape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fixture := filepath.Join(root, "invalid.yaml")
	require.NoError(t, os.WriteFile(fixture, []byte(`
apiVersion: ksail.dev/v1alpha1
kind: KyvernoAPICallResponses
responses:
  - method: DELETE
    urlPath: api/v1/namespaces/default
    body: null
`), 0o600))

	_, err := loadKyvernoAPICallResponses(root, fixture)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "response 1")
}

// TestLoadKyvernoAPICallFixtureExcludesItsFileFromLooseValidation separates fixtures from workloads.
func TestLoadKyvernoAPICallFixtureExcludesItsFileFromLooseValidation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fixtureDir := filepath.Join(root, "validation")
	require.NoError(t, os.MkdirAll(fixtureDir, 0o750))

	fixture := filepath.Join(fixtureDir, "kyverno-api-responses.yaml")
	require.NoError(t, os.WriteFile(fixture, []byte(`
apiVersion: ksail.dev/v1alpha1
kind: KyvernoAPICallResponses
responses: []
`), 0o600))

	workload := filepath.Join(root, "configmap.yaml")
	require.NoError(t, os.WriteFile(workload, []byte("apiVersion: v1\nkind: ConfigMap\n"), 0o600))

	loaded, err := loadKyvernoAPICallFixture(root, "validation/kyverno-api-responses.yaml")
	require.NoError(t, err)
	assert.NotNil(t, loaded.responses, "an empty fixture is still a configured fixture")

	targets, err := discoverValidationTargets(root)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{fixture, workload}, targets.yamlFiles,
		"the directory walk discovers the fixture as a YAML file")

	assert.Equal(t, []string{workload}, excludeFixtureFile(targets.yamlFiles, loaded.path),
		"the fixture is a KSail input, not a workload to validate")
}

// TestLoadKyvernoAPICallFixtureWithoutPathExcludesNothing preserves validation without a fixture.
func TestLoadKyvernoAPICallFixtureWithoutPathExcludesNothing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	loaded, err := loadKyvernoAPICallFixture(root, "")
	require.NoError(t, err)
	assert.Nil(t, loaded.responses)
	assert.Empty(t, loaded.path)

	files := []string{filepath.Join(root, "a.yaml")}
	assert.Equal(t, files, excludeFixtureFile(files, loaded.path))
}

// A response body reaches Kyverno as written: an integer beyond float64's exact
// range keeps every digit.
func TestLoadKyvernoAPICallResponsesKeepsLargeIntegersExact(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fixture := filepath.Join(root, "api-responses.yaml")
	require.NoError(t, os.WriteFile(fixture, []byte(`
apiVersion: ksail.dev/v1alpha1
kind: KyvernoAPICallResponses
responses:
  - urlPath: /api/v1/namespaces/default
    body:
      metadata:
        generation: 9007199254740993
`), 0o600))

	responses, err := loadKyvernoAPICallResponses(root, "api-responses.yaml")
	require.NoError(t, err)

	body := responses[kyvernopolicy.APICallRequest{
		Method: "GET", URLPath: "/api/v1/namespaces/default",
	}]
	assert.Contains(t, string(body), "9007199254740993")
}

// TestLoadKyvernoAPICallResponsesRejectsNullBody requires a usable response rather than JSON null.
func TestLoadKyvernoAPICallResponsesRejectsNullBody(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fixture := filepath.Join(root, "api-responses.yaml")
	require.NoError(t, os.WriteFile(fixture, []byte(`
apiVersion: ksail.dev/v1alpha1
kind: KyvernoAPICallResponses
responses:
  - urlPath: /api/v1/namespaces/default
    body: null
  - urlPath: /api/v1/namespaces/other
`), 0o600))

	_, err := loadKyvernoAPICallResponses(root, "api-responses.yaml")
	require.ErrorIs(t, err, errInvalidKyvernoAPICallResponse)
}
