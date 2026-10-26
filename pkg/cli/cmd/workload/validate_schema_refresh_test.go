package workload_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/devantler-tech/ksail/v7/pkg/svc/fluxsubst"
	"github.com/stretchr/testify/require"
	"github.com/yannh/kubeconform/pkg/cache"
)

const (
	refreshSchemaOld = `{
  "type":"object", "required":["spec"],
  "properties":{"spec":{"type":"object","properties":{"value":{"type":"integer"}},
  "required":["value"],"additionalProperties":false}}
}`
	refreshSchemaCurrent = `{
  "type":"object", "required":["spec"],
  "properties":{"spec":{"type":"object",
  "properties":{"value":{"type":"boolean"},"mode":{"enum":["fresh"]}},
  "required":["value","mode"],"additionalProperties":false}}
}`
	refreshManifestOld = `apiVersion: refresh.ksail.io/v1
kind: RefreshingWidget
metadata:
  name: widget
spec:
  value: ${VALUE}
`
)

// TestValidateRefreshSchemas exercises the real command against changing HTTP
// schemas. A dedicated subprocess isolates proxy and certificate-root caches;
// every schema request is served locally and TLS verification remains enabled.
//
//nolint:paralleltest // The child owns its isolated home/cache environment.
func TestValidateRefreshSchemas(t *testing.T) {
	if os.Getenv("KSAIL_SCHEMA_REFRESH_CHILD") == "1" {
		for _, layout := range []string{"loose", "parallel", "kustomize", "rendered"} {
			t.Run(layout, func(t *testing.T) {
				runSchemaRefreshScenario(t, os.Getenv("KSAIL_SCHEMA_REFRESH_LOCATION"), layout)
			})
		}

		return
	}

	location := changingRefreshSchemaServer(t)
	proxy, cert := localSchemaRegistryProxy(t)
	tempRoot := t.TempDir()

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	//nolint:gosec // The child executes this same test binary with fixed test arguments.
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestValidateRefreshSchemas$", "-test.v")

	cmd.Env = append(os.Environ(),
		"KSAIL_SCHEMA_REFRESH_CHILD=1", "KSAIL_SCHEMA_REFRESH_LOCATION="+location,
		"HTTPS_PROXY="+proxy, "HTTP_PROXY="+proxy, "NO_PROXY=127.0.0.1,localhost",
		"SSL_CERT_FILE="+cert, "TMPDIR="+tempRoot, "TMP="+tempRoot, "TEMP="+tempRoot,
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "isolated refresh command scenario: %s", output)
}

// runSchemaRefreshScenario proves warm-cache recovery, persisted offline reuse,
// invalid-value rejection and preservation of the cache when refresh is unavailable.
func runSchemaRefreshScenario(t *testing.T, location, layout string) {
	t.Helper()
	cacheDir, manifest, parallelManifests, args := prepareRefreshScenario(t, location, layout)

	_, err := runValidate(t, args...)
	require.NoError(t, err, "the original warmed schema must be usable")
	setRefreshSchemaStage(t, location, "current")

	current := refreshManifestOld + "  mode: fresh\n"
	require.NoError(t, os.WriteFile(manifest, []byte(current), 0o600))

	for _, path := range parallelManifests {
		require.NoError(t, os.WriteFile(path, []byte(current), 0o600))
	}

	_, err = runValidate(t, args...)
	require.Error(t, err, "ordinary validation must demonstrate the stale schema")
	tempBefore := refreshTempEntries(t)
	output, err := runValidate(t, append(args, "--refresh-schemas")...)
	require.NoError(t, err, "refresh must accept the current field and boolean schema: %s", output)
	require.Equal(
		t,
		tempBefore,
		refreshTempEntries(t),
		"successful refresh must remove its temporary cache",
	)
	require.Equal(
		t,
		"1",
		refreshRequestCount(t, location),
		"parallel validators must reuse one freshly fetched response",
	)
	setRefreshSchemaStage(t, location, "unavailable")
	_, err = runValidate(t, args...)
	require.NoError(t, err, "the next ordinary invocation must reuse the refreshed schema offline")

	invalid := strings.Replace(current, "mode: fresh", "mode: invalid", 1)
	require.NoError(t, os.WriteFile(manifest, []byte(invalid), 0o600))
	_, err = runValidate(t, args...)
	require.Error(t, err, "refresh must not bypass enum validation")
	require.NoError(t, os.WriteFile(manifest, []byte(current), 0o600))
	stableBefore := refreshCacheDigests(t, cacheDir)
	_, err = runValidate(t, append(args, "--refresh-schemas")...)
	require.Error(t, err, "an unavailable refresh must never invent a clean result")
	require.Equal(
		t,
		stableBefore,
		refreshCacheDigests(t, cacheDir),
		"failed refresh must preserve existing cache bytes",
	)
	require.Equal(
		t,
		tempBefore,
		refreshTempEntries(t),
		"failed refresh must remove its temporary cache",
	)
	_, err = runValidate(t, args...)
	require.NoError(t, err, "a failed refresh must preserve normal offline validation")
}

// changingRefreshSchemaServer changes only fixture responses, never a real registry.
func changingRefreshSchemaServer(t *testing.T) string {
	t.Helper()

	state := &refreshSchemaState{overlap: make(chan struct{})}
	server := httptest.NewServer(
		http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			state.Lock()
			if request.Method == http.MethodPost && request.URL.Path == "/stage" {
				state.stage = request.URL.Query().Get("value")
				state.requests = 0
				state.overlap = make(chan struct{})
				state.Unlock()
				response.WriteHeader(http.StatusNoContent)

				return
			}

			if request.URL.Path == "/requests" {
				count := state.requests
				state.Unlock()

				_, _ = io.WriteString(response, strconv.Itoa(count))

				return
			}

			stage := state.stage
			if request.URL.Path != "/schema.json" || stage == "unavailable" {
				state.Unlock()
				http.NotFound(response, request)

				return
			}

			if !state.waitForSchemaRequest(request, stage) {
				return
			}

			response.Header().Set("Content-Type", "application/json")

			schema := refreshSchemaOld
			if stage == "current" {
				schema = refreshSchemaCurrent
			}

			_, _ = io.WriteString(response, schema)
		}),
	)
	t.Cleanup(server.Close)

	return server.URL + "/schema.json"
}

// refreshRequestCount observes real successful fixture downloads after a stage change.
func refreshRequestCount(t *testing.T, location string) string {
	t.Helper()

	url := strings.TrimSuffix(location, "/schema.json") + "/requests"
	response := refreshFixtureRequest(t, http.MethodGet, url)

	defer func() { _ = response.Body.Close() }()

	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	return string(data)
}

// setRefreshSchemaStage updates the loopback fixture used by the child command.
func setRefreshSchemaStage(t *testing.T, location, stage string) {
	t.Helper()

	url := strings.TrimSuffix(location, "/schema.json") + "/stage?value=" + stage
	response := refreshFixtureRequest(t, http.MethodPost, url)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusNoContent, response.StatusCode)
}

// refreshCacheDigests checks actual persistent bytes without relying on cache internals.
func refreshCacheDigests(t *testing.T, dir string) map[string]string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		//nolint:gosec // dir and its entries belong to this test's isolated cache.
		data, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		require.NoError(t, readErr)

		digest := sha256.Sum256(data)
		result[entry.Name()] = hex.EncodeToString(digest[:])
	}

	return result
}

// refreshTempEntries observes only this subprocess's private temporary directory.
func refreshTempEntries(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(os.TempDir())
	require.NoError(t, err)

	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Name())
	}

	return result
}

// localSchemaRegistryProxy serves built-in registry misses locally over verified
// TLS. It accepts one exact CONNECT authority and forwards only to its fixed
// loopback fixture; it can never act as a proxy to an external destination.
func localSchemaRegistryProxy(t *testing.T) (string, string) {
	t.Helper()

	registry, certPath := localSchemaRegistry(t)

	fixture := &schemaRegistryProxy{target: registry.Listener.Addr().String()}
	proxy := httptest.NewServer(fixture)
	t.Cleanup(proxy.Close)
	t.Cleanup(fixture.closeConnections)

	return proxy.URL, certPath
}

// prepareRefreshScenario warms the original schema and creates the selected real input layout.
func prepareRefreshScenario(
	t *testing.T,
	location, layout string,
) (string, string, []string, []string) {
	t.Helper()
	setRefreshSchemaStage(t, location, "old")

	home := t.TempDir()
	for _, key := range []string{"HOME", "USERPROFILE", "XDG_CACHE_HOME", "LOCALAPPDATA"} {
		t.Setenv(key, home)
	}

	cacheDir := fluxsubst.SchemaCacheDir()
	require.NoError(t, os.MkdirAll(cacheDir, 0o700))
	store := cache.NewOnDiskCache(cacheDir)

	for _, variant := range []string{"master-standalone", "master-standalone-strict"} {
		url := "https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/" +
			variant + "/refreshingwidget-refresh-v1.json"
		require.NoError(t, store.Set(url, []byte("null")))
	}

	warm := strings.Replace(refreshManifestOld, "${VALUE}", "3", 1)
	require.NoError(t, kubeconform.NewClient().ValidateBytes(t.Context(), "warm.yaml", []byte(warm),
		&kubeconform.ValidationOptions{Strict: true, SchemaLocations: []string{location}}))
	dir := t.TempDir()
	manifest := filepath.Join(dir, "widget.yaml")
	require.NoError(t, os.WriteFile(manifest, []byte(refreshManifestOld), 0o600))

	if layout != "loose" && layout != "parallel" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "kustomization.yaml"),
			[]byte("resources:\n  - widget.yaml\n"), 0o600))
	}

	var parallelManifests []string

	if layout == "parallel" {
		for _, name := range []string{"one", "two", "three", "four"} {
			path := filepath.Join(dir, name+".yaml")
			parallelManifests = append(parallelManifests, path)
			require.NoError(t, os.WriteFile(path, []byte(refreshManifestOld), 0o600))
		}
	}

	args := []string{
		dir,
		"--strict",
		"--ignore-missing-schemas=false",
		"--schema-location",
		location,
	}
	if layout != "rendered" {
		args = append(args, "--skip-helm-render")
	}

	return cacheDir, manifest, parallelManifests, args
}

// refreshSchemaState is owned by one loopback fixture.
type refreshSchemaState struct {
	sync.Mutex

	stage    string
	requests int
	overlap  chan struct{}
}

// waitForSchemaRequest exposes parallel cold downloads with a bounded delay.
// The caller owns the lock; this releases it before waiting.
func (state *refreshSchemaState) waitForSchemaRequest(request *http.Request, stage string) bool {
	state.requests++

	count, overlap := state.requests, state.overlap
	if count == 2 {
		close(overlap)
	}
	state.Unlock()
	// Without coalescing, parallel cold readers reach the second request.
	// A serialized fetch completes after this bounded fixture delay instead.
	if stage == "current" && count == 1 {
		select {
		case <-overlap:
		case <-time.After(time.Second):
		case <-request.Context().Done():
			return false
		}
	}

	return true
}

// localSchemaRegistry creates a test-only CA and TLS endpoint for exact built-in misses.
func localSchemaRegistry(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "schema fixture"},
		DNSNames: []string{"raw.githubusercontent.com"}, NotBefore: time.Now().Add(-time.Minute),
		NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, publicKey, privateKey)
	require.NoError(t, err)
	certPath := filepath.Join(t.TempDir(), "registry-ca.pem")
	require.NoError(
		t,
		os.WriteFile(
			certPath,
			pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
			0o600,
		),
	)

	registry := httptest.NewUnstartedServer(http.NotFoundHandler())
	registry.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: privateKey}},
	}
	registry.StartTLS()
	t.Cleanup(registry.Close)

	return registry, certPath
}

// schemaRegistryProxy forwards only the fixed built-in authority to a loopback TLS fixture.
type schemaRegistryProxy struct {
	sync.Mutex

	target      string
	connections []net.Conn
}

func (proxy *schemaRegistryProxy) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodConnect ||
		request.Host != "raw.githubusercontent.com:443" {
		http.Error(response, "unexpected fixture authority", http.StatusForbidden)

		return
	}

	var dialer net.Dialer

	target, dialErr := dialer.DialContext(
		request.Context(),
		"tcp",
		proxy.target,
	)
	if dialErr != nil {
		http.Error(response, "fixture unavailable", http.StatusBadGateway)

		return
	}

	hijacker, ok := response.(http.Hijacker)
	if !ok {
		_ = target.Close()

		http.Error(response, "hijack unavailable", http.StatusInternalServerError)

		return
	}

	client, buffered, hijackErr := hijacker.Hijack()
	if hijackErr != nil {
		_ = target.Close()

		return
	}

	proxy.Lock()

	proxy.connections = append(proxy.connections, client, target)
	proxy.Unlock()

	defer func() { _ = client.Close() }()
	defer func() { _ = target.Close() }()

	_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	if buffered.Flush() != nil {
		return
	}

	go func() { _, _ = io.Copy(target, client); _ = target.Close() }()

	_, _ = io.Copy(client, target)
}

func (proxy *schemaRegistryProxy) closeConnections() {
	proxy.Lock()
	defer proxy.Unlock()

	for _, conn := range proxy.connections {
		_ = conn.Close()
	}
}

// refreshFixtureRequest sends only loopback fixture requests and binds their lifetime to the test.
func refreshFixtureRequest(t *testing.T, method, location string) *http.Response {
	t.Helper()
	//nolint:gosec // Before execution below, the URL is required to be the parent-owned HTTP loopback fixture.
	request, err := http.NewRequestWithContext(t.Context(), method, location, http.NoBody)
	require.NoError(t, err)
	require.Equal(t, "http", request.URL.Scheme)
	require.Equal(t, "127.0.0.1", request.URL.Hostname())
	//nolint:gosec // The request URL is asserted to be the parent-owned loopback fixture above.
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	return response
}
