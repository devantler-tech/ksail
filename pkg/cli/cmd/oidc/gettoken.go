package oidc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	oidcsvc "github.com/devantler-tech/ksail/v7/pkg/svc/oidc"
	"github.com/spf13/cobra"
)

const (
	// authTimeout is the maximum time to wait for the OIDC authentication flow.
	authTimeout = 2 * time.Minute
)

var (
	errNoCachedToken = errors.New("no cached token")
	errTokenExpired  = errors.New("token expired and no refresh token")
)

// newGetTokenCmd creates the 'oidc get-token' subcommand.
// This command implements the Kubernetes exec credential plugin protocol.
func newGetTokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-token",
		Short: "Get an OIDC token (exec credential plugin)",
		Long: `Get an OIDC token for Kubernetes authentication.

This command implements the client.authentication.k8s.io/v1 ExecCredential protocol.
It is intended to be used as a kubeconfig exec credential plugin, not called directly.

The token acquisition flow:
  1. Check the local cache for a valid token
  2. If expired, attempt to refresh using the stored refresh token
  3. If no valid token, start the browser-based authorization code flow with PKCE
  4. Output the ExecCredential JSON to stdout`,
		SilenceUsage: true,
		RunE:         handleGetToken,
	}

	cmd.Flags().String("issuer-url", "", "OIDC provider issuer URL (required)")
	cmd.Flags().String("client-id", "", "OIDC client ID (required)")
	cmd.Flags().StringSlice("extra-scope", nil, "Additional OIDC scopes")
	cmd.Flags().String("ca-file", "", "Path to CA certificate for self-signed OIDC providers")

	_ = cmd.MarkFlagRequired("issuer-url")
	_ = cmd.MarkFlagRequired("client-id")

	return cmd
}

func handleGetToken(cmd *cobra.Command, _ []string) error {
	issuerURL, _ := cmd.Flags().GetString("issuer-url")
	clientID, _ := cmd.Flags().GetString("client-id")
	extraScopes, _ := cmd.Flags().GetStringSlice("extra-scope")
	caFile, _ := cmd.Flags().GetString("ca-file")

	cacheDir, cacheDirErr := oidcsvc.CacheDir()
	cacheKey := oidcsvc.CacheKey(issuerURL, clientID, extraScopes)

	// 1. Try cached or refreshed token (skip if cache dir unavailable)
	if cacheDirErr == nil {
		token, err := tryFromCache(cmd, cacheDir, cacheKey, issuerURL, clientID, extraScopes, caFile)
		if err == nil && token != nil {
			return outputExecCredential(token.IDToken, token.Expiry)
		}
	}

	// 2. Interactive browser-based flow
	auth := &oidcsvc.Authenticator{
		IssuerURL:   issuerURL,
		ClientID:    clientID,
		ExtraScopes: extraScopes,
		CAFile:      caFile,
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), authTimeout)
	defer cancel()

	result, err := auth.Authenticate(ctx)
	if err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}

	if cacheDirErr == nil {
		_ = oidcsvc.SaveCachedToken(cacheDir, cacheKey, result)
	}

	return outputExecCredential(result.IDToken, result.Expiry)
}

func tryFromCache(
	cmd *cobra.Command,
	cacheDir, cacheKey, issuerURL, clientID string,
	extraScopes []string,
	caFile string,
) (*oidcsvc.TokenResult, error) {
	cached := oidcsvc.LoadCachedToken(cacheDir, cacheKey)
	if cached == nil {
		return nil, errNoCachedToken
	}

	if time.Now().Before(cached.Expiry) {
		return &oidcsvc.TokenResult{
			IDToken: cached.IDToken,
			Expiry:  cached.Expiry,
		}, nil
	}

	if cached.RefreshToken == "" {
		return nil, errTokenExpired
	}

	auth := &oidcsvc.Authenticator{
		IssuerURL:   issuerURL,
		ClientID:    clientID,
		ExtraScopes: extraScopes,
		CAFile:      caFile,
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), authTimeout)
	defer cancel()

	refreshed, err := auth.RefreshToken(ctx, cached.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("refresh failed: %w", err)
	}

	_ = oidcsvc.SaveCachedToken(cacheDir, cacheKey, refreshed)

	return refreshed, nil
}

func outputExecCredential(idToken string, expiry time.Time) error {
	data, err := oidcsvc.ExecCredentialJSON(idToken, expiry)
	if err != nil {
		return fmt.Errorf("failed to generate exec credential: %w", err)
	}

	_, err = fmt.Fprintln(os.Stdout, string(data))
	if err != nil {
		return fmt.Errorf("failed to write exec credential to stdout: %w", err)
	}

	return nil
}
