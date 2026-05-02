package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"golang.org/x/oauth2"
)

const (
	// callbackPath is the HTTP path the OIDC provider redirects to.
	callbackPath = "/callback"
	// shutdownTimeout is the duration to wait for the callback server to shut down.
	shutdownTimeout = 5 * time.Second
	// codeVerifierLength is the byte length of the PKCE code verifier.
	codeVerifierLength = 32
	// stateLength is the byte length of the OIDC state parameter.
	stateLength = 16
	// defaultCallbackPort is the preferred port for the OIDC callback server.
	// A stable port is required because most OIDC providers enforce exact redirect URI matching.
	// Falls back to a random port if the default is already in use.
	defaultCallbackPort = 18000
)

// ErrAuthenticationFailed is returned when the OIDC authentication flow fails.
var ErrAuthenticationFailed = errors.New("OIDC authentication failed")

// ErrUnsupportedPlatform is returned when the runtime OS is not supported for browser opening.
var ErrUnsupportedPlatform = errors.New("unsupported platform")

// TokenResult holds the tokens returned by the OIDC provider.
type TokenResult struct {
	IDToken      string    `json:"idToken"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	Expiry       time.Time `json:"expiry"`
}

// Authenticator handles the OIDC authorization code flow with PKCE.
type Authenticator struct {
	IssuerURL   string
	ClientID    string
	ExtraScopes []string
	CAFile      string
}

// providerResult holds the provider setup shared between Authenticate and RefreshToken.
type providerResult struct {
	oidcCtx      context.Context //nolint:containedctx // passed as bundle to avoid 5-param functions
	provider     *gooidc.Provider
	oauth2Config *oauth2.Config
}

// Authenticate performs the OIDC authorization code flow with PKCE.
// It starts a local HTTP server, opens the browser to the OIDC provider,
// and waits for the callback with the authorization code.
func (a *Authenticator) Authenticate(ctx context.Context) (*TokenResult, error) {
	listener, server, resultCh, err := a.startCallbackServer(ctx)
	if err != nil {
		return nil, err
	}

	defer func() { _ = listener.Close() }()

	go func() {
		serveErr := server.Serve(listener)
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			select {
			case resultCh <- callbackResult{err: fmt.Errorf("%w: callback server error: %w", ErrAuthenticationFailed, serveErr)}:
			default:
			}
		}
	}()

	return a.awaitAuthResult(ctx, server, resultCh)
}

// RefreshToken attempts to refresh an expired token using the refresh token.
func (a *Authenticator) RefreshToken(
	ctx context.Context,
	refreshToken string,
) (*TokenResult, error) {
	prov, err := a.newOIDCProvider(ctx, "")
	if err != nil {
		return nil, err
	}

	//nolint:contextcheck // oidcCtx carries the custom HTTP client for OIDC discovery
	tokenSource := prov.oauth2Config.TokenSource(prov.oidcCtx, &oauth2.Token{
		RefreshToken: refreshToken,
	})

	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("%w: token refresh failed: %w", ErrAuthenticationFailed, err)
	}

	idToken, ok := newToken.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("%w: no id_token in refresh response", ErrAuthenticationFailed)
	}

	return &TokenResult{
		IDToken:      idToken,
		RefreshToken: newToken.RefreshToken,
		Expiry:       newToken.Expiry,
	}, nil
}

// --- internals ---

type callbackResult struct {
	token *TokenResult
	err   error
}

func (a *Authenticator) newOIDCProvider(
	ctx context.Context,
	redirectURL string,
) (*providerResult, error) {
	httpClient, err := a.buildHTTPClient()
	if err != nil {
		return nil, err
	}

	oidcCtx := gooidc.ClientContext(ctx, httpClient)

	provider, err := gooidc.NewProvider(oidcCtx, a.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: failed to discover OIDC provider: %w",
			ErrAuthenticationFailed,
			err,
		)
	}

	scopes := append([]string{gooidc.ScopeOpenID}, a.ExtraScopes...)

	cfg := &oauth2.Config{
		ClientID:    a.ClientID,
		Endpoint:    provider.Endpoint(),
		RedirectURL: redirectURL,
		Scopes:      scopes,
	}

	return &providerResult{
		oidcCtx:      oidcCtx,
		provider:     provider,
		oauth2Config: cfg,
	}, nil
}

func (a *Authenticator) startCallbackServer(
	ctx context.Context,
) (net.Listener, *http.Server, chan callbackResult, error) {
	listener, port, err := a.listenOnRandomPort(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	redirectURL := fmt.Sprintf("http://localhost:%d%s", port, callbackPath)

	prov, err := a.newOIDCProvider(ctx, redirectURL)
	if err != nil {
		_ = listener.Close()

		return nil, nil, nil, err
	}

	codeVerifier, codeChallenge, err := generatePKCE()
	if err != nil {
		_ = listener.Close()

		return nil, nil, nil, err
	}

	state, err := generateState()
	if err != nil {
		_ = listener.Close()

		return nil, nil, nil, err
	}

	resultCh := make(chan callbackResult, 1)
	sendOnce := &sync.Once{}

	mux := http.NewServeMux()
	//nolint:contextcheck // oidcCtx carries the custom HTTP client for OIDC token exchange
	mux.HandleFunc(callbackPath, a.handleCallback(
		prov.oidcCtx, prov.oauth2Config, prov.provider,
		state, codeVerifier, resultCh, sendOnce,
	))

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: shutdownTimeout,
	}

	a.promptBrowserVisit(ctx, prov.oauth2Config, state, codeChallenge)

	return listener, server, resultCh, nil
}

func (a *Authenticator) listenOnRandomPort(ctx context.Context) (net.Listener, int, error) {
	listenCfg := net.ListenConfig{}

	// Use a stable port so the redirect URI is deterministic — OIDC providers
	// require an exact redirect URI match including port.
	listener, err := listenCfg.Listen(ctx, "tcp", fmt.Sprintf("localhost:%d", defaultCallbackPort))
	if err != nil {
		return nil, 0, fmt.Errorf(
			"%w: callback port %d is unavailable — free it or configure your OIDC provider to allow a different port: %w",
			ErrAuthenticationFailed,
			defaultCallbackPort,
			err,
		)
	}

	return listener, defaultCallbackPort, nil
}

func (a *Authenticator) promptBrowserVisit(
	ctx context.Context,
	oauth2Config *oauth2.Config,
	state, codeChallenge string,
) {
	authURL := oauth2Config.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)

	err := openBrowser(ctx, authURL)
	if err != nil {
		_, _ = fmt.Fprintf(
			os.Stderr,
			"Failed to open browser automatically.\nPlease visit: %s\n",
			authURL,
		)
	}
}

func (a *Authenticator) awaitAuthResult(
	ctx context.Context,
	server *http.Server,
	resultCh <-chan callbackResult,
) (*TokenResult, error) {
	select {
	case result := <-resultCh:
		//nolint:contextcheck // shutdownServer deliberately uses background context
		shutdownErr := a.shutdownServer(server)

		if result.err != nil {
			return nil, result.err
		}

		_ = shutdownErr

		return result.token, nil
	case <-ctx.Done():
		//nolint:contextcheck // shutdownServer deliberately uses background context
		_ = a.shutdownServer(server)

		return nil, fmt.Errorf("%w: %w", ErrAuthenticationFailed, ctx.Err())
	}
}

func (a *Authenticator) shutdownServer(server *http.Server) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	err := server.Shutdown(shutdownCtx)
	if err != nil {
		return fmt.Errorf("failed to shutdown callback server: %w", err)
	}

	return nil
}

func (a *Authenticator) handleCallback(
	ctx context.Context,
	oauth2Config *oauth2.Config,
	provider *gooidc.Provider,
	expectedState, codeVerifier string,
	resultCh chan<- callbackResult,
	sendOnce *sync.Once,
) http.HandlerFunc {
	sendResult := func(result callbackResult) {
		sendOnce.Do(func() { resultCh <- result })
	}

	return func(writer http.ResponseWriter, request *http.Request) {
		code, err := validateCallbackRequest(request, expectedState)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			sendResult(callbackResult{err: err})

			return
		}

		result, err := a.exchangeAndVerifyToken(ctx, oauth2Config, provider, code, codeVerifier)
		if err != nil {
			http.Error(
				writer,
				"Token exchange or verification failed",
				http.StatusInternalServerError,
			)
			sendResult(callbackResult{err: err})

			return
		}

		_, _ = fmt.Fprintf(writer, "Authentication successful! You can close this window.")

		sendResult(callbackResult{token: result})
	}
}

// validateCallbackRequest checks the OIDC callback request for errors, state
// mismatch, and the presence of an authorization code.
func validateCallbackRequest(request *http.Request, expectedState string) (string, error) {
	if errMsg := request.URL.Query().Get("error"); errMsg != "" {
		desc := request.URL.Query().Get("error_description")

		return "", fmt.Errorf("%w: %s: %s", ErrAuthenticationFailed, errMsg, desc)
	}

	if request.URL.Query().Get("state") != expectedState {
		return "", fmt.Errorf("%w: state mismatch", ErrAuthenticationFailed)
	}

	code := request.URL.Query().Get("code")
	if code == "" {
		return "", fmt.Errorf("%w: missing authorization code", ErrAuthenticationFailed)
	}

	return code, nil
}

func (a *Authenticator) exchangeAndVerifyToken(
	ctx context.Context,
	oauth2Config *oauth2.Config,
	provider *gooidc.Provider,
	code, codeVerifier string,
) (*TokenResult, error) {
	token, err := oauth2Config.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: token exchange failed: %w", ErrAuthenticationFailed, err)
	}

	idToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("%w: no id_token in token response", ErrAuthenticationFailed)
	}

	verifier := provider.Verifier(&gooidc.Config{ClientID: a.ClientID})

	verified, err := verifier.Verify(ctx, idToken)
	if err != nil {
		return nil, fmt.Errorf("%w: token verification failed: %w", ErrAuthenticationFailed, err)
	}

	return &TokenResult{
		IDToken:      idToken,
		RefreshToken: token.RefreshToken,
		Expiry:       verified.Expiry,
	}, nil
}

func (a *Authenticator) buildHTTPClient() (*http.Client, error) {
	if a.CAFile == "" {
		return http.DefaultClient, nil
	}

	canonicalPath, err := fsutil.EvalCanonicalPath(a.CAFile)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: failed to resolve CA file path: %w",
			ErrAuthenticationFailed,
			err,
		)
	}

	caCert, err := os.ReadFile(canonicalPath) //nolint:gosec // G304: path canonicalized above
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read CA file: %w", ErrAuthenticationFailed, err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("%w: failed to parse CA certificate", ErrAuthenticationFailed)
	}

	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("%w: unexpected default transport type", ErrAuthenticationFailed)
	}

	transport := defaultTransport.Clone()
	transport.TLSClientConfig = &tls.Config{
		RootCAs:    caCertPool,
		MinVersion: tls.VersionTLS12,
	}

	return &http.Client{
		Transport: transport,
	}, nil
}

func generatePKCE() (string, string, error) {
	randomBytes := make([]byte, codeVerifierLength)

	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", "", fmt.Errorf(
			"%w: failed to generate PKCE verifier: %w",
			ErrAuthenticationFailed,
			err,
		)
	}

	verifier := base64.RawURLEncoding.EncodeToString(randomBytes)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])

	return verifier, challenge, nil
}

func generateState() (string, error) {
	randomBytes := make([]byte, stateLength)

	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", fmt.Errorf("%w: failed to generate state: %w", ErrAuthenticationFailed, err)
	}

	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

func openBrowser(ctx context.Context, targetURL string) error {
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.CommandContext( //nolint:gosec // G204: user-visible URL only
			ctx,
			"open",
			targetURL,
		)
		err := cmd.Start()
		if err != nil {
			return fmt.Errorf("failed to open browser: %w", err)
		}

		return nil
	case "linux":
		cmd := exec.CommandContext( //nolint:gosec // G204: user-visible URL only
			ctx,
			"xdg-open",
			targetURL,
		)
		err := cmd.Start()
		if err != nil {
			return fmt.Errorf("failed to open browser: %w", err)
		}

		return nil
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedPlatform, runtime.GOOS)
	}
}

// ExecCredentialJSON generates the ExecCredential JSON output for kubectl.
func ExecCredentialJSON(idToken string, expiry time.Time) ([]byte, error) {
	cred := map[string]any{
		"apiVersion": "client.authentication.k8s.io/v1",
		"kind":       "ExecCredential",
		"status": map[string]any{
			"token":               idToken,
			"expirationTimestamp": expiry.UTC().Format(time.RFC3339),
		},
	}

	data, err := json.Marshal(cred)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ExecCredential: %w", err)
	}

	return data, nil
}
