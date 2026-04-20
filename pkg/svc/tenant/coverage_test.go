package tenant_test

import (
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		resources []string
		relPath   string
		expected  []string
	}{
		{
			name:      "adds to empty list",
			resources: nil,
			relPath:   "my-tenant",
			expected:  []string{"my-tenant"},
		},
		{
			name:      "adds to existing list",
			resources: []string{"tenant-a"},
			relPath:   "tenant-b",
			expected:  []string{"tenant-a", "tenant-b"},
		},
		{
			name:      "idempotent when already present",
			resources: []string{"tenant-a", "tenant-b"},
			relPath:   "tenant-a",
			expected:  []string{"tenant-a", "tenant-b"},
		},
		{
			name:      "adds with path separators",
			resources: []string{"tenants/a"},
			relPath:   "tenants/b",
			expected:  []string{"tenants/a", "tenants/b"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := tenant.ExportAddResource(testCase.resources, testCase.relPath)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestRemoveResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		resources []string
		relPath   string
		expected  []string
	}{
		{
			name:      "removes existing resource",
			resources: []string{"tenant-a", "tenant-b", "tenant-c"},
			relPath:   "tenant-b",
			expected:  []string{"tenant-a", "tenant-c"},
		},
		{
			name:      "no-op when not present",
			resources: []string{"tenant-a", "tenant-b"},
			relPath:   "tenant-c",
			expected:  []string{"tenant-a", "tenant-b"},
		},
		{
			name:      "empty list returns empty",
			resources: nil,
			relPath:   "tenant-a",
			expected:  []string{},
		},
		{
			name:      "removes last item",
			resources: []string{"tenant-a"},
			relPath:   "tenant-a",
			expected:  []string{},
		},
		{
			name:      "removes with path separators",
			resources: []string{"tenants/a", "tenants/b"},
			relPath:   "tenants/a",
			expected:  []string{"tenants/b"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := tenant.ExportRemoveResource(testCase.resources, testCase.relPath)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestHasDuplicateNamespaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		namespaces []string
		expected   bool
	}{
		{
			name:       "no duplicates",
			namespaces: []string{"ns-a", "ns-b", "ns-c"},
			expected:   false,
		},
		{
			name:       "has duplicates",
			namespaces: []string{"ns-a", "ns-b", "ns-a"},
			expected:   true,
		},
		{
			name:       "empty list",
			namespaces: nil,
			expected:   false,
		},
		{
			name:       "single item",
			namespaces: []string{"ns-a"},
			expected:   false,
		},
		{
			name:       "all duplicates",
			namespaces: []string{"ns-a", "ns-a", "ns-a"},
			expected:   true,
		},
		{
			name:       "consecutive duplicates",
			namespaces: []string{"ns-a", "ns-a"},
			expected:   true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := tenant.ExportHasDuplicateNamespaces(testCase.namespaces)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestIsValidType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		typ      tenant.Type
		expected bool
	}{
		{
			name:     "flux is valid",
			typ:      tenant.TypeFlux,
			expected: true,
		},
		{
			name:     "argocd is valid",
			typ:      tenant.TypeArgoCD,
			expected: true,
		},
		{
			name:     "kubectl is valid",
			typ:      tenant.TypeKubectl,
			expected: true,
		},
		{
			name:     "empty is invalid",
			typ:      "",
			expected: false,
		},
		{
			name:     "random string is invalid",
			typ:      "helm",
			expected: false,
		},
		{
			name:     "case sensitive - FLUX is invalid",
			typ:      "FLUX",
			expected: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := tenant.ExportIsValidType(testCase.typ)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestSafeRelPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		base      string
		target    string
		expected  string
		expectErr bool
	}{
		{
			name:     "simple relative path",
			base:     "/repo",
			target:   "/repo/tenants/my-tenant",
			expected: "tenants/my-tenant",
		},
		{
			name:     "same directory",
			base:     "/repo",
			target:   "/repo/file.yaml",
			expected: "file.yaml",
		},
		{
			name:      "escapes repo root",
			base:      "/repo/tenants",
			target:    "/other/dir/file.yaml",
			expectErr: true,
		},
		{
			name:      "parent directory escape",
			base:      "/repo/tenants",
			target:    "/repo/file.yaml",
			expectErr: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := tenant.ExportSafeRelPath(testCase.base, testCase.target)
			if testCase.expectErr {
				require.Error(t, err)
				require.ErrorIs(t, err, tenant.ErrOutsideRepoRoot)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.expected, result)
			}
		})
	}
}

func TestGetResources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      map[string]any
		expected []string
	}{
		{
			name:     "extracts string resources",
			raw:      map[string]any{"resources": []any{"a", "b", "c"}},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "nil when resources key missing",
			raw:      map[string]any{},
			expected: nil,
		},
		{
			name:     "nil when resources is not a slice",
			raw:      map[string]any{"resources": "not-a-slice"},
			expected: nil,
		},
		{
			name:     "nil map",
			raw:      nil,
			expected: nil,
		},
		{
			name:     "filters non-string items",
			raw:      map[string]any{"resources": []any{"a", 42, "b"}},
			expected: []string{"a", "b"},
		},
		{
			name:     "empty slice",
			raw:      map[string]any{"resources": []any{}},
			expected: []string{},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := tenant.ExportGetResources(testCase.raw)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestCollectDeliveryFiles_WithoutKustomization(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	tenantDir := repoRoot + "/tenants"
	require.NoError(t, os.MkdirAll(tenantDir+"/my-tenant", 0o750))
	require.NoError(
		t,
		os.WriteFile(tenantDir+"/my-tenant/namespace.yaml", []byte("kind: Namespace"), 0o600),
	)

	files, err := tenant.CollectDeliveryFiles("my-tenant", tenantDir, "", repoRoot)
	require.NoError(t, err)

	// Only tenant files, no kustomization
	require.Len(t, files, 1)
	require.Contains(t, files, "tenants/my-tenant/namespace.yaml")
}
