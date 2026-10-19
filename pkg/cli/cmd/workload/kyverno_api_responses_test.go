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

func TestLoadKyvernoAPICallResponsesUsesContainingDirectoryForFileRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "workload.yaml")
	fixture := filepath.Join(root, "api-responses.yaml")

	require.NoError(t, os.WriteFile(target, []byte("apiVersion: v1\nkind: ConfigMap\n"), 0o600))
	require.NoError(t, os.WriteFile(fixture, []byte(`
apiVersion: ksail.dev/v1alpha1
kind: KyvernoAPICallResponses
responses:
  - method: GET
    urlPath: /api/v1/namespaces/default
    body: {metadata: {name: default}}
`), 0o600))

	responses, err := loadKyvernoAPICallResponses(target, "api-responses.yaml")
	require.NoError(t, err)
	require.Contains(t, responses, kyvernopolicy.APICallRequest{
		Method: "GET", URLPath: "/api/v1/namespaces/default",
	})
}

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
