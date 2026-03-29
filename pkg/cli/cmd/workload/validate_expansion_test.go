package workload_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandFluxSubstitutionsNoVars(t *testing.T) {
	t.Parallel()

	input := []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n")
	result := workload.ExportExpandFluxSubstitutions(context.Background(), input)
	assert.Equal(t, input, result)
}

func TestExpandFluxSubstitutionsDefaultSyntax(t *testing.T) {
	t.Parallel()

	input := []byte(
		"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: test\nspec:\n  replicas: ${count:=3}\n",
	)
	result := workload.ExportExpandFluxSubstitutions(context.Background(), input)
	assert.Contains(t, string(result), "replicas: 3")
	assert.NotContains(t, string(result), "${count")
}

func TestExpandFluxSubstitutionsDefaultHyphenSyntax(t *testing.T) {
	t.Parallel()

	input := []byte(
		"apiVersion: v1\nkind: Service\nmetadata:\n  name: ${svc_name:-my-service}\n",
	)
	result := workload.ExportExpandFluxSubstitutions(context.Background(), input)
	assert.Contains(t, string(result), "name: my-service")
}

func TestExpandFluxSubstitutionsBareVarStringField(t *testing.T) {
	t.Parallel()

	input := []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: ${my_name}\n")
	result := workload.ExportExpandFluxSubstitutions(context.Background(), input)
	resultStr := string(result)
	assert.Contains(t, resultStr, "name: placeholder")
	assert.NotContains(t, resultStr, "${my_name}")
}

func TestExpandFluxSubstitutionsBareVarIntegerField(t *testing.T) {
	t.Parallel()

	input := []byte(
		"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: test\nspec:\n  replicas: ${count}\n",
	)
	result := workload.ExportExpandFluxSubstitutions(context.Background(), input)
	resultStr := string(result)
	// Should substitute with a value (0 if schema available, placeholder otherwise)
	assert.NotContains(t, resultStr, "${count}")
}

func TestExpandFluxSubstitutionsMixedText(t *testing.T) {
	t.Parallel()

	input := []byte(
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\ndata:\n  host: whoami.${domain}\n",
	)
	result := workload.ExportExpandFluxSubstitutions(context.Background(), input)
	resultStr := string(result)
	assert.Contains(t, resultStr, "whoami.placeholder")
	assert.NotContains(t, resultStr, "${domain}")
}

func TestExpandFluxSubstitutionsMultipleVarsInOneLine(t *testing.T) {
	t.Parallel()

	input := []byte(
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\ndata:\n  url: https://${sub}.${domain}/path\n",
	)
	result := workload.ExportExpandFluxSubstitutions(context.Background(), input)
	resultStr := string(result)
	assert.Contains(t, resultStr, "https://placeholder.placeholder/path")
}

func TestExpandFluxSubstitutionsFallbackOnBadYAML(t *testing.T) {
	t.Parallel()

	input := []byte("not: valid: yaml: ${var}\n[broken")
	result := workload.ExportExpandFluxSubstitutions(context.Background(), input)
	resultStr := string(result)
	assert.NotContains(t, resultStr, "${var}")
}

func TestExpandFluxSubstitutionsMultiDoc(t *testing.T) {
	t.Parallel()

	input := []byte("apiVersion: v1\nkind: ConfigMap\n" +
		"metadata:\n  name: ${name1}\n---\n" +
		"apiVersion: v1\nkind: ConfigMap\n" +
		"metadata:\n  name: ${name2}\n",
	)
	result := workload.ExportExpandFluxSubstitutions(context.Background(), input)
	resultStr := string(result)
	assert.NotContains(t, resultStr, "${name1}")
	assert.NotContains(t, resultStr, "${name2}")
}

func TestExportGetSchemaTypeAtPath(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"properties": map[string]any{
			"spec": map[string]any{
				"properties": map[string]any{
					"replicas": map[string]any{
						"type": "integer",
					},
					"paused": map[string]any{
						"type": "boolean",
					},
					"hostnames": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"integer field", "/spec/replicas", "integer"},
		{"boolean field", "/spec/paused", "boolean"},
		{"array item", "/spec/hostnames/0", "string"},
		{"unknown field", "/spec/unknown", "string"},
		{"nonexistent path", "/nonexistent/path", "string"},
		{"empty path", "", "string"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := workload.ExportGetSchemaTypeAtPath(schema, testCase.path)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestExportGetSchemaTypeAtPathNilSchema(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "string", workload.ExportGetSchemaTypeAtPath(nil, "/spec/replicas"))
}

func TestExportSchemaURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		apiVersion string
		kind       string
		contains   string
	}{
		{"core resource", "v1", "Service", "kubernetes-json-schema"},
		{
			"apps group", "apps/v1", "Deployment",
			"deployment-apps-v1.json",
		},
		{
			"CRD", "gateway.networking.k8s.io/v1", "HTTPRoute",
			"httproute-gateway.networking.k8s.io-v1.json",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			urls := workload.ExportSchemaURLs(testCase.apiVersion, testCase.kind)
			require.NotEmpty(t, urls)
			assert.Contains(t, urls[0], testCase.contains)
		})
	}
}

func TestExportSplitAPIVersion(t *testing.T) {
	t.Parallel()

	group, version := workload.ExportSplitAPIVersion("apps/v1")
	assert.Equal(t, "apps", group)
	assert.Equal(t, "v1", version)

	group, version = workload.ExportSplitAPIVersion("v1")
	assert.Empty(t, group)
	assert.Equal(t, "v1", version)

	group, version = workload.ExportSplitAPIVersion("gateway.networking.k8s.io/v1")
	assert.Equal(t, "gateway.networking.k8s.io", group)
	assert.Equal(t, "v1", version)
}

func TestExportTypedPlaceholderValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		schemaType string
		expected   any
	}{
		{"string", "placeholder"},
		{"integer", 0},
		{"number", 0.0},
		{"boolean", true},
		{"unknown", "placeholder"},
		{"", "placeholder"},
	}

	for _, testCase := range tests {
		t.Run(testCase.schemaType, func(t *testing.T) {
			t.Parallel()

			result := workload.ExportTypedPlaceholderValue(testCase.schemaType)
			assert.Equal(t, testCase.expected, result)
		})
	}
}
