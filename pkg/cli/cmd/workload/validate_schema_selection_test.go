package workload_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/stretchr/testify/require"
	"github.com/yannh/kubeconform/pkg/cache"
)

const selectionCatalogue = "https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/" +
	"{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json"

const selectionSchemaURL = "https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/" +
	"helm.toolkit.fluxcd.io/helmrelease_v2.json"

const selectionSchemaContentDigest = "c853417c55e892ae5ea7041629af046477fd70a07485c92fa656fd6dfdb65d38"

const selectionHelmRelease = `apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: cached-release
spec:
  interval: 5m
  maxHistory: ${HISTORY}
  chartRef:
    kind: OCIRepository
    name: podinfo
`

// TestSelectedSchemaFixtureProvenance permits formatting while retaining every
// upstream JSON token, including validation rules and whitespace inside strings.
func TestSelectedSchemaFixtureProvenance(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/schema-selection/helmrelease-v2.json")
	require.NoError(t, err)
	digest, err := schemaTokenDigest(data)
	require.NoError(t, err)
	require.Equal(t, selectionSchemaContentDigest, digest)

	var indented bytes.Buffer

	require.NoError(t, json.Indent(&indented, data, "", "    "))
	require.NotEqual(t, data, indented.Bytes())
	formattedDigest, err := schemaTokenDigest(indented.Bytes())
	require.NoError(t, err)
	require.Equal(t, digest, formattedDigest)

	changed := bytes.Replace(data, []byte(`"OCIRepository"`), []byte(`"UnsupportedSource"`), 1)
	require.NotEqual(t, data, changed)
	changedDigest, err := schemaTokenDigest(changed)
	require.NoError(t, err)
	require.NotEqual(t, selectionSchemaContentDigest, changedDigest)

	_, err = schemaTokenDigest(append(data, '\n', '{', '}'))
	require.Error(t, err, "additional JSON values must not disappear during normalization")
}

// TestValidateSelectedSchemaLocations uses the actual catalogue schema and real
// command. Real HTTP 404 misses never insert fictional built-in cache entries.
// Fresh subprocesses isolate proxy, root certificates and config state.
//
//nolint:paralleltest // Subprocesses own isolated environments and ordered cache phases.
func TestValidateSelectedSchemaLocations(t *testing.T) {
	if phase := os.Getenv("KSAIL_SCHEMA_SELECTION_PHASE"); phase != "" {
		runSelectedSchemaChild(t, phase)

		return
	}

	var offline atomic.Bool

	var requests atomic.Int64

	proxy, certificate := selectedSchemaRegistry(t, &offline, &requests)
	home, manifests := t.TempDir(), t.TempDir()
	require.NoError(
		t,
		os.WriteFile(filepath.Join(manifests, "release.yaml"), []byte(selectionHelmRelease), 0o600),
	)
	run := selectedSchemaPhaseRunner(t, home, manifests, proxy, certificate)
	run("warm")
	require.Positive(t, requests.Load(), "warming must use real registry responses")
	offline.Store(true)
	requests.Store(0)
	run("default-control")
	require.Positive(t, requests.Load(), "the absent earlier registry must reproduce the failure")

	for _, phase := range []string{
		"only", "invalid-enum", "invalid-type", "local", "typing-choice",
		"missing-local", "empty", "blank", "invalid-mode", "empty-mode",
	} {
		requests.Store(0)
		run(phase)
		require.Zero(
			t,
			requests.Load(),
			"complete cached/local selection makes no request: %s",
			phase,
		)
	}

	requests.Store(0)
	run("missing-cache")
	require.Positive(t, requests.Load(), "an incomplete cache can attempt transport and must fail")
	offline.Store(false)
	requests.Store(0)
	run("untrusted-tls")
	require.Zero(t, requests.Load(), "untrusted TLS fails before accepting schema bytes")
}

// selectedSchemaRegistry serves the pinned schema through a private TLS endpoint.
func selectedSchemaRegistry(
	t *testing.T,
	offline *atomic.Bool,
	requests *atomic.Int64,
) (string, string) {
	t.Helper()

	schema, err := os.ReadFile("testdata/schema-selection/helmrelease-v2.json")
	require.NoError(t, err)

	handler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)

		if offline.Load() {
			http.Error(response, "isolated registry unavailable", http.StatusServiceUnavailable)

			return
		}

		if request.URL.Path == "/datreeio/CRDs-catalog/main/helm.toolkit.fluxcd.io/helmrelease_v2.json" {
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write(schema)

			return
		}

		http.NotFound(response, request)
	})
	registry, certificate := localSchemaRegistryWithHandler(t, handler)
	forwarder := &schemaRegistryProxy{target: registry.Listener.Addr().String()}
	proxy := httptest.NewServer(forwarder)
	t.Cleanup(proxy.Close)
	t.Cleanup(forwarder.closeConnections)

	return proxy.URL, certificate
}

// selectedSchemaPhaseRunner retains one cache across independent processes and
// gives incomplete-cache and TLS controls fresh homes without changing transport.
func selectedSchemaPhaseRunner(
	t *testing.T,
	home, manifests, proxy, certificate string,
) func(string) {
	t.Helper()

	return func(phase string) {
		t.Helper()

		ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
		defer cancel()

		phaseHome := home
		if phase == "untrusted-tls" || phase == "missing-cache" {
			phaseHome = t.TempDir()
		}

		trustedCertificate := certificate
		if phase == "untrusted-tls" {
			trustedCertificate = ""
		}

		//nolint:gosec // Executes this same test binary with fixed test arguments.
		cmd := exec.CommandContext(
			ctx,
			os.Args[0],
			"-test.run=^TestValidateSelectedSchemaLocations$",
			"-test.v",
		)
		cmd.Dir = manifests

		cmd.Env = append(
			os.Environ(),
			"KSAIL_SCHEMA_SELECTION_PHASE="+phase,
			"KSAIL_SCHEMA_SELECTION_HOME="+phaseHome,
			"HOME="+phaseHome,
			"USERPROFILE="+phaseHome,
			"XDG_CACHE_HOME="+phaseHome,
			"LOCALAPPDATA="+phaseHome,
			"HTTPS_PROXY="+proxy,
			"HTTP_PROXY="+proxy,
			"NO_PROXY=127.0.0.1,localhost",
			"SSL_CERT_FILE="+trustedCertificate,
		)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "phase %s: %s", phase, output)
	}
}

// runSelectedSchemaChild rebinds the shared fixture after TestMain's isolated
// home, then runs the public handler or the explicitly selected built CLI.
func runSelectedSchemaChild(t *testing.T, phase string) {
	t.Helper()

	for _, key := range []string{"HOME", "USERPROFILE", "XDG_CACHE_HOME", "LOCALAPPDATA"} {
		t.Setenv(key, os.Getenv("KSAIL_SCHEMA_SELECTION_HOME"))
	}

	manifest, location := selectedSchemaInput(t, phase)
	require.NoError(t, os.WriteFile("release.yaml", []byte(manifest), 0o600))
	output, err := runSelectedValidation(t, selectedSchemaArguments(phase, location))
	assertSelectedSchemaResult(t, phase, output, err)
}

// selectedSchemaInput keeps malformed manifests independent from unavailable
// transport. TLS and missing-cache controls use concrete, otherwise valid data.
func selectedSchemaInput(t *testing.T, phase string) (string, string) {
	t.Helper()

	switch phase {
	case "warm", "untrusted-tls", "missing-cache":
		return strings.ReplaceAll(selectionHelmRelease, "${HISTORY}", "3"), selectionCatalogue
	case "invalid-enum":
		return strings.ReplaceAll(
			selectionHelmRelease,
			"kind: OCIRepository",
			"kind: NotAChartSource",
		), selectionCatalogue
	case "invalid-type":
		return strings.ReplaceAll(selectionHelmRelease, "${HISTORY}", "true"), selectionCatalogue
	case "local", "missing-local":
		location := filepath.Join(t.TempDir(), "helmrelease_v2.json")
		require.NoError(t, os.WriteFile(location, selectedCachedSchema(t), 0o600))

		if phase == "missing-local" {
			location += ".missing.json"
		}

		return selectionHelmRelease, location
	case "typing-choice":
		return selectedTypingFixture(t)
	case "blank":
		return selectionHelmRelease, " "
	default:
		return selectionHelmRelease, selectionCatalogue
	}
}

// selectedTypingFixture exposes a lost CLI-to-typing selection flag using
// deliberately conflicting schemas, without claiming Kubernetes admission.
func selectedTypingFixture(t *testing.T) (string, string) {
	t.Helper()

	store := cache.NewOnDiskCache(kubeconform.DefaultSchemaCacheDir())
	require.NoError(
		t,
		store.Set("https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/"+
			"master-standalone-strict/configmap-v1.json", []byte(`{"type":"object","properties":{
"data":{"type":"object","properties":{"attempts":{"type":"string"}}}}}`)),
	)
	location := filepath.Join(t.TempDir(), "configmap_v1.json")
	require.NoError(t, os.WriteFile(location, []byte(`{"type":"object","properties":{
"data":{"type":"object","properties":{"attempts":{"type":"integer"}}}}}`), 0o600))

	return "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: typing\ndata:\n  attempts: ${ATTEMPTS}\n", location
}

// selectedCachedSchema proves the later offline process sees the pinned schema's
// JSON tokens, without trusting a path supplied through the environment.
func selectedCachedSchema(t *testing.T) []byte {
	t.Helper()

	stored, err := cache.NewOnDiskCache(kubeconform.DefaultSchemaCacheDir()).Get(selectionSchemaURL)
	require.NoError(t, err, "offline acceptance requires a real persisted catalogue entry")

	data, ok := stored.([]byte)
	require.True(t, ok, "the native cache must contain schema bytes")

	digest, err := schemaTokenDigest(data)
	require.NoError(t, err)
	require.Equal(t, selectionSchemaContentDigest, digest)

	return data
}

// schemaTokenDigest removes only insignificant JSON whitespace. It preserves
// key order, duplicate keys, number spellings and string contents without decoding.
func schemaTokenDigest(data []byte) (string, error) {
	var compact bytes.Buffer

	err := json.Compact(&compact, data)
	if err != nil {
		return "", fmt.Errorf("compact schema fixture: %w", err)
	}

	digest := sha256.Sum256(compact.Bytes())

	return hex.EncodeToString(digest[:]), nil
}

// selectedSchemaArguments retains full strictness and disables chart rendering
// so every phase validates the HelmRelease resource itself.
func selectedSchemaArguments(phase, location string) []string {
	args := []string{
		".",
		"--strict",
		"--skip-helm-render",
		"--skip-secrets=false",
		"--ignore-missing-schemas=false",
	}
	if phase != "empty" {
		args = append(args, "--schema-location", location)
	}

	switch phase {
	case "warm", "default-control":
		return args
	case "invalid-mode":
		return append(args, "--schema-location-mode", "unsupported")
	case "empty-mode":
		return append(args, "--schema-location-mode", "")
	default:
		return append(args, "--schema-location-mode", "only")
	}
}

// assertSelectedSchemaResult distinguishes successful validation from expected
// refusals and proves the warm phase really persisted the original schema.
func assertSelectedSchemaResult(t *testing.T, phase, output string, err error) {
	t.Helper()

	switch phase {
	case "warm":
		require.NoError(t, err, output)
		selectedCachedSchema(t)
	case "only", "local", "typing-choice":
		require.NoError(t, err, output)
	case "empty":
		require.Error(t, err, output)
		require.Contains(t, output+err.Error(), "at least one")
	case "invalid-mode", "empty-mode":
		require.Error(t, err, output)
		require.Contains(t, output+err.Error(), "must be additional or only")
	default:
		require.Error(t, err, "invalid or unavailable schemas must fail: %s", output)
	}
}

// runSelectedValidation permits the same isolated acceptance fixture to evaluate
// an explicitly built CLI, retaining all assertions used for the public handler.
func runSelectedValidation(t *testing.T, args []string) (string, error) {
	t.Helper()

	if binary := os.Getenv("KSAIL_SCHEMA_SELECTION_BINARY"); binary != "" {
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()

		//nolint:gosec // The local evaluation explicitly supplies its own built test binary.
		cmd := exec.CommandContext(
			ctx,
			binary,
			append([]string{"workload", "validate"}, args...)...)
		output, err := cmd.CombinedOutput()

		return string(output), err
	}

	return runValidate(t, args...)
}
