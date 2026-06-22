package clusterapi

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
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
