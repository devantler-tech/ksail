package clusterapi

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
)

// makeTarGz builds an in-memory gzip-compressed tar from name->content entries.
func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer

	gzipWriter := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gzipWriter)

	for name, content := range files {
		header := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}

		writeErr := tarWriter.WriteHeader(header)
		if writeErr != nil {
			t.Fatalf("write tar header: %v", writeErr)
		}

		_, writeErr = tarWriter.Write([]byte(content))
		if writeErr != nil {
			t.Fatalf("write tar body: %v", writeErr)
		}
	}

	_ = tarWriter.Close()
	_ = gzipWriter.Close()

	return buf.Bytes()
}

// TestIsTrustedPluginHost asserts the download host allowlist: GitHub / Artifact Hub (and their
// subdomains) and loopback are allowed; arbitrary, look-alike, internal, and cloud-metadata hosts are
// rejected, so a user-supplied URL cannot be aimed at the internal network.
func TestIsTrustedPluginHost(t *testing.T) {
	t.Parallel()

	allowed := []string{
		"github.com", "objects.githubusercontent.com", "raw.githubusercontent.com",
		"owner.github.io", "artifacthub.io", "localhost", "127.0.0.1", "::1",
	}
	for _, host := range allowed {
		if !isTrustedPluginHost(host) {
			t.Errorf("isTrustedPluginHost(%q) = false, want true", host)
		}
	}

	rejected := []string{
		"", "evil.com", "github.com.evil.com", "notgithub.com",
		"169.254.169.254", "10.0.0.5", "192.168.1.1", "metadata.google.internal",
	}
	for _, host := range rejected {
		if isTrustedPluginHost(host) {
			t.Errorf("isTrustedPluginHost(%q) = true, want false", host)
		}
	}
}

// TestIsBlockedPluginIP asserts the dialer's SSRF guard blocks private, link-local (cloud metadata), and
// unspecified addresses while permitting public and loopback addresses (the latter for local dev).
func TestIsBlockedPluginIP(t *testing.T) {
	t.Parallel()

	blocked := []string{
		"169.254.169.254",
		"10.0.0.5",
		"172.16.0.1",
		"192.168.1.1",
		"0.0.0.0",
		"fe80::1",
	}
	for _, addr := range blocked {
		if !isBlockedPluginIP(net.ParseIP(addr)) {
			t.Errorf("isBlockedPluginIP(%q) = false, want true", addr)
		}
	}

	allowed := []string{"8.8.8.8", "140.82.112.3", "127.0.0.1", "::1"}
	for _, addr := range allowed {
		if isBlockedPluginIP(net.ParseIP(addr)) {
			t.Errorf("isBlockedPluginIP(%q) = true, want false", addr)
		}
	}
}

// serveBytes serves data over a throwaway HTTP server, closed on test cleanup.
func serveBytes(t *testing.T, data []byte) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write(data)
		}),
	)
	t.Cleanup(server.Close)

	return server
}

// newTempStore returns a pluginStore rooted at a temp directory.
func newTempStore(t *testing.T) (pluginStore, string) {
	t.Helper()

	dir := t.TempDir()

	return pluginStore{dir: func() (string, error) { return dir, nil }}, dir
}

func TestPluginStoreInstallAndUninstall(t *testing.T) {
	t.Parallel()

	store, dir := newTempStore(t)
	archive := makeTarGz(t, map[string]string{
		"my-plugin/package.json": `{"name":"my-plugin","version":"1.0.0","main":"main.js"}`,
		"my-plugin/main.js":      `console.log("hi")`,
	})
	server := serveBytes(t, archive)

	info, err := store.install(context.Background(), api.PluginInstallRequest{URL: server.URL})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	if info.Name != "my-plugin" {
		t.Errorf("info.Name = %q, want my-plugin", info.Name)
	}

	_, statErr := os.Stat(filepath.Join(dir, "my-plugin", "main.js"))
	if statErr != nil {
		t.Errorf("entry bundle not installed: %v", statErr)
	}

	uninstallErr := store.uninstall("my-plugin")
	if uninstallErr != nil {
		t.Fatalf("uninstall: %v", uninstallErr)
	}

	_, statErr = os.Stat(filepath.Join(dir, "my-plugin"))
	if !os.IsNotExist(statErr) {
		t.Errorf("plugin directory still present after uninstall")
	}
}

func TestPluginStoreInstallRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	store, dir := newTempStore(t)
	archive := makeTarGz(t, map[string]string{"../escape.js": "pwned"})
	server := serveBytes(t, archive)

	_, err := store.install(context.Background(), api.PluginInstallRequest{URL: server.URL})
	if !errors.Is(err, ErrPluginInstall) {
		t.Errorf("install error = %v, want ErrPluginInstall", err)
	}

	_, statErr := os.Stat(filepath.Join(filepath.Dir(dir), "escape.js"))
	if !os.IsNotExist(statErr) {
		t.Errorf("path traversal wrote outside the plugins directory")
	}
}

func TestPluginStoreInstallRejectsChecksumMismatch(t *testing.T) {
	t.Parallel()

	store, _ := newTempStore(t)
	archive := makeTarGz(t, map[string]string{
		"p/package.json": `{"name":"p","main":"main.js"}`,
		"p/main.js":      "x",
	})
	server := serveBytes(t, archive)

	_, err := store.install(
		context.Background(),
		api.PluginInstallRequest{URL: server.URL, SHA256: "not-the-real-checksum"},
	)
	if !errors.Is(err, ErrPluginInstall) {
		t.Errorf("install error = %v, want ErrPluginInstall for a checksum mismatch", err)
	}
}
