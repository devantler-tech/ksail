package clusterapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
)

const (
	// pluginsDirName is the ~/.ksail subdirectory web-UI plugins are installed under.
	pluginsDirName = "plugins"
	// defaultPluginMain is the entry bundle served when a plugin's package.json names none. It matches
	// the Headlamp convention so a Headlamp plugin (which builds to main.js) drops in without a manifest
	// edit.
	defaultPluginMain = "main.js"
	// pluginPackageFile is the metadata file read from each plugin directory.
	pluginPackageFile = "package.json"
)

// pluginStore serves web-UI plugins from a local directory (default ~/.ksail/plugins). Each plugin is
// a directory containing a Headlamp-format package.json and an entry bundle (main.js by default), so
// an existing Headlamp plugin drops in unmodified. The Service embeds one so `ksail ui` advertises the
// plugins capability (api.PluginService) and serves the bundles the SPA loads as same-origin scripts.
type pluginStore struct {
	// dir resolves the plugins directory. Injectable so tests point at a temp dir instead of ~/.ksail,
	// mirroring the kubeconfigPath seam on Service.
	dir func() (string, error)
	// cosign verifies a download against cosign/sigstore material (the strongest authenticity tier). It
	// is wired in by the `ksail open web` command (Service.UseCosignVerifier) so the heavy sigstore-go
	// dependency stays out of clusterapi and the desktop module; nil means cosign verification is
	// unavailable, so a request carrying cosign material is rejected rather than downgraded.
	cosign cosignVerifier
}

// pluginPackage is the subset of a plugin's package.json the store reads for metadata. The author and
// homepage fields vary in shape across manifests (string or object), so only the simple string fields
// are read; a parse failure falls back to directory-name defaults, so a non-standard package.json
// never hides an otherwise-loadable plugin.
type pluginPackage struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Main        string `json:"main"`
}

// defaultPluginsDir resolves the per-user plugins directory (~/.ksail/plugins). Missing is not an
// error: ListPlugins reports an empty set when the directory does not exist yet.
func defaultPluginsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".ksail", pluginsDirName), nil
}

// ListPlugins enumerates the installed plugins. A missing plugins directory yields an empty slice
// (no plugins installed), not an error. Directories without a loadable entry bundle are skipped so the
// SPA is never told to load a plugin that would 404.
func (p pluginStore) ListPlugins(_ context.Context) ([]api.PluginInfo, error) {
	dir, err := p.dir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []api.PluginInfo{}, nil
		}

		return nil, fmt.Errorf("read plugins directory: %w", err)
	}

	plugins := make([]api.PluginInfo, 0, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Skip hidden directories (e.g. the .staging-* directory an in-progress install uses) so a
		// partial or leaked install is never advertised as a loadable plugin.
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		info, ok := pluginInfoFor(dir, entry.Name())
		if ok {
			plugins = append(plugins, info)
		}
	}

	sort.Slice(plugins, func(i, j int) bool { return plugins[i].Name < plugins[j].Name })

	return plugins, nil
}

// pluginInfoFor builds a plugin's metadata from its directory: the directory name is the addressable
// id (Name, used in asset URLs); package.json supplies the display title/version/description and an
// optional entry file. It returns ok=false when the entry bundle is missing or escapes the plugin
// directory, so such a directory is skipped from the list rather than advertised as loadable.
func pluginInfoFor(dir, name string) (api.PluginInfo, bool) {
	pluginDir := filepath.Join(dir, name)
	info := api.PluginInfo{Name: name, Main: defaultPluginMain}

	data, err := fsutil.ReadFileSafe(pluginDir, filepath.Join(pluginDir, pluginPackageFile))
	if err == nil {
		var pkg pluginPackage
		if json.Unmarshal(data, &pkg) == nil {
			info.Title = pkg.Name
			info.Version = pkg.Version
			info.Description = pkg.Description

			if pkg.Main != "" {
				info.Main = pkg.Main
			}
		}
	}

	// The entry bundle must be a local path within the plugin directory and exist as a file, else the
	// plugin cannot load. filepath.IsLocal rejects an absolute or ..-escaping "main" from package.json.
	if !filepath.IsLocal(info.Main) {
		return api.PluginInfo{}, false
	}

	stat, statErr := os.Stat(filepath.Join(pluginDir, info.Main))
	if statErr != nil || stat.IsDir() {
		return api.PluginInfo{}, false
	}

	return info, true
}

// PluginAsset serves a plugin's file by plugin id (directory name) and the relative file path within
// it. Both segments must be local paths (no absolute or ..-escaping components) and the read is
// containment-checked by fsutil.ReadFileSafe, so a crafted path cannot read outside the plugin
// directory. Any failure maps to api.ErrNotFound (404) rather than leaking why.
func (p pluginStore) PluginAsset(_ context.Context, name, file string) (api.PluginAsset, error) {
	dir, err := p.dir()
	if err != nil {
		return api.PluginAsset{}, err
	}

	if !filepath.IsLocal(name) || file == "" || !filepath.IsLocal(file) {
		return api.PluginAsset{}, fmt.Errorf("%w: plugin asset %s/%s", api.ErrNotFound, name, file)
	}

	pluginDir := filepath.Join(dir, name)

	content, err := fsutil.ReadFileSafe(pluginDir, filepath.Join(pluginDir, file))
	if err != nil {
		return api.PluginAsset{}, fmt.Errorf("%w: plugin asset %s/%s", api.ErrNotFound, name, file)
	}

	return api.PluginAsset{Content: content, ContentType: api.PluginContentType(file)}, nil
}

// ListPlugins exposes the embedded plugin store's listing on the Service so it satisfies
// api.PluginService (advertising capabilities.plugins=true for the local `ksail ui`/desktop backend).
func (s *Service) ListPlugins(ctx context.Context) ([]api.PluginInfo, error) {
	return s.plugins.ListPlugins(ctx)
}

// PluginAsset exposes the embedded plugin store's asset reads on the Service (api.PluginService).
func (s *Service) PluginAsset(ctx context.Context, name, file string) (api.PluginAsset, error) {
	return s.plugins.PluginAsset(ctx, name, file)
}
