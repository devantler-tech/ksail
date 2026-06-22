package api

import (
	"context"
	"net/http"
	"path"
	"strings"
)

// PluginInfo describes one installed web-UI plugin the SPA can load. The shape deliberately mirrors
// the metadata a Headlamp plugin carries in its package.json (name/version/description) plus the
// entry bundle to load, so an existing Headlamp plugin is described without a bespoke manifest.
//
// Name is the plugin's addressable id — the directory segment under which its assets are served
// (GET /api/v1/plugins/{Name}/{file...}); Title is the human-readable package.json name. Keeping the
// two distinct lets the asset URLs stay stable (the directory) while the UI shows a friendly label.
type PluginInfo struct {
	// Name is the plugin id used in asset URLs (its install-directory name).
	Name string `json:"name"`
	// Title is the display name from the plugin's package.json (falls back to Name in the SPA).
	Title string `json:"title,omitempty"`
	// Version and Description are optional package.json metadata surfaced in the Plugins view.
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	// Main is the entry bundle, served at /api/v1/plugins/{Name}/{Main}; defaults to "main.js".
	Main string `json:"main"`
}

// PluginAsset is a plugin file served to the browser: its bytes plus the content type to set. The
// service determines the content type from the file extension so the SPA can load a plugin bundle as
// a classic <script> with the correct MIME (the strict CSP forbids eval, so plugins load as
// same-origin scripts — see the SPA's plugin loader).
type PluginAsset struct {
	Content     []byte
	ContentType string
}

// PluginService is an optional interface a ClusterService may implement to expose installed web-UI
// plugins (Headlamp-compatible JS bundles) for the SPA to load. A backend that implements it
// advertises capabilities.plugins=true; the SPA then loads each plugin's entry bundle as a
// same-origin classic script (CSP-safe — no eval) with window.pluginLib in scope, mapping the
// plugin's register*() calls onto the SPA's native extension registry.
//
// The local `ksail ui`/desktop backend implements it over a local plugins directory
// (~/.ksail/plugins); the operator leaves it unimplemented (in-cluster plugin serving carries a
// larger trust surface and is deferred), so the capability stays false and the routes are not
// registered. Both methods are reads, so the read-only guard does not apply.
type PluginService interface {
	// ListPlugins returns the installed plugins' metadata. Implementations return a non-nil slice
	// (empty, not nil) so the JSON encodes as [] rather than null.
	ListPlugins(ctx context.Context) ([]PluginInfo, error)
	// PluginAsset returns a plugin's file by plugin id (name) and the relative file path within it. It
	// MUST reject any path that escapes the plugin's directory — returning ErrNotFound — so a crafted
	// file segment cannot read files outside the plugin. The returned asset carries the bytes and the
	// content type to set on the response.
	PluginAsset(ctx context.Context, name, file string) (PluginAsset, error)
}

// PluginInstallRequest is the body of POST /api/v1/plugins: install a Headlamp-format plugin tarball
// from a URL. SHA256 (hex, optional) pins the download to a known digest; Name (optional) overrides the
// install id when the package.json name is unsuitable or absent.
type PluginInstallRequest struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256,omitempty"`
	Name   string `json:"name,omitempty"`
}

// PluginInstaller is an optional interface a ClusterService may implement to install and uninstall
// web-UI plugins. A backend that implements it advertises capabilities.pluginInstall=true; the SPA then
// offers the install/uninstall surface behind an explicit trust prompt (an installed plugin runs
// unsandboxed with the user's cluster credentials). Both methods mutate, so they are registered behind
// the read-only guard and only on the local loopback backend — the operator leaves them unimplemented.
type PluginInstaller interface {
	// InstallPlugin downloads, verifies and installs a plugin from req, returning its metadata.
	InstallPlugin(ctx context.Context, req PluginInstallRequest) (PluginInfo, error)
	// UninstallPlugin removes an installed plugin by id (name).
	UninstallPlugin(ctx context.Context, name string) error
}

// pluginService returns the backend's PluginService, or false when it does not implement one (the
// routes are only registered when it does, so this is belt-and-suspenders for the handlers).
func (s *Server) pluginService() (PluginService, bool) {
	svc, ok := s.Service.(PluginService)

	return svc, ok
}

func (s *Server) handleListPlugins(writer http.ResponseWriter, request *http.Request) {
	svc, ok := s.pluginService()
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	plugins, err := svc.ListPlugins(request.Context())
	if err != nil {
		writeClientError(writer, err)

		return
	}

	if plugins == nil {
		plugins = []PluginInfo{}
	}

	writeJSON(writer, http.StatusOK, map[string]any{"plugins": plugins})
}

func (s *Server) handlePluginAsset(writer http.ResponseWriter, request *http.Request) {
	svc, ok := s.pluginService()
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	name := request.PathValue("name")
	// Reject a plugin id that is empty or a relative path element; the service's PluginAsset also
	// enforces containment, but rejecting these here keeps a crafted id from ever reaching the store.
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		writeClientError(writer, ErrNotFound)

		return
	}

	asset, err := svc.PluginAsset(request.Context(), name, request.PathValue("file"))
	if err != nil {
		writeClientError(writer, err)

		return
	}

	// Plugin assets are same-origin classic scripts/styles; nosniff keeps the declared content type
	// authoritative so the browser never reinterprets a bundle as another type.
	writer.Header().Set("Content-Type", asset.ContentType)
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = writer.Write(asset.Content)
}

// handleInstallPlugin installs a plugin from the POSTed PluginInstallRequest. Registered only when the
// backend implements PluginInstaller and gated by the read-only guard (it mutates the plugins
// directory). An install failure is surfaced as 422 with the explanatory message.
func (s *Server) handleInstallPlugin(writer http.ResponseWriter, request *http.Request) {
	installer, ok := s.Service.(PluginInstaller)
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	var req PluginInstallRequest

	err := decodeJSON(writer, request, &req)
	if err != nil {
		return
	}

	info, err := installer.InstallPlugin(request.Context(), req)
	if err != nil {
		// Any install failure (bad URL, download, checksum, archive or layout) is a problem with the
		// request rather than the server, so surface it as 422 with the message instead of a 500.
		writeError(writer, http.StatusUnprocessableEntity, err)

		return
	}

	writeJSON(writer, http.StatusCreated, info)
}

// handleUninstallPlugin removes an installed plugin by id. The id is validated here (and again in the
// store) so a crafted path can never reach the filesystem.
func (s *Server) handleUninstallPlugin(writer http.ResponseWriter, request *http.Request) {
	installer, ok := s.Service.(PluginInstaller)
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	name := request.PathValue("name")
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		writeClientError(writer, ErrNotFound)

		return
	}

	err := installer.UninstallPlugin(request.Context(), name)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	writer.WriteHeader(http.StatusNoContent)
}

// PluginContentType maps a plugin file's extension to the content type the asset handler sets. It is
// exported so a PluginService implementation building PluginAsset uses the same mapping the server
// would, keeping the served MIME consistent across backends.
func PluginContentType(file string) string {
	switch strings.ToLower(path.Ext(file)) {
	case ".js", ".mjs", ".cjs":
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json", ".map":
		return "application/json; charset=utf-8"
	case ".html":
		return "text/html; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
}
