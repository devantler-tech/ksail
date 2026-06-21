package clusterapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
)

// fluxPackageJSON is a representative Headlamp-format package.json used across the store tests.
const fluxPackageJSON = `{"name":"Flux UI","version":"1.0.0","description":"Flux views"}`

// writePluginDir creates a plugin directory under root with an optional package.json and main.js.
// Passing an empty string for either file skips writing it (to exercise the missing-metadata and
// missing-bundle paths).
func writePluginDir(t *testing.T, root, name, pkg, main string) {
	t.Helper()

	dir := filepath.Join(root, name)

	err := os.MkdirAll(dir, 0o750)
	if err != nil {
		t.Fatalf("mkdir plugin %q: %v", name, err)
	}

	if pkg != "" {
		err = os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o600)
		if err != nil {
			t.Fatalf("write package.json: %v", err)
		}
	}

	if main != "" {
		err = os.WriteFile(filepath.Join(dir, "main.js"), []byte(main), 0o600)
		if err != nil {
			t.Fatalf("write main.js: %v", err)
		}
	}
}

// storeAt returns a pluginStore rooted at dir (the kubeconfigPath-style dir seam).
func storeAt(dir string) pluginStore {
	return pluginStore{dir: func() (string, error) { return dir, nil }}
}

func TestPluginStoreListsInstalledPlugins(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePluginDir(t, root, "flux", fluxPackageJSON, "console.log(1)")
	// A directory without an entry bundle is not loadable, so it is skipped.
	writePluginDir(t, root, "broken", `{"name":"Broken"}`, "")

	// A stray file (not a directory) is ignored.
	err := os.WriteFile(filepath.Join(root, "README.md"), []byte("x"), 0o600)
	if err != nil {
		t.Fatalf("write stray file: %v", err)
	}

	plugins, err := storeAt(root).ListPlugins(context.Background())
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}

	if len(plugins) != 1 {
		t.Fatalf("got %d plugins, want 1: %+v", len(plugins), plugins)
	}

	got := plugins[0]
	if got.Name != "flux" || got.Title != "Flux UI" ||
		got.Version != "1.0.0" || got.Main != "main.js" {
		t.Errorf("plugin = %+v", got)
	}
}

func TestPluginStoreFallsBackToDefaultsWithoutPackageJSON(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePluginDir(t, root, "bare", "", "bundle")

	plugins, err := storeAt(root).ListPlugins(context.Background())
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}

	if len(plugins) != 1 {
		t.Fatalf("got %d plugins, want 1", len(plugins))
	}

	if plugins[0].Name != "bare" || plugins[0].Main != "main.js" || plugins[0].Title != "" {
		t.Errorf("plugin = %+v, want name=bare main=main.js title empty", plugins[0])
	}
}

func TestPluginStoreMissingDirectoryIsEmpty(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "does-not-exist")

	plugins, err := storeAt(missing).ListPlugins(context.Background())
	if err != nil {
		t.Fatalf("ListPlugins on missing dir: %v", err)
	}

	if len(plugins) != 0 {
		t.Errorf("got %d plugins, want 0 for a missing plugins dir", len(plugins))
	}
}

func TestPluginStoreServesAsset(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePluginDir(t, root, "flux", `{"name":"Flux"}`, "BUNDLE_BYTES")

	asset, err := storeAt(root).PluginAsset(context.Background(), "flux", "main.js")
	if err != nil {
		t.Fatalf("PluginAsset: %v", err)
	}

	if string(asset.Content) != "BUNDLE_BYTES" {
		t.Errorf("content = %q, want BUNDLE_BYTES", asset.Content)
	}

	if asset.ContentType != api.PluginContentType("main.js") {
		t.Errorf("content type = %q", asset.ContentType)
	}
}

func TestPluginStoreRejectsPathEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePluginDir(t, root, "flux", `{}`, "x")

	// A secret sibling outside any plugin directory that traversal must not reach.
	err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("secret"), 0o600)
	if err != nil {
		t.Fatalf("write secret: %v", err)
	}

	store := storeAt(root)

	cases := []struct{ name, file string }{
		{"flux", "../secret.txt"},
		{"flux", "../../etc/hosts"},
		{"..", "secret.txt"},
		{"flux", ""},
	}

	for _, tc := range cases {
		_, err := store.PluginAsset(context.Background(), tc.name, tc.file)
		if !errors.Is(err, api.ErrNotFound) {
			t.Errorf("PluginAsset(%q, %q) error = %v, want ErrNotFound", tc.name, tc.file, err)
		}
	}
}
