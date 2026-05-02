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
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
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
)

// ErrAuthenticationFailed is returned when the OIDC authentication flow fails.
var ErrAuthenticationFailed = errors.New("OIDC authentication failed")

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

// Authenticate performs the OIDC authorization code flow with PKCE.
// It starts a local HTTP server, opens the browser to the OIDC provider,
// and waits for the callback with the authorization code.
func (a *Authenticator) Authenticate(ctx context.Context) (*TokenResult, error) {
	httpClient, err := a.buildHTTPClient()
	if err != nil {
		return nil, err
	}

	oidcCtx := gooidc.ClientContext(ctx, httpClient)

	provider, err := gooidc.NewProvider(oidcCtx, a.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to discover OIDC provider: %w", ErrAuthenticationFailed, err)
	}

	// Start local callback server on a random port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("%w: failed to start callback server: %w", ErrAuthenticationFailed, err)
	}
	defer listener.Close()

	redirectURL := fmt.Sprintf("http://localhost:%d%s", listener.Addr().(*net.TCPAddr).Port, callbackPath)

	scopes := append([]string{gooidc.ScopeOpenID}, a.ExtraScopes...)

	oauth2Config := &oauth2.Config{
		ClientID:    a.ClientID,
		Endpoint:    provider.Endpoint(),
		RedirectURL: redirectURL,
		Scopes:      scopes,
	}

	// Generate PKCE code verifier and challenge
	codeVerifier, codeChallenge, err := generatePKCE()
	if err != nil {
		return nil, err
	}

	state, err := generateState()
	if err != nil {
		return nil, err
	}

	// Channel to receive the result from the callback handler
	resultCh := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, a.handleCallback(oidcCtx, oauth2Config, provider, state, codeVerifier, resultCh))

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: shutdownTimeout,
	}

	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			resultCh <- callbackResult{err: fmt.Errorf("%w: callback server error: %w", ErrAuthenticationFailed, serveErr)}
		}
	}()

	// Build and open the authorization URL
	authURL := oauth2Config.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)

	if err := openBrowser(authURL); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open browser automatically.\nPlease visit: %s\n", authURL)
	}

	// Wait for callback or context cancellation
	select {
	case result := <-resultCh:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		_ = server.Shutdown(shutdownCtx)

		if result.err != nil {
			return nil, result.err
		}

		return result.token, nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		_ = server.Shutdown(shutdownCtx)

		return nil, fmt.Errorf("%w: %w", ErrAuthenticationFailed, ctx.Err())
	}
}

// RefreshToken attempts to refresh an expired token using the refresh token.
func (a *Authenticator) RefreshToken(ctx context.Context, refreshToken string) (*TokenResult, error) {
	httpClient, err := a.buildHTTPClient()
	if err != nil {
		return nil, err
	}

	oidcCtx := gooidc.ClientContext(ctx, httpClient)

	provider, err := gooidc.NewProvider(oidcCtx, a.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to discover OIDC provider: %w", ErrAuthenticationFailed, err)
	}

	scopes := append([]string{gooidc.ScopeOpenID}, a.ExtraScopes...)

	oauth2Config := &oauth2.Config{
		ClientID: a.ClientID,
		Endpoint: provider.Endpoint(),
		Scopes:   scopes,
	}

	tokenSource := oauth2Config.TokenSource(oidcCtx, &oauth2.Token{
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

func (a *Authenticator) handleCallback(
	ctx context.Context,
	oauth2Config *oauth2.Config,
	provider *gooidc.Provider,
	expectedState, codeVerifier string,
	resultCh chan<- callbackResult,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			desc := r.URL.Query().Get("error_description")
			http.Error(w, "Authentication failed: "+errMsg, http.StatusBadRequest)
			resultCh <- callbackResult{err: fmt.Errorf("%w: %s: %s", ErrAuthenticationFailed, errMsg, desc)}

			return
		}

		if r.URL.Query().Get("state") != expectedState {
			http.Error(w, "Invalid state parameter", http.StatusBadRequest)
			resultCh <- callbackResult{err: fmt.Errorf("%w: state mismatch", ErrAuthenticationFailed)}

			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			resultCh <- callbackResult{err: fmt.Errorf("%w: missing authorization code", ErrAuthenticationFailed)}

			return
		}

		token, err := oauth2Config.Exchange(ctx, code,
			oauth2.SetAuthURLParam("code_verifier", codeVerifier),
		)
		if err != nil {
			http.Error(w, "Token exchange failed", http.StatusInternalServerError)
			resultCh <- callbackResult{err: fmt.Errorf("%w: token exchange failed: %w", ErrAuthenticationFailed, err)}

			return
		}

		idToken, ok := token.Extra("id_token").(string)
		if !ok {
			http.Error(w, "No ID token in response", http.StatusInternalServerError)
			resultCh <- callbackResult{err: fmt.Errorf("%w: no id_token in token response", ErrAuthenticationFailed)}

			return
		}

		// Verify the ID token
		verifier := provider.Verifier(&gooidc.Config{ClientID: a.ClientID})

		verified, err := verifier.Verify(ctx, idToken)
		if err != nil {
			http.Error(w, "Token verification failed", http.StatusInternalServerError)
			resultCh <- callbackResult{err: fmt.Errorf("%w: token verification failed: %w", ErrAuthenticationFailed, err)}

			return
		}

		fmt.Fprintf(w, "Authentication successful! You can close this window.")

		resultCh <- callbackResult{
			token: &TokenResult{
				IDToken:      idToken,
				RefreshToken: token.RefreshToken,
				Expiry:       verified.Expiry,
			},
		}
	}
}

func (a *Authenticator) buildHTTPClient() (*http.Client, error) {
	if a.CAFile == "" {
		return http.DefaultClient, nil
	}

	caCert, err := os.ReadFile(a.CAFile)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read CA file: %w", ErrAuthenticationFailed, err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("%w: failed to parse CA certificate", ErrAuthenticationFailed)
	}

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    caCertPool,
				MinVersion: tls.VersionTLS12,
			},
		},
	}, nil
}

func generatePKCE() (verifier, challenge string, err error) {
	b := make([]byte, codeVerifierLength)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("%w: failed to generate PKCE verifier: %w", ErrAuthenticationFailed, err)
	}

	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])

	return verifier, challenge, nil
}

func generateState() (string, error) {
	b := make([]byte, stateLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("%w: failed to generate state: %w", ErrAuthenticationFailed, err)
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}

func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
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
