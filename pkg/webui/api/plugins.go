package api

import (
	"context"
	"errors"
	"fmt"
	"mime"
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
// from a URL. The verification tiers are layered strongest-first (see pkg/cli/clusterapi/plugininstall.go
// for the full trust model):
//
//   - Cosign (strongest, optional): a sigstore bundle verified against the public-good trust root
//     (keyless: Fulcio cert + Rekor entry, enforcing an expected certificate identity) or a cosign
//     ECDSA public key (key-based). When set, a failure rejects the install (422); when absent, the
//     lighter tiers below apply.
//   - SHA256 (hex, optional): pins the download to a known digest (integrity only).
//   - Signature (base64 ed25519 detached signature, optional): authenticates the bytes against the
//     trusted public key configured out-of-band (KSAIL_PLUGIN_SIGNING_PUBKEY).
//
// Name (optional) overrides the install id when the package.json name is unsuitable or absent.
type PluginInstallRequest struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256,omitempty"`
	// Trusted is the server-enforced acknowledgement that the caller understands the plugin will run
	// unsandboxed as same-origin JavaScript with access to this UI's cluster APIs. The SPA sets it only
	// after the explicit consent checkbox is checked; direct callers must opt in too.
	Trusted bool `json:"trusted"`
	// Signature is an optional base64-encoded ed25519 detached signature over the downloaded tarball
	// bytes. When set, the install verifies it against KSAIL_PLUGIN_SIGNING_PUBKEY and rejects the
	// install when no trusted key is configured (a claimed signature is never silently ignored).
	Signature string `json:"signature,omitempty"`
	// Cosign carries optional sigstore/cosign verification material (the strongest tier). When present
	// and non-empty it gates the install; when nil or empty the install falls through to the SHA-256 +
	// ed25519 tiers. A cosign verification failure rejects the install (422).
	Cosign *PluginCosign `json:"cosign,omitempty"`
	Name   string        `json:"name,omitempty"`
}

// PluginCosign carries cosign/sigstore verification material for a plugin install. It is set when the
// caller wants the strongest authenticity tier; it gates the install (a failure is fatal). Exactly one
// of the two modes is used, in this order:
//
//   - Keyless: provide the sigstore Bundle (the Fulcio cert + Rekor entry + signature over the tarball)
//     plus the expected certificate Identity (IdentitySubject + IdentityIssuer). The bundle is verified
//     against the public-good trust root and the signing certificate's identity must match.
//   - Key-based: provide the cosign PublicKey (a PEM-encoded ECDSA public key) and a Bundle (or a bare
//     signature carried in the bundle). The tarball bytes are verified against that key.
//
// The Bundle is supplied inline (raw or base64-encoded sigstore-bundle JSON) via Bundle — the SPA fetches
// it client-side, so KSail's backend never fetches a user-supplied URL.
type PluginCosign struct {
	// Bundle is the sigstore bundle as JSON, supplied inline (raw JSON or base64-encoded JSON). Required
	// for both keyless and key-based verification.
	Bundle string `json:"bundle,omitempty"`
	// PublicKey is a PEM-encoded cosign ECDSA public key for key-based verification. When set, the
	// verifier uses key-based mode (and ignores the identity fields); when empty, keyless mode is used.
	PublicKey string `json:"publicKey,omitempty"`
	// IdentitySubject is the expected signing-certificate SAN (keyless mode). It is matched exactly
	// unless IdentitySubjectRegex is set, in which case it is treated as a regular expression.
	IdentitySubject string `json:"identitySubject,omitempty"`
	// IdentitySubjectRegex, when true, treats IdentitySubject as a regular expression rather than an
	// exact SAN match.
	IdentitySubjectRegex bool `json:"identitySubjectRegex,omitempty"`
	// IdentityIssuer is the expected OIDC issuer recorded in the signing certificate (keyless mode). It
	// is matched exactly unless IdentityIssuerRegex is set.
	IdentityIssuer string `json:"identityIssuer,omitempty"`
	// IdentityIssuerRegex, when true, treats IdentityIssuer as a regular expression rather than an exact
	// issuer match.
	IdentityIssuerRegex bool `json:"identityIssuerRegex,omitempty"`
}

// IsEmpty reports whether no cosign material is present, so the install can fall through to the lighter
// SHA-256 + ed25519 tiers. A nil receiver is empty.
func (c *PluginCosign) IsEmpty() bool {
	if c == nil {
		return true
	}

	return strings.TrimSpace(c.Bundle) == "" &&
		strings.TrimSpace(c.PublicKey) == "" &&
		strings.TrimSpace(c.IdentitySubject) == "" &&
		strings.TrimSpace(c.IdentityIssuer) == ""
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

// CatalogEntry describes one installable plugin offered by a PluginCatalog (e.g. a Headlamp plugin
// published on Artifact Hub). URL is a direct .tar.gz the existing install flow consumes, so the SPA's
// Install button can hand it straight to InstallPlugin without any further resolution.
type CatalogEntry struct {
	// Name is the plugin's display name (and the suggested install id).
	Name string `json:"name"`
	// Description, Version and Repository are catalog metadata surfaced in the search results.
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
	Repository  string `json:"repository,omitempty"`
	// URL is the installable tarball (.tar.gz) the install flow downloads.
	URL string `json:"url"`
	// Checksum is the tarball's SHA-256 hex (when the catalog publishes one), forwarded to the install
	// flow so it verifies integrity before extracting.
	Checksum string `json:"checksum,omitempty"`
}

// PluginCatalog is an optional interface a ClusterService may implement to browse installable web-UI
// plugins from a remote catalog (the local backend queries Artifact Hub for Headlamp plugins). A
// backend that implements it advertises capabilities.pluginCatalog=true; the SPA then offers a search
// box whose results each install via the existing PluginInstaller flow. ListCatalog is a read (the
// catalog is remote and immutable from the UI's perspective), so it is registered as a GET without the
// read-only guard; the install it feeds is still gated by PluginInstaller (and read-only) separately.
type PluginCatalog interface {
	// ListCatalog returns the catalog entries matching query (an empty query lists the default set).
	// Implementations return a non-nil slice (empty, not nil) so the JSON encodes as [] rather than null.
	ListCatalog(ctx context.Context, query string) ([]CatalogEntry, error)
}

// pluginService returns the backend's PluginService, or false when it does not implement one (the
// routes are only registered when it does, so this is belt-and-suspenders for the handlers).
func (s *Server) pluginService() (PluginService, bool) {
	svc, ok := s.Service.(PluginService)

	return svc, ok
}

// isValidPluginID reports whether name is a usable plugin id: a non-empty single path element (no
// directory separators or relative components), so a crafted id can never reach outside the plugins dir.
func isValidPluginID(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
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
	if !isValidPluginID(name) {
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

	err := requirePluginInstallRequestGuards(request)
	if err != nil {
		status := http.StatusForbidden
		if errors.Is(err, errUnsupportedContentType) {
			status = http.StatusUnsupportedMediaType
		}
		writeError(writer, status, err)

		return
	}

	var req PluginInstallRequest

	err = decodeJSON(writer, request, &req)
	if err != nil {
		return
	}

	if !req.Trusted {
		writeClientError(
			writer,
			fmt.Errorf("%w: plugin install requires explicit trust acknowledgement", ErrInvalid),
		)

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

var errUnsupportedContentType = errors.New("unsupported content type: expected application/json")

func requirePluginInstallRequestGuards(request *http.Request) error {
	contentType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return errUnsupportedContentType
	}

	if isCrossSiteBrowserRequest(request) {
		return errors.New("cross-site plugin install request rejected")
	}

	return nil
}

func isCrossSiteBrowserRequest(request *http.Request) bool {
	switch request.Header.Get("Sec-Fetch-Site") {
	case "cross-site", "none":
		return true
	}

	origin := request.Header.Get("Origin")
	if origin == "" {
		return false
	}

	return origin != "http://"+request.Host && origin != "https://"+request.Host
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
	if !isValidPluginID(name) {
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

// handlePluginCatalog returns the installable-plugin catalog entries matching the `q` query parameter.
// Registered only when the backend implements PluginCatalog; a GET (the catalog is a remote read), so
// the read-only guard does not apply. A lookup failure (e.g. the upstream catalog is unreachable) is
// surfaced as 422 with the message rather than a 500, matching the install handler.
func (s *Server) handlePluginCatalog(writer http.ResponseWriter, request *http.Request) {
	catalog, ok := s.Service.(PluginCatalog)
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	entries, err := catalog.ListCatalog(request.Context(), request.URL.Query().Get("q"))
	if err != nil {
		writeError(writer, http.StatusUnprocessableEntity, err)

		return
	}

	if entries == nil {
		entries = []CatalogEntry{}
	}

	writeJSON(writer, http.StatusOK, map[string]any{"entries": entries})
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
