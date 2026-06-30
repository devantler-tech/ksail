package environment_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// overlayKustomization mirrors a real platform clusters/<env>/kustomization.yaml:
// a cluster-meta patch carrying the per-environment cluster_name/provider, a
// clusters/<env> path reference, and prose that incidentally contains the
// environment name as a substring ("local-config").
const overlayKustomization = `---
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../base
patches:
  - patch: |
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: cluster-meta
        annotations:
          config.kubernetes.io/local-config: "true"
      data:
        cluster_name: prod
        provider: hetzner
components:
  - clusters/prod/components
`

const bootstrapKustomization = `---
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../../providers/hetzner/bootstrap
`

// encryptedSecret deliberately contains "prod" and "clusters/prod" text that the
// structured rewrites WOULD touch in a plaintext file, to prove an *.enc.yaml file
// is cloned byte-for-byte regardless.
//
//nolint:gosec // G101: a test fixture simulating SOPS ciphertext, not a real credential.
const encryptedSecret = `cluster_name: prod
data: ENC[AES256_GCM,data:clusters/prod,type:str]
sops:
    age: []
`

// writeOverlay materialises a prod overlay tree under repoRoot and returns it.
func writeOverlay(t *testing.T, repoRoot string) {
	t.Helper()

	files := map[string]string{
		"k8s/clusters/prod/kustomization.yaml":                          overlayKustomization,
		"k8s/clusters/prod/bootstrap/kustomization.yaml":                bootstrapKustomization,
		"k8s/clusters/prod/bootstrap/variables-cluster-secret.enc.yaml": encryptedSecret,
	}

	for rel, content := range files {
		abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o750))
		require.NoError(t, os.WriteFile(abs, []byte(content), 0o600))
	}
}

func readClone(t *testing.T, repoRoot, rel string) string {
	t.Helper()

	//nolint:gosec // G304: reads a file just written under the test's own t.TempDir().
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
	require.NoError(t, err)

	return string(data)
}

func TestCloneOverlay_ClonesTreeWithStructuredRewrites(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeOverlay(t, repoRoot)

	rewrites := environment.DeriveRewrites("prod", "staging", "", "hetzner")

	written, err := environment.CloneOverlay(repoRoot, "k8s/clusters/prod", rewrites, false)
	require.NoError(t, err)

	// Walk order is lexical and deterministic: the bootstrap/ subdirectory is
	// visited before the sibling kustomization.yaml file.
	assert.Equal(t, []string{
		"k8s/clusters/staging/bootstrap/kustomization.yaml",
		"k8s/clusters/staging/bootstrap/variables-cluster-secret.enc.yaml",
		"k8s/clusters/staging/kustomization.yaml",
	}, written)

	top := readClone(t, repoRoot, "k8s/clusters/staging/kustomization.yaml")
	assert.Contains(t, top, "cluster_name: staging")
	assert.NotContains(t, top, "cluster_name: prod")
	// The provider was not overridden, so it is untouched.
	assert.Contains(t, top, "provider: hetzner")
	// The clusters/<env> content reference is repointed.
	assert.Contains(t, top, "clusters/staging/components")
	// An unrelated substring of the environment name is left alone.
	assert.Contains(t, top, "config.kubernetes.io/local-config")
}

func TestCloneOverlay_CopiesSopsEncryptedFilesVerbatim(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeOverlay(t, repoRoot)

	rewrites := environment.DeriveRewrites("prod", "staging", "", "hetzner")

	_, err := environment.CloneOverlay(repoRoot, "k8s/clusters/prod", rewrites, false)
	require.NoError(t, err)

	// The encrypted file's path is repointed to the destination environment...
	enc := readClone(t, repoRoot,
		"k8s/clusters/staging/bootstrap/variables-cluster-secret.enc.yaml")
	// ...but its ciphertext is byte-for-byte identical, including the "prod" and
	// "clusters/prod" text a plaintext rewrite would have changed.
	assert.Equal(t, encryptedSecret, enc)
}

func TestCloneOverlay_ProviderOverride(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeOverlay(t, repoRoot)

	rewrites := environment.DeriveRewrites("prod", "staging", "aws", "hetzner")

	_, err := environment.CloneOverlay(repoRoot, "k8s/clusters/prod", rewrites, false)
	require.NoError(t, err)

	top := readClone(t, repoRoot, "k8s/clusters/staging/kustomization.yaml")
	assert.Contains(t, top, "provider: aws")
	assert.NotContains(t, top, "provider: hetzner")
}

func TestCloneOverlay_SkipsExistingWithoutForce(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeOverlay(t, repoRoot)

	// Pre-create a destination file with sentinel content.
	dest := filepath.Join(repoRoot, "k8s", "clusters", "staging", "kustomization.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o750))
	require.NoError(t, os.WriteFile(dest, []byte("SENTINEL\n"), 0o600))

	rewrites := environment.DeriveRewrites("prod", "staging", "", "hetzner")

	written, err := environment.CloneOverlay(repoRoot, "k8s/clusters/prod", rewrites, false)
	require.NoError(t, err)

	// Without force the existing destination file is preserved.
	assert.Equal(t, "SENTINEL\n",
		readClone(t, repoRoot, "k8s/clusters/staging/kustomization.yaml"))
	// The skipped file is excluded from the returned "paths written" list.
	assert.NotContains(t, written, "k8s/clusters/staging/kustomization.yaml")
	assert.Contains(t, written, "k8s/clusters/staging/bootstrap/kustomization.yaml")
}

func TestCloneOverlay_OverwritesExistingWithForce(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeOverlay(t, repoRoot)

	dest := filepath.Join(repoRoot, "k8s", "clusters", "staging", "kustomization.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o750))
	require.NoError(t, os.WriteFile(dest, []byte("SENTINEL\n"), 0o600))

	rewrites := environment.DeriveRewrites("prod", "staging", "", "hetzner")

	_, err := environment.CloneOverlay(repoRoot, "k8s/clusters/prod", rewrites, true)
	require.NoError(t, err)

	// With force the destination is overwritten with the cloned content.
	top := readClone(t, repoRoot, "k8s/clusters/staging/kustomization.yaml")
	assert.NotContains(t, top, "SENTINEL")
	assert.Contains(t, top, "cluster_name: staging")
}

func TestCloneOverlay_MissingSourceOverlay(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	rewrites := environment.DeriveRewrites("prod", "staging", "", "hetzner")

	_, err := environment.CloneOverlay(repoRoot, "k8s/clusters/absent", rewrites, false)
	require.ErrorIs(t, err, environment.ErrSourceOverlayMissing)
}

func TestCloneOverlay_SourceIsFileNotDirectory(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	cfgPath := filepath.Join(repoRoot, "ksail.prod.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("x\n"), 0o600))

	rewrites := environment.DeriveRewrites("prod", "staging", "", "hetzner")

	// A path that exists but is a file, not a directory, is rejected.
	_, err := environment.CloneOverlay(repoRoot, "ksail.prod.yaml", rewrites, false)
	require.ErrorIs(t, err, environment.ErrSourceOverlayMissing)
}

func TestCloneOverlay_SourceTraversalRejected(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	rewrites := environment.DeriveRewrites("prod", "staging", "", "hetzner")

	// A srcRelDir with ".." segments that escapes repoRoot must be rejected even
	// if it resolves to a real directory outside the repo.
	_, err := environment.CloneOverlay(repoRoot, "../../etc", rewrites, false)
	require.ErrorIs(t, err, environment.ErrSourceOverlayMissing)
}

func TestCloneOverlay_InvalidRewritePropagates(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeOverlay(t, repoRoot)

	// A malformed rewrite (empty New) must surface ErrInvalidRewrite from the
	// per-file rewrite rather than producing a partial clone.
	bad := []environment.Rewrite{{Kind: environment.PathSegment, Old: "prod", New: ""}}

	_, err := environment.CloneOverlay(repoRoot, "k8s/clusters/prod", bad, false)
	require.ErrorIs(t, err, environment.ErrInvalidRewrite)
}
