package csrapprover_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/talos/csrapprover"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestManifest(t *testing.T) {
	t.Parallel()

	manifest := csrapprover.Manifest()
	require.NotEmpty(t, manifest)

	// Verify the manifest contains expected resources
	assert.Contains(t, manifest, "kind: Namespace")
	assert.Contains(t, manifest, "kind: ServiceAccount")
	assert.Contains(t, manifest, "kind: ClusterRole")
	assert.Contains(t, manifest, "kind: ClusterRoleBinding")
	assert.Contains(t, manifest, "kind: Deployment")
	assert.Contains(t, manifest, "kubelet-serving-cert-approver")

	// Verify the manifest contains the expected image reference
	assert.Contains(t, manifest, "ghcr.io/alex1989hu/kubelet-serving-cert-approver:main",
		"manifest should use upstream :main image tag")
}

func TestManifest_MultipleDocuments(t *testing.T) {
	t.Parallel()

	manifest := csrapprover.Manifest()

	// Verify multi-document YAML structure
	docs := strings.Split(manifest, "---")
	assert.GreaterOrEqual(t, len(docs), 6,
		"manifest should contain multiple YAML documents")

	// Verify each non-empty document is parseable YAML
	for docIdx, doc := range docs {
		trimmed := strings.TrimSpace(doc)
		if trimmed == "" {
			continue
		}

		var parsed map[string]any

		err := yaml.Unmarshal([]byte(trimmed), &parsed)
		assert.NoError(t, err, "document %d should be valid YAML", docIdx)
	}
}
