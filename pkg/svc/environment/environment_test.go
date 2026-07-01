package environment_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// prodOverlay mirrors the shape of a real platform clusters/<env>/kustomization.yaml
// overlay: a cluster-meta patch carrying the per-environment cluster_name/provider,
// a byte-identical replacements: block, and prose that incidentally contains the
// environment name as a substring ("local-config", "localhost").
const prodOverlay = `---
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
        namespace: flux-system
        annotations:
          # consumed locally only (not applied): config.kubernetes.io/local-config
          config.kubernetes.io/local-config: "true"
      data:
        cluster_name: prod
        provider: hetzner

replacements:
  - source:
      kind: ConfigMap
      name: cluster-meta
      fieldPath: data.provider
    targets:
      - select:
          kind: Kustomization
          namespace: flux-system
        fieldPaths:
          - spec.path
        options:
          delimiter: "/"
          index: 1
`

func TestDeriveRewrites_NoProviderOverride(t *testing.T) {
	t.Parallel()

	rewrites := environment.DeriveRewrites("prod", "staging", "", "hetzner")

	require.Len(t, rewrites, 2)
	assert.Equal(t, environment.Rewrite{
		Kind: environment.MetaFieldValue, Field: "cluster_name", Old: "prod", New: "staging",
	}, rewrites[0])
	assert.Equal(t, environment.Rewrite{
		Kind: environment.PathSegment, Old: "prod", New: "staging",
	}, rewrites[1])
}

func TestDeriveRewrites_ProviderOverride(t *testing.T) {
	t.Parallel()

	rewrites := environment.DeriveRewrites("prod", "staging", "aws", "hetzner")

	require.Len(t, rewrites, 3)
	assert.Equal(t, environment.Rewrite{
		Kind: environment.MetaFieldValue, Field: "provider", Old: "hetzner", New: "aws",
	}, rewrites[2])
}

func TestDeriveRewrites_ProviderUnchangedWhenEqual(t *testing.T) {
	t.Parallel()

	rewrites := environment.DeriveRewrites("prod", "staging", "hetzner", "hetzner")

	assert.Len(t, rewrites, 2, "an unchanged provider produces no provider rewrite")
}

func TestRewriteOverlayFile_RewritesClusterNameAndPreservesEverythingElse(t *testing.T) {
	t.Parallel()

	rewrites := environment.DeriveRewrites("prod", "staging", "", "hetzner")

	newPath, newContent, err := environment.RewriteOverlayFile(
		"k8s/clusters/prod/kustomization.yaml", prodOverlay, rewrites,
	)
	require.NoError(t, err)

	assert.Equal(t, "k8s/clusters/staging/kustomization.yaml", newPath)
	assert.Contains(t, newContent, "cluster_name: staging")
	assert.NotContains(t, newContent, "cluster_name: prod")

	// The provider was not overridden, so it is untouched.
	assert.Contains(t, newContent, "provider: hetzner")

	// The replacements: block and base wiring are byte-identical to the source.
	_, srcReplacements, srcFound := strings.Cut(prodOverlay, "replacements:")
	require.True(t, srcFound)

	_, dstReplacements, dstFound := strings.Cut(newContent, "replacements:")
	require.True(t, dstFound)
	assert.Equal(t, srcReplacements, dstReplacements)
}

func TestRewriteOverlayFile_ProviderOverride(t *testing.T) {
	t.Parallel()

	rewrites := environment.DeriveRewrites("prod", "staging", "aws", "hetzner")

	_, newContent, err := environment.RewriteOverlayFile(
		"k8s/clusters/prod/kustomization.yaml", prodOverlay, rewrites,
	)
	require.NoError(t, err)

	assert.Contains(t, newContent, "provider: aws")
	assert.NotContains(t, newContent, "provider: hetzner")
}

func TestRewriteOverlayFile_NoSpuriousSubstringRewrites(t *testing.T) {
	t.Parallel()

	const content = `metadata:
  annotations:
    config.kubernetes.io/local-config: "true"
  host: localhost
  note: a local-first cluster, reproduced locally
data:
  cluster_name: local
  unrelated_key: local
`

	rewrites := environment.DeriveRewrites("local", "edge", "", "docker")

	_, newContent, err := environment.RewriteOverlayFile("ksail.local.yaml", content, rewrites)
	require.NoError(t, err)

	// Only the cluster_name field value is rewritten.
	assert.Contains(t, newContent, "cluster_name: edge")
	// Substrings and unrelated keys with the same value are untouched.
	assert.Contains(t, newContent, "local-config")
	assert.Contains(t, newContent, "host: localhost")
	assert.Contains(t, newContent, "a local-first cluster, reproduced locally")
	assert.Contains(t, newContent, "unrelated_key: local")
}

func TestRewriteOverlayFile_KsailConfigFilenameAndKustomizationPath(t *testing.T) {
	t.Parallel()

	const content = `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  workload:
    sourceDirectory: k8s
    kustomizationFile: clusters/prod
`

	rewrites := environment.DeriveRewrites("prod", "staging", "", "hetzner")

	newPath, newContent, err := environment.RewriteOverlayFile("ksail.prod.yaml", content, rewrites)
	require.NoError(t, err)

	assert.Equal(t, "ksail.staging.yaml", newPath)
	assert.Contains(t, newContent, "kustomizationFile: clusters/staging")
}

func TestRewriteOverlayFile_ClustersPathBoundary(t *testing.T) {
	t.Parallel()

	const content = `paths:
  - clusters/prod
  - clusters/prod/bootstrap
  - clusters/production
  # documentary cross-reference to clusters/prod stays put
`

	rewrites := environment.DeriveRewrites("prod", "staging", "", "hetzner")

	_, newContent, err := environment.RewriteOverlayFile("kustomization.yaml", content, rewrites)
	require.NoError(t, err)

	assert.Contains(t, newContent, "clusters/staging\n")
	assert.Contains(t, newContent, "clusters/staging/bootstrap")
	// "clusters/production" must NOT be rewritten — "prod" is only a prefix there.
	assert.Contains(t, newContent, "clusters/production")
	// A documentary reference inside a comment is left unchanged.
	assert.Contains(t, newContent, "# documentary cross-reference to clusters/prod stays put")
}

func TestRewriteOverlayFile_PreservesQuotingAndTrailingComment(t *testing.T) {
	t.Parallel()

	const content = `data:
  cluster_name: "prod"   # the environment name
`

	rewrites := []environment.Rewrite{
		{Kind: environment.MetaFieldValue, Field: "cluster_name", Old: "prod", New: "staging"},
	}

	_, newContent, err := environment.RewriteOverlayFile("kustomization.yaml", content, rewrites)
	require.NoError(t, err)

	assert.Contains(t, newContent, `cluster_name: "staging"   # the environment name`)
}

func TestRewriteOverlayFile_InvalidRewrite(t *testing.T) {
	t.Parallel()

	cases := map[string]environment.Rewrite{
		"empty Old": {
			Kind: environment.MetaFieldValue, Field: "cluster_name", Old: "", New: "staging",
		},
		"empty New": {Kind: environment.PathSegment, Old: "prod", New: ""},
		"no Field": {
			Kind: environment.MetaFieldValue, Field: "", Old: "prod", New: "staging",
		},
		"unknown Kind": {
			Kind: environment.RewriteKind(99), Old: "prod", New: "staging",
		},
	}

	for name, rewrite := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, err := environment.RewriteOverlayFile(
				"kustomization.yaml", "data: {}\n", []environment.Rewrite{rewrite},
			)
			require.ErrorIs(t, err, environment.ErrInvalidRewrite)
		})
	}
}

func TestRewriteOverlayFile_FieldKeyMatchingIsExact(t *testing.T) {
	t.Parallel()

	const content = `data:
  cluster_name: prod
  cluster_named: prod
  cluster_name:prod
  cluster_name:
`

	rewrites := []environment.Rewrite{
		{Kind: environment.MetaFieldValue, Field: "cluster_name", Old: "prod", New: "staging"},
	}

	_, newContent, err := environment.RewriteOverlayFile("kustomization.yaml", content, rewrites)
	require.NoError(t, err)

	// Only the exact "cluster_name: prod" mapping line is rewritten.
	assert.Contains(t, newContent, "  cluster_name: staging\n")
	// A different key, a colon without a following space, and a valueless key are all left alone.
	assert.Contains(t, newContent, "  cluster_named: prod\n")
	assert.Contains(t, newContent, "  cluster_name:prod\n")
	assert.Contains(t, newContent, "  cluster_name:\n")
}
