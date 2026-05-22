// Package api serves the KSail operator's REST/JSON API over the Cluster custom resource.
// It is a thin layer over the controller-runtime client and is registered as a manager
// Runnable. When read-only mode is enabled, all mutating verbs are rejected server-side so the
// optional web UI cannot diverge from a GitOps-enforced source of truth.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 5 * time.Second
	// maxRequestBodyBytes caps decoded request bodies to guard against oversized payloads.
	maxRequestBodyBytes = 1 << 20 // 1 MiB
	// defaultNamespace is used when a create request does not specify a namespace.
	defaultNamespace = "default"
	// lastAppliedSpecAnnotation is the operator-managed drift baseline annotation
	// (controller.LastAppliedSpecAnnotation); the API strips it from client input.
	lastAppliedSpecAnnotation = "ksail.io/last-applied-spec"
)

// Server serves the operator REST API. It implements controller-runtime's manager.Runnable.
//
// Authentication is optional and app-driven (see OIDCConfig): when configured, the API owns the
// OIDC login/callback and requires a valid session for all cluster endpoints. When OIDC is not
// configured the API has no built-in authn/z — any caller that can reach the listener inherits the
// operator's RBAC, so keep it cluster-internal (and rely on the read-only lock) or enable OIDC.
// The operator always acts with its own RBAC; OIDC provides authentication, not per-user authz.
type Server struct {
	// Client is the controller-runtime client used to read and mutate Cluster resources.
	Client client.Client
	// ReadOnly rejects all mutating requests with HTTP 403 when true.
	ReadOnly bool
	// BindAddress is the address the HTTP server listens on (e.g. ":8080").
	BindAddress string
	// OIDC configures app-driven OIDC authentication; empty IssuerURL disables it.
	OIDC OIDCConfig
	// UIFS is the embedded web UI served at the root path. Nil disables UI serving (API only),
	// so the SPA and the API share one origin and no reverse proxy is needed.
	UIFS fs.FS

	// auth is built from OIDC at Start; nil means authentication is disabled.
	auth *authenticator
}

// configResponse describes the deployment mode the SPA needs to render the correct UI.
type configResponse struct {
	ReadOnly    bool      `json:"readOnly"`
	AuthEnabled bool      `json:"authEnabled"`
	User        *userInfo `json:"user,omitempty"`
}

// userInfo is the authenticated identity surfaced to the SPA.
type userInfo struct {
	Subject string `json:"subject"`
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`
}

// NeedLeaderElection reports that the API server runs on every replica, not only the leader,
// so reads remain available on standby replicas.
func (s *Server) NeedLeaderElection() bool {
	return false
}

// Start runs the HTTP server until the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	if s.OIDC.Enabled() && s.auth == nil {
		auth, err := newAuthenticator(ctx, s.OIDC)
		if err != nil {
			return fmt.Errorf("set up OIDC authentication: %w", err)
		}

		s.auth = auth
	}

	server := &http.Server{
		Addr:              s.BindAddress,
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
		Info("starting operator API server", "address", s.BindAddress, "readOnly", s.ReadOnly)

	err := server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("operator API server: %w", err)
	}

	return nil
}

// Handler builds the HTTP routes. The API is wrapped in the auth and read-only guards; when a UI
// is embedded it is served unguarded at the root path so the login screen and its assets load
// before authentication. Security headers are applied to every response.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleHealth)
	mux.HandleFunc("GET /api/v1/config", s.handleConfig)
	mux.HandleFunc("GET /api/v1/clusters", s.handleListClusters)
	mux.HandleFunc("POST /api/v1/clusters", s.handleCreateCluster)
	mux.HandleFunc("GET /api/v1/clusters/{namespace}/{name}", s.handleGetCluster)
	mux.HandleFunc("PUT /api/v1/clusters/{namespace}/{name}", s.handleUpdateCluster)
	mux.HandleFunc("DELETE /api/v1/clusters/{namespace}/{name}", s.handleDeleteCluster)

	if s.auth != nil {
		mux.HandleFunc("GET /api/v1/auth/login", s.auth.handleLogin)
		mux.HandleFunc("GET /api/v1/auth/callback", s.auth.handleCallback)
		mux.HandleFunc("POST /api/v1/auth/logout", s.auth.handleLogout)
	}

	guardedAPI := s.authGuard(s.readOnlyGuard(mux))

	var handler http.Handler = guardedAPI
	if s.UIFS != nil {
		handler = s.uiOrAPI(guardedAPI)
	}

	return securityHeaders(handler)
}

// uiOrAPI routes API and health paths to the guarded API and everything else to the embedded SPA.
// The SPA is served outside the auth guard so the login screen and its assets load even before a
// session exists; the SPA then discovers auth state via the (open) /api/v1/config endpoint.
func (s *Server) uiOrAPI(api http.Handler) http.Handler {
	spa := s.spaHandler()

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if isAPIPath(request.URL.Path) {
			api.ServeHTTP(writer, request)

			return
		}

		spa.ServeHTTP(writer, request)
	})
}

// isAPIPath reports whether a request path is served by the REST API rather than the UI.
func isAPIPath(requestPath string) bool {
	return requestPath == "/healthz" ||
		requestPath == "/readyz" ||
		strings.HasPrefix(requestPath, "/api/")
}

// spaHandler serves the embedded SPA, falling back to index.html for unknown paths so client-side
// routing works. Hashed assets under /assets/ are marked immutable; index.html is never cached so
// new deployments take effect immediately.
func (s *Server) spaHandler() http.Handler {
	fileServer := http.FileServer(http.FS(s.UIFS))

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		name := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}

		info, err := fs.Stat(s.UIFS, name)
		if err != nil || info.IsDir() {
			// Unknown path: serve the SPA entry point so the client router (not the server) resolves it.
			request = request.Clone(request.Context())
			request.URL.Path = "/"
		}

		if strings.HasPrefix(request.URL.Path, "/assets/") {
			writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			writer.Header().Set("Cache-Control", "no-cache")
		}

		fileServer.ServeHTTP(writer, request)
	})
}

// securityHeaders applies conservative security headers to every response. The CSP allows only
// same-origin resources (the SPA's bundled JS/CSS are same-origin and it makes no cross-origin
// requests); 'unsafe-inline' is permitted for styles only, which React inline style attributes and
// the bundled stylesheet require.
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
				"error":    "authentication required",
				"loginURL": loginPath,
			})

			return
		}

		next.ServeHTTP(writer, request)
	})
}

// isOpenPath reports whether a path is reachable without an authenticated session.
func isOpenPath(path string) bool {
	switch path {
	case "/healthz", "/readyz", "/api/v1/config":
		return true
	default:
		return strings.HasPrefix(path, "/api/v1/auth/")
	}
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
	response := configResponse{ReadOnly: s.ReadOnly, AuthEnabled: s.auth != nil}

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
	var list v1alpha1.ClusterList

	err := s.Client.List(request.Context(), &list)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)

		return
	}

	// Emit an empty array rather than null for items when there are no clusters,
	// matching Kubernetes list semantics so clients don't have to special-case null.
	if list.Items == nil {
		list.Items = []v1alpha1.Cluster{}
	}

	writeJSON(writer, http.StatusOK, &list)
}

func (s *Server) handleGetCluster(writer http.ResponseWriter, request *http.Request) {
	var cluster v1alpha1.Cluster

	key := types.NamespacedName{
		Namespace: request.PathValue("namespace"),
		Name:      request.PathValue("name"),
	}

	err := s.Client.Get(request.Context(), key, &cluster)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	writeJSON(writer, http.StatusOK, &cluster)
}

func (s *Server) handleCreateCluster(writer http.ResponseWriter, request *http.Request) {
	decoded, err := decodeCluster(writer, request)
	if err != nil {
		writeDecodeError(writer, err)

		return
	}

	cluster := sanitizeForWrite(decoded)
	if cluster.Namespace == "" {
		cluster.Namespace = defaultNamespace
	}

	createErr := s.Client.Create(request.Context(), cluster)
	if createErr != nil {
		writeClientError(writer, createErr)

		return
	}

	writeJSON(writer, http.StatusCreated, cluster)
}

func (s *Server) handleUpdateCluster(writer http.ResponseWriter, request *http.Request) {
	decoded, err := decodeCluster(writer, request)
	if err != nil {
		writeDecodeError(writer, err)

		return
	}

	key := types.NamespacedName{
		Namespace: request.PathValue("namespace"),
		Name:      request.PathValue("name"),
	}

	// Fetch the existing object so the update carries the current resourceVersion and preserves
	// server- and operator-managed fields (status, finalizers, operator annotations). Only the
	// client-mutable spec is applied.
	var existing v1alpha1.Cluster

	getErr := s.Client.Get(request.Context(), key, &existing)
	if getErr != nil {
		writeClientError(writer, getErr)

		return
	}

	existing.Spec = decoded.Spec

	updateErr := s.Client.Update(request.Context(), &existing)
	if updateErr != nil {
		writeClientError(writer, updateErr)

		return
	}

	writeJSON(writer, http.StatusOK, &existing)
}

func (s *Server) handleDeleteCluster(writer http.ResponseWriter, request *http.Request) {
	cluster := &v1alpha1.Cluster{}
	cluster.Namespace = request.PathValue("namespace")
	cluster.Name = request.PathValue("name")

	err := s.Client.Delete(request.Context(), cluster)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	writer.WriteHeader(http.StatusNoContent)
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
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

// sanitizeForWrite returns a copy of a client-supplied Cluster containing only the fields a caller
// is allowed to set (name, namespace, labels, spec). It drops status, finalizers, resourceVersion,
// and the operator-managed last-applied-spec annotation so the API cannot be used to interfere with
// reconciliation or drift detection.
func sanitizeForWrite(cluster *v1alpha1.Cluster) *v1alpha1.Cluster {
	out := &v1alpha1.Cluster{}
	out.Name = cluster.Name
	out.Namespace = cluster.Namespace
	out.Labels = cluster.Labels
	out.Spec = cluster.Spec

	if len(cluster.Annotations) > 0 {
		annotations := make(map[string]string, len(cluster.Annotations))

		for key, value := range cluster.Annotations {
			if key == lastAppliedSpecAnnotation {
				continue
			}

			annotations[key] = value
		}

		if len(annotations) > 0 {
			out.Annotations = annotations
		}
	}

	return out
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
	writeJSON(writer, status, map[string]string{"error": err.Error()})
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

// clientErrorStatus maps a Kubernetes API error to the closest HTTP status code so clients and the
// UI can react appropriately instead of treating everything as a 500.
func clientErrorStatus(err error) int {
	switch {
	case apierrors.IsNotFound(err):
		return http.StatusNotFound
	case apierrors.IsConflict(err), apierrors.IsAlreadyExists(err):
		return http.StatusConflict
	case apierrors.IsInvalid(err):
		return http.StatusUnprocessableEntity
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
