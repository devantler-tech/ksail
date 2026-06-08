// Package api serves the KSail operator's REST/JSON API over the Cluster custom resource.
// It is a thin layer over the controller-runtime client and is registered as a manager
// Runnable. When read-only mode is enabled, all mutating verbs are rejected server-side so the
// optional web UI cannot diverge from a GitOps-enforced source of truth.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 5 * time.Second
	// maxRequestBodyBytes caps decoded request bodies to guard against oversized payloads.
	maxRequestBodyBytes = 1 << 20 // 1 MiB
)

// errorJSONKey is the JSON object key carrying a human-readable error message in API and SSE error
// responses. The SPA parses both with the same logic, so the key is shared.
const errorJSONKey = "error"

// Server serves the operator REST API. It implements controller-runtime's manager.Runnable.
//
// Authentication is optional and app-driven (see OIDCConfig): when configured, the API owns the
// OIDC login/callback and requires a valid session for all cluster endpoints. When OIDC is not
// configured the API has no built-in authn/z — any caller that can reach the listener inherits the
// operator's RBAC, so keep it cluster-internal (and rely on the read-only lock) or enable OIDC.
// The operator always acts with its own RBAC; OIDC provides authentication, not per-user authz.
type Server struct {
	// Service is the backend the cluster handlers delegate to (controller-runtime-backed in the
	// operator, CLI-lifecycle-backed for `ksail ui`).
	Service ClusterService
	// ReadOnly rejects all mutating requests with HTTP 403 when true.
	ReadOnly bool
	// BindAddress is the address the HTTP server listens on (e.g. ":8080").
	BindAddress string
	// OIDC configures app-driven OIDC authentication; empty IssuerURL disables it.
	OIDC OIDCConfig

	// Distributions lists the distributions the create form should offer. When empty the SPA falls
	// back to its built-in default (VCluster), so the operator can leave it unset.
	Distributions []string

	// ProviderStatus reports which infrastructure providers are usable on this backend (e.g. the
	// local UI gates create options on whether HCLOUD_TOKEN is set, Docker is running, etc.). When
	// nil the SPA does not gate by provider — it offers every provider valid for a distribution (the
	// operator leaves it nil, since it provisions via the cluster CR regardless of local credentials).
	ProviderStatus func(ctx context.Context) []ProviderInfo

	// Settings, when non-nil, enables the credential-settings endpoints and the SPA's Settings page.
	// Only the local UI backend sets it; the operator leaves it nil (credentials are managed
	// in-cluster), so the settings routes are not registered and the Settings page stays hidden.
	Settings SettingsService

	// StaticFS, when non-nil, serves the embedded web UI (SPA) for any route the API does not handle,
	// falling back to index.html for client-side routing. The operator leaves it nil (nginx serves
	// the UI separately); `ksail ui` sets it to the embedded assets.
	StaticFS fs.FS

	// EventsInterval is how often the SSE events stream (GET /api/v1/events) re-checks the backend for
	// changes. Zero selects defaultEventsInterval; tests set it low to observe ticks quickly.
	EventsInterval time.Duration

	// auth is built from OIDC at Start; nil means authentication is disabled.
	auth *authenticator
}

// configResponse describes the deployment mode the SPA needs to render the correct UI.
type configResponse struct {
	ReadOnly        bool           `json:"readOnly"`
	AuthEnabled     bool           `json:"authEnabled"`
	User            *userInfo      `json:"user,omitempty"`
	Distributions   []string       `json:"distributions,omitempty"`
	Providers       []ProviderInfo `json:"providers,omitempty"`
	SettingsEnabled bool           `json:"settingsEnabled,omitempty"`
	// Capabilities reports which optional operations the serving backend supports so the SPA can gate
	// affordances (e.g. cluster edit) it cannot fulfill. Always present (no omitempty): an absent
	// field would force the SPA to guess, and the false zero-value is meaningful.
	Capabilities Capabilities `json:"capabilities"`
}

// ProviderInfo reports whether an infrastructure provider is usable on the serving backend, with a
// human-readable reason when it is not. The SPA uses it to gate the create form's provider options.
type ProviderInfo struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// userInfo is the authenticated identity surfaced to the SPA.
type userInfo struct {
	Subject string `json:"subject"`
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`
}

// fullCluster serializes a Cluster with its complete field values. v1alpha1.Cluster.MarshalJSON
// prunes fields that equal their defaults (e.g. distribution=Vanilla, provider=Docker) to keep
// config files clean; a UI-facing API instead needs those values so the web UI can display them.
// Defining a distinct type drops the custom MarshalJSON, falling back to struct-tag marshaling.
type fullCluster v1alpha1.Cluster

// fullClusterList is the list response shape the SPA consumes ({"items":[...]}), with each item
// serialized in full (see fullCluster).
type fullClusterList struct {
	Items []fullCluster `json:"items"`
}

func toFullClusterList(list *v1alpha1.ClusterList) fullClusterList {
	items := make([]fullCluster, 0, len(list.Items))
	for index := range list.Items {
		items = append(items, fullCluster(list.Items[index]))
	}

	return fullClusterList{Items: items}
}

// NeedLeaderElection reports that the API server runs on every replica, not only the leader,
// so reads remain available on standby replicas.
func (s *Server) NeedLeaderElection() bool {
	return false
}

// Start runs the HTTP server on BindAddress until the context is cancelled. It satisfies
// controller-runtime's manager.Runnable for the operator.
func (s *Server) Start(ctx context.Context) error {
	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(ctx, "tcp", s.BindAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.BindAddress, err)
	}

	return s.Serve(ctx, listener)
}

// Serve runs the HTTP server on the supplied listener until the context is cancelled. Binding the
// listener separately lets callers (e.g. `ksail ui`) discover the chosen port before serving
// when port 0 is requested.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	if s.OIDC.Enabled() && s.auth == nil {
		auth, err := newAuthenticator(ctx, s.OIDC)
		if err != nil {
			return fmt.Errorf("set up OIDC authentication: %w", err)
		}

		s.auth = auth
	}

	server := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	go func() {
		<-ctx.Done()

		// Derive from ctx (without cancellation, since it is already cancelled) so shutdown
		// keeps a bounded deadline while remaining linked to the parent context values.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()

		_ = server.Shutdown(shutdownCtx)
	}()

	logf.FromContext(ctx).
		Info("starting KSail API server", "address", listener.Addr().String(), "readOnly", s.ReadOnly)

	err := server.Serve(listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("api server: %w", err)
	}

	return nil
}

// Handler builds the HTTP routes wrapped in the read-only guard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleHealth)
	mux.HandleFunc("GET /api/v1/config", s.handleConfig)
	mux.HandleFunc("GET /api/v1/meta", s.handleMeta)
	mux.HandleFunc("GET /api/v1/clusters", s.handleListClusters)
	mux.HandleFunc("GET /api/v1/events", s.handleEvents)
	mux.HandleFunc("POST /api/v1/clusters", s.handleCreateCluster)
	mux.HandleFunc("GET /api/v1/clusters/{namespace}/{name}", s.handleGetCluster)
	mux.HandleFunc("PUT /api/v1/clusters/{namespace}/{name}", s.handleUpdateCluster)
	mux.HandleFunc("DELETE /api/v1/clusters/{namespace}/{name}", s.handleDeleteCluster)

	s.registerCapabilityRoutes(mux)

	// Credential settings are local-UI-only: registered only when a SettingsService is provided, so
	// the operator's API surface is unchanged.
	if s.Settings != nil {
		mux.HandleFunc("GET /api/v1/settings", s.handleGetSettings)
		mux.HandleFunc("PUT /api/v1/settings", s.handleUpdateSettings)
	}

	if s.auth != nil {
		mux.HandleFunc("GET /api/v1/auth/login", s.auth.handleLogin)
		mux.HandleFunc("GET /api/v1/auth/callback", s.auth.handleCallback)
		mux.HandleFunc("POST /api/v1/auth/logout", s.auth.handleLogout)
	}

	// Serve the embedded SPA on every route the API does not handle (more specific patterns above
	// take precedence). Only wired when StaticFS is set, so the operator's API-only mode is unchanged.
	if s.StaticFS != nil {
		mux.Handle("GET /", spaFileServer(s.StaticFS))
	}

	return securityHeaders(s.authGuard(s.readOnlyGuard(mux)))
}

// registerCapabilityRoutes wires the optional endpoints whose presence is gated on the backend
// implementing the matching interface (so the operator's API-only surface is unchanged). Each block's
// routes are only reachable when the corresponding capability is advertised in /api/v1/config.
func (s *Server) registerCapabilityRoutes(mux *http.ServeMux) {
	// Read-only resource browser (ResourceService). These patterns are strictly more specific than the
	// cluster get/update/delete routes, so the mux routes them without conflict.
	if _, ok := s.Service.(ResourceService); ok {
		mux.HandleFunc("GET /api/v1/clusters/{namespace}/{name}/resources", s.handleListResources)
		mux.HandleFunc(
			"GET /api/v1/clusters/{namespace}/{name}/resources/{kind}/{rname}",
			s.handleGetResource,
		)
	}

	// Safe write actions (scale, rollout restart, delete) on browsable resources (ResourceWriter).
	// Mutating verbs, so the read-only guard rejects them with 403 when the UI is read-only.
	if _, ok := s.Service.(ResourceWriter); ok {
		mux.HandleFunc(
			"PUT /api/v1/clusters/{namespace}/{name}/resources/{kind}/{rname}/scale",
			s.handleScaleResource,
		)
		mux.HandleFunc(
			"POST /api/v1/clusters/{namespace}/{name}/resources/{kind}/{rname}/restart",
			s.handleRestartResource,
		)
		mux.HandleFunc(
			"DELETE /api/v1/clusters/{namespace}/{name}/resources/{kind}/{rname}",
			s.handleDeleteResource,
		)
		mux.HandleFunc(
			"POST /api/v1/clusters/{namespace}/{name}/resources/{kind}/{rname}/reconcile",
			s.handleReconcileResource,
		)
	}

	// Kubeconfig export (KubeconfigProvider). A read (GET), so the read-only guard does not apply.
	if _, ok := s.Service.(KubeconfigProvider); ok {
		mux.HandleFunc("GET /api/v1/clusters/{namespace}/{name}/kubeconfig", s.handleKubeconfig)
	}

	// Manifest apply (ApplyService). A mutating verb, so the read-only guard rejects it in read-only.
	if _, ok := s.Service.(ApplyService); ok {
		mux.HandleFunc("POST /api/v1/clusters/{namespace}/{name}/apply", s.handleApply)
	}

	// SOPS secret cipher with local age keys (CipherService). Cluster-independent local crypto.
	// encrypt/decrypt are POST, so the read-only guard rejects them in read-only mode. This is
	// intentional: a read-only deployment locks down secret operations entirely — including decrypt,
	// which would otherwise reveal plaintext. The local `ksail ui`/desktop backend runs writable, so
	// this only bites a deliberately read-only deployment that also opts into the cipher service.
	if _, ok := s.Service.(CipherService); ok {
		mux.HandleFunc("GET /api/v1/secrets/recipients", s.handleCipherRecipients)
		mux.HandleFunc("POST /api/v1/secrets/encrypt", s.handleSecretEncrypt)
		mux.HandleFunc("POST /api/v1/secrets/decrypt", s.handleSecretDecrypt)
	}

	// Pod log streaming (LogService) over SSE — a GET, read-only, so it is not gated by the read-only
	// guard (logs don't mutate). Registered only when the backend implements LogService.
	if _, ok := s.Service.(LogService); ok {
		mux.HandleFunc("GET /api/v1/clusters/{namespace}/{name}/logs", s.handleLogs)
	}
}

// securityHeaders applies conservative security headers to every response. The CSP allows only
// same-origin resources (the SPA's bundled JS/CSS and the theme-init script are same-origin and it
// makes no cross-origin requests); 'unsafe-inline' is permitted for styles only, which React inline
// style attributes and the bundled stylesheet require.
func securityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; " +
		"object-src 'none'; base-uri 'self'; frame-ancestors 'none'"

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		header := writer.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Content-Security-Policy", csp)

		next.ServeHTTP(writer, request)
	})
}

// uiNotBuiltMessage is served when StaticFS is set but the SPA assets were never built (only the
// embed placeholder is present).
const uiNotBuiltMessage = `<!doctype html><html><head><meta charset="utf-8">` +
	`<title>KSail UI</title></head><body style="font-family:sans-serif;padding:2rem">` +
	`<h1>KSail web UI not built</h1>` +
	`<p>Run <code>make ui</code> and rebuild the binary to embed the web UI.</p>` +
	`</body></html>`

// spaFileServer serves files from fsys, falling back to index.html for unknown paths so the SPA can
// own client-side routing (mirrors the nginx try_files behavior used by the operator's UI image).
func spaFileServer(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))

	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		index = []byte(uiNotBuiltMessage)
	}

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		name := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if name != "" {
			_, statErr := fs.Stat(fsys, name)
			if statErr == nil {
				fileServer.ServeHTTP(writer, request)

				return
			}
		}

		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write(index)
	})
}

// authGuard requires a valid session for cluster endpoints when OIDC is enabled. Health checks,
// the config endpoint, and the auth flow itself remain open so the SPA can detect the mode and log
// in. When OIDC is disabled the guard is a pass-through.
func (s *Server) authGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if s.auth == nil || isOpenPath(request.URL.Path) {
			next.ServeHTTP(writer, request)

			return
		}

		_, ok := s.auth.currentUser(request)
		if !ok {
			writeJSON(writer, http.StatusUnauthorized, map[string]any{
				errorJSONKey: "authentication required",
				"loginURL":   loginPath,
			})

			return
		}

		next.ServeHTTP(writer, request)
	})
}

// isOpenPath reports whether a path is reachable without an authenticated session. Cluster API
// paths stay guarded; everything else (health, config, meta, the auth flow, and the SPA's static
// assets/client routes) is open so the login screen and its assets load before a session exists —
// the SPA then discovers auth state via the (open) /api/v1/config endpoint.
func isOpenPath(path string) bool {
	switch path {
	case "/healthz", "/readyz", "/api/v1/config", "/api/v1/meta":
		return true
	}

	if strings.HasPrefix(path, "/api/v1/auth/") {
		return true
	}

	// Non-API paths are SPA static assets / client-side routes; serve them unauthenticated.
	return !strings.HasPrefix(path, "/api/")
}

// readOnlyGuard rejects mutating cluster requests with 403 when the server is in read-only mode.
// Auth endpoints (e.g. POST /api/v1/auth/logout) are exempt: read-only constrains cluster
// mutations, not session management, so users must still be able to log out.
func (s *Server) readOnlyGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		isAuthPath := strings.HasPrefix(request.URL.Path, "/api/v1/auth/")
		if s.ReadOnly && isMutating(request.Method) && !isAuthPath {
			writeJSON(writer, http.StatusForbidden, map[string]any{
				"readOnly": true,
				"reason":   "UI is configured read-only (GitOps-enforced)",
			})

			return
		}

		next.ServeHTTP(writer, request)
	})
}

func (s *Server) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleConfig(writer http.ResponseWriter, request *http.Request) {
	capabilities := serviceCapabilities(s.Service)
	// workloadRead is true exactly when the resource endpoints are registered (the backend implements
	// ResourceService), so the SPA's gate can never diverge from whether the routes exist.
	_, capabilities.WorkloadRead = s.Service.(ResourceService)
	_, capabilities.WorkloadWrite = s.Service.(ResourceWriter)
	_, capabilities.KubeconfigDownload = s.Service.(KubeconfigProvider)
	_, capabilities.ApplyManifests = s.Service.(ApplyService)
	_, capabilities.SecretsCipher = s.Service.(CipherService)
	_, capabilities.WorkloadLogs = s.Service.(LogService)

	response := configResponse{
		ReadOnly:        s.ReadOnly,
		AuthEnabled:     s.auth != nil,
		Distributions:   s.Distributions,
		SettingsEnabled: s.Settings != nil,
		Capabilities:    capabilities,
	}

	if s.ProviderStatus != nil {
		response.Providers = s.ProviderStatus(request.Context())
	}

	if s.auth != nil {
		claims, ok := s.auth.currentUser(request)
		if ok {
			response.User = &userInfo{
				Subject: claims.Subject,
				Email:   claims.Email,
				Name:    claims.Name,
			}
		}
	}

	writeJSON(writer, http.StatusOK, response)
}

func (s *Server) handleListClusters(writer http.ResponseWriter, request *http.Request) {
	list, err := s.Service.List(request.Context())
	if err != nil {
		writeClientError(writer, err)

		return
	}

	writeJSON(writer, http.StatusOK, toFullClusterList(list))
}

func (s *Server) handleGetCluster(writer http.ResponseWriter, request *http.Request) {
	cluster, err := s.Service.Get(
		request.Context(),
		request.PathValue("namespace"),
		request.PathValue("name"),
	)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	writeJSON(writer, http.StatusOK, (*fullCluster)(cluster))
}

func (s *Server) handleCreateCluster(writer http.ResponseWriter, request *http.Request) {
	decoded, err := decodeCluster(writer, request)
	if err != nil {
		writeDecodeError(writer, err)

		return
	}

	created, createErr := s.Service.Create(request.Context(), decoded)
	if createErr != nil {
		writeClientError(writer, createErr)

		return
	}

	writeJSON(writer, http.StatusCreated, (*fullCluster)(created))
}

func (s *Server) handleUpdateCluster(writer http.ResponseWriter, request *http.Request) {
	decoded, err := decodeCluster(writer, request)
	if err != nil {
		writeDecodeError(writer, err)

		return
	}

	updated, updateErr := s.Service.Update(
		request.Context(),
		request.PathValue("namespace"),
		request.PathValue("name"),
		decoded,
	)
	if updateErr != nil {
		writeClientError(writer, updateErr)

		return
	}

	writeJSON(writer, http.StatusOK, (*fullCluster)(updated))
}

func (s *Server) handleDeleteCluster(writer http.ResponseWriter, request *http.Request) {
	err := s.Service.Delete(
		request.Context(),
		request.PathValue("namespace"),
		request.PathValue("name"),
	)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	writer.WriteHeader(http.StatusNoContent)
}

// resourceService returns the backend's ResourceService, or false when it does not implement one
// (the routes are only registered when it does, so this is belt-and-suspenders).
func (s *Server) resourceService() (ResourceService, bool) {
	svc, ok := s.Service.(ResourceService)

	return svc, ok
}

func (s *Server) handleListResources(writer http.ResponseWriter, request *http.Request) {
	svc, ok := s.resourceService()
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	list, err := svc.ListResources(
		request.Context(),
		request.PathValue("namespace"),
		request.PathValue("name"),
		ResourceQuery{
			Kind:      request.URL.Query().Get("kind"),
			Namespace: request.URL.Query().Get("namespace"),
		},
	)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	writeJSON(writer, http.StatusOK, list)
}

func (s *Server) handleGetResource(writer http.ResponseWriter, request *http.Request) {
	svc, ok := s.resourceService()
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	obj, err := svc.GetResource(
		request.Context(),
		request.PathValue("namespace"),
		request.PathValue("name"),
		resourceRefFrom(request),
	)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	writeJSON(writer, http.StatusOK, obj)
}

// resourceRefFrom builds a ResourceRef from the request's path values and namespace query param.
func resourceRefFrom(request *http.Request) ResourceRef {
	return ResourceRef{
		Kind:      request.PathValue("kind"),
		Namespace: request.URL.Query().Get("namespace"),
		Name:      request.PathValue("rname"),
	}
}

func (s *Server) resourceWriter() (ResourceWriter, bool) {
	svc, ok := s.Service.(ResourceWriter)

	return svc, ok
}

func (s *Server) handleScaleResource(writer http.ResponseWriter, request *http.Request) {
	svc, ok := s.resourceWriter()
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	var scale ScaleRequest

	limited := http.MaxBytesReader(writer, request.Body, maxRequestBodyBytes)

	err := json.NewDecoder(limited).Decode(&scale)
	if err != nil {
		writeDecodeError(writer, fmt.Errorf("decode scale request: %w", err))

		return
	}

	err = svc.ScaleResource(
		request.Context(),
		request.PathValue("namespace"),
		request.PathValue("name"),
		resourceRefFrom(request),
		scale.Replicas,
	)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	writer.WriteHeader(http.StatusNoContent)
}

// runResourceWrite invokes a no-body ResourceWriter action (rollout restart, delete) and writes 204 —
// mapping a backend without ResourceWriter to 501 and a service error to its client status. Shared by
// the restart/delete handlers; handleScaleResource is separate because it first decodes a body.
func (s *Server) runResourceWrite(
	writer http.ResponseWriter,
	request *http.Request,
	invoke func(svc ResourceWriter, ctx context.Context, namespace, name string, ref ResourceRef) error,
) {
	svc, ok := s.resourceWriter()
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	err := invoke(
		svc,
		request.Context(),
		request.PathValue("namespace"),
		request.PathValue("name"),
		resourceRefFrom(request),
	)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	writer.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRestartResource(writer http.ResponseWriter, request *http.Request) {
	s.runResourceWrite(
		writer,
		request,
		func(svc ResourceWriter, ctx context.Context, namespace, name string, ref ResourceRef) error {
			return svc.RestartResource(ctx, namespace, name, ref)
		},
	)
}

func (s *Server) handleReconcileResource(writer http.ResponseWriter, request *http.Request) {
	s.runResourceWrite(
		writer,
		request,
		func(svc ResourceWriter, ctx context.Context, namespace, name string, ref ResourceRef) error {
			return svc.ReconcileResource(ctx, namespace, name, ref)
		},
	)
}

func (s *Server) handleDeleteResource(writer http.ResponseWriter, request *http.Request) {
	s.runResourceWrite(
		writer,
		request,
		func(svc ResourceWriter, ctx context.Context, namespace, name string, ref ResourceRef) error {
			return svc.DeleteResource(ctx, namespace, name, ref)
		},
	)
}

func (s *Server) handleKubeconfig(writer http.ResponseWriter, request *http.Request) {
	provider, ok := s.Service.(KubeconfigProvider)
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	name := request.PathValue("name")

	kubeconfig, err := provider.Kubeconfig(request.Context(), request.PathValue("namespace"), name)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	filename := name + ".kubeconfig"

	// Served as a non-HTML attachment with nosniff so the browser downloads it and never renders the
	// bytes as a document. ServeContent (not a raw Write) handles the body, honouring the Content-Type
	// set here rather than sniffing it.
	writer.Header().Set("Content-Type", "application/yaml")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	http.ServeContent(writer, request, filename, time.Time{}, bytes.NewReader(kubeconfig))
}

func (s *Server) handleApply(writer http.ResponseWriter, request *http.Request) {
	svc, ok := s.Service.(ApplyService)
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	limited := http.MaxBytesReader(writer, request.Body, maxRequestBodyBytes)

	manifests, err := io.ReadAll(limited)
	if err != nil {
		writeDecodeError(writer, fmt.Errorf("read manifests: %w", err))

		return
	}

	dryRun := request.URL.Query().Get("dryRun") == "true"

	results, err := svc.ApplyManifests(
		request.Context(),
		request.PathValue("namespace"),
		request.PathValue("name"),
		manifests,
		dryRun,
	)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	writeJSON(writer, http.StatusOK, map[string]any{"results": results, "dryRun": dryRun})
}

func (s *Server) handleCipherRecipients(writer http.ResponseWriter, request *http.Request) {
	svc, ok := s.Service.(CipherService)
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	recipients, err := svc.CipherRecipients(request.Context())
	if err != nil {
		writeClientError(writer, err)

		return
	}

	if recipients == nil {
		recipients = []string{}
	}

	writeJSON(writer, http.StatusOK, map[string]any{"recipients": recipients})
}

func (s *Server) handleSecretEncrypt(writer http.ResponseWriter, request *http.Request) {
	svc, ok := s.Service.(CipherService)
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	var req SecretEncryptRequest

	err := decodeJSON(writer, request, &req)
	if err != nil {
		return
	}

	encrypted, err := svc.EncryptSecret(request.Context(), req.Plaintext, req.Recipient, req.Format)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	writeJSON(writer, http.StatusOK, map[string]any{"encrypted": encrypted})
}

func (s *Server) handleSecretDecrypt(writer http.ResponseWriter, request *http.Request) {
	svc, ok := s.Service.(CipherService)
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	var req SecretDecryptRequest

	err := decodeJSON(writer, request, &req)
	if err != nil {
		return
	}

	plaintext, err := svc.DecryptSecret(request.Context(), req.Encrypted, req.Format)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	writeJSON(writer, http.StatusOK, map[string]any{"plaintext": plaintext})
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// decodeJSON decodes a size-capped JSON request body into v. On failure it writes the appropriate
// error response (413 if oversized, else 400) and returns the error so the handler returns early.
func decodeJSON(writer http.ResponseWriter, request *http.Request, value any) error {
	limited := http.MaxBytesReader(writer, request.Body, maxRequestBodyBytes)

	err := json.NewDecoder(limited).Decode(value)
	if err != nil {
		wrapped := fmt.Errorf("decode request: %w", err)
		writeDecodeError(writer, wrapped)

		return wrapped
	}

	return nil
}

func decodeCluster(writer http.ResponseWriter, request *http.Request) (*v1alpha1.Cluster, error) {
	var cluster v1alpha1.Cluster

	// Cap the request body to guard against oversized payloads when exposed via Ingress.
	limited := http.MaxBytesReader(writer, request.Body, maxRequestBodyBytes)

	err := json.NewDecoder(limited).Decode(&cluster)
	if err != nil {
		return nil, fmt.Errorf("decode cluster: %w", err)
	}

	return &cluster, nil
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	data, err := json.Marshal(body)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)

		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)

	_, _ = writer.Write(data)
}

func writeError(writer http.ResponseWriter, status int, err error) {
	writeJSON(writer, status, map[string]string{errorJSONKey: err.Error()})
}

// writeDecodeError maps a request-body decode failure to the most appropriate status: 413 when the
// body exceeded maxRequestBodyBytes, otherwise 400 for malformed JSON.
func writeDecodeError(writer http.ResponseWriter, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		writeError(writer, http.StatusRequestEntityTooLarge, err)

		return
	}

	writeError(writer, http.StatusBadRequest, err)
}

func writeClientError(writer http.ResponseWriter, err error) {
	writeError(writer, clientErrorStatus(err), err)
}

// clientErrorStatus maps a backend error to the closest HTTP status code so clients and the UI can
// react appropriately instead of treating everything as a 500. It recognizes both the ClusterService
// sentinels (returned by the local CLI backend) and Kubernetes apierrors (returned by the operator's
// controller-runtime backend).
func clientErrorStatus(err error) int {
	switch {
	case errors.Is(err, ErrNotFound), apierrors.IsNotFound(err):
		return http.StatusNotFound
	case errors.Is(err, ErrAlreadyExists),
		apierrors.IsConflict(err),
		apierrors.IsAlreadyExists(err):
		return http.StatusConflict
	case errors.Is(err, ErrInvalid), apierrors.IsInvalid(err):
		return http.StatusUnprocessableEntity
	case errors.Is(err, ErrNotSupported):
		return http.StatusNotImplemented
	case apierrors.IsBadRequest(err):
		return http.StatusBadRequest
	case apierrors.IsUnauthorized(err):
		return http.StatusUnauthorized
	case apierrors.IsForbidden(err):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}
