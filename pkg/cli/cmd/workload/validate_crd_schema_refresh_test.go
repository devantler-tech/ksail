package workload_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestValidateRefreshCRDDerivedSchemas exercises discovery and final validation
// through the real command. The child isolates Go's proxy and root-CA caches;
// schema traffic terminates at the loopback TLS proxy, never a public registry.
//
//nolint:paralleltest // The child owns process-wide proxy and home/cache settings.
func TestValidateRefreshCRDDerivedSchemas(t *testing.T) {
	if os.Getenv("KSAIL_CRD_REFRESH_CHILD") == "1" {
		for _, layout := range []string{"raw", "rendered"} {
			t.Run(layout, func(t *testing.T) { runDerivedSchemaRefreshCases(t, layout) })
		}

		t.Run("helm-generated", runGeneratedSchemaRefreshCases)

		return
	}

	proxy, cert := localSchemaRegistryProxy(t)
	tempRoot := t.TempDir()

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	//nolint:gosec // Executes this same test binary with fixed test arguments.
	cmd := exec.CommandContext(
		ctx,
		os.Args[0],
		"-test.run=^TestValidateRefreshCRDDerivedSchemas$",
		"-test.v",
	)

	cmd.Env = append(os.Environ(),
		"KSAIL_CRD_REFRESH_CHILD=1", "HTTPS_PROXY="+proxy, "HTTP_PROXY="+proxy,
		"NO_PROXY=127.0.0.1,localhost", "SSL_CERT_FILE="+cert,
		"TMPDIR="+tempRoot, "TMP="+tempRoot, "TEMP="+tempRoot,
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "isolated CRD refresh command scenarios: %s", output)
}

func runDerivedSchemaRefreshCases(t *testing.T, layout string) {
	t.Helper()

	for _, test := range []struct {
		name       string
		resource   string
		includeCRD bool
		ignore     bool
		invalid    bool
	}{
		{name: "literal", resource: validWidget, includeCRD: true},
		{
			name: "typed-placeholder", includeCRD: true,
			resource: strings.Replace(validWidget, "size: 3", "size: ${SIZE}", 1),
		},
		{name: "invalid", resource: invalidWidget, includeCRD: true, ignore: true, invalid: true},
		{name: "missing-schema", resource: validWidget, ignore: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateDerivedSchemaCache(t)

			var dir string
			if layout == "rendered" {
				dir = writeRenderedCRDKustomization(t, test.resource)
			} else {
				dir = writeCRDTree(t, test.resource)
				require.NoError(t, os.WriteFile(filepath.Join(dir, "kustomization.yaml"),
					[]byte("resources:\n  - crd.yaml\n  - cr.yaml\n"), 0o600))
			}

			args := derivedSchemaValidationArgs(dir, test.includeCRD, test.ignore)

			output, err := runValidate(t, args...)
			if test.invalid {
				require.Error(t, err, "invalid CR must remain rejected: %s", output)
			} else {
				require.NoError(t, err, "ordinary derived-schema validation: %s", output)
			}

			output, err = runValidate(t, append(args, "--refresh-schemas")...)
			if test.invalid || !test.includeCRD {
				require.Error(
					t,
					err,
					"refresh must reject invalid or unavailable schemas: %s",
					output,
				)
			} else {
				require.NoError(t, err, "refresh must use the discovered CRD schema: %s", output)
			}
		})
	}
}

func runGeneratedSchemaRefreshCases(t *testing.T) {
	t.Helper()

	for _, test := range []struct {
		name       string
		resource   string
		includeCRD bool
		ignore     bool
		invalid    bool
	}{
		{name: "valid", resource: validWidget, includeCRD: true},
		{
			name: "literal-helm-placeholder", includeCRD: true, invalid: true,
			resource: strings.Replace(validWidget, "size: 3", "size: ${SIZE}", 1),
		},
		{name: "missing-schema", resource: validWidget, ignore: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateDerivedSchemaCache(t)
			dir := writeGeneratedWidgetKustomization(t, test.resource)
			args := derivedSchemaValidationArgs(dir, test.includeCRD, test.ignore)

			output, err := runValidate(t, args...)
			if test.invalid {
				require.Error(
					t,
					err,
					"Flux must not type placeholders produced later by Helm: %s",
					output,
				)
			} else {
				require.NoError(t, err, "ordinary rendered-resource validation: %s", output)
			}

			output, err = runValidate(t, append(args, "--refresh-schemas")...)
			if test.invalid || !test.includeCRD {
				require.Error(
					t,
					err,
					"refresh must check every final rendered resource: %s",
					output,
				)
			} else {
				require.NoError(t, err, "refresh must validate the generated resource: %s", output)
			}
		})
	}
}

func isolateDerivedSchemaCache(t *testing.T) {
	t.Helper()

	home := t.TempDir()
	for _, key := range []string{"HOME", "USERPROFILE", "XDG_CACHE_HOME", "LOCALAPPDATA"} {
		t.Setenv(key, home)
	}
}

func derivedSchemaValidationArgs(dir string, includeCRD, ignore bool) []string {
	args := []string{
		dir,
		"--skip-kinds",
		"CustomResourceDefinition,HelmRelease,OCIRepository",
		"--strict",
	}
	if includeCRD {
		args = append(args, "--include-crd-schemas")
	}

	if !ignore {
		args = append(args, "--ignore-missing-schemas=false")
	}

	return args
}

func writeGeneratedWidgetKustomization(t *testing.T, resource string) string {
	t.Helper()
	chart := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(chart, "templates"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(chart, "Chart.yaml"),
		[]byte("apiVersion: v2\nname: generated-widget\nversion: 0.1.0\n"), 0o600))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(chart, "templates", "widget.yaml"), []byte(resource), 0o600),
	)
	dir := writeHelmReleaseKustomization(t, chart)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kustomization.yaml"),
		[]byte("resources:\n  - ocirepository.yaml\n  - helmrelease.yaml\n  - crd.yaml\n"), 0o600))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(dir, "crd.yaml"), []byte(widgetCRDManifest), 0o600),
	)

	return dir
}
