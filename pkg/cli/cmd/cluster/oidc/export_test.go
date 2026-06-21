package oidc

import (
	oidcsvc "github.com/devantler-tech/ksail/v7/pkg/svc/oidc"
	"github.com/spf13/cobra"
)

// TryFromCache exposes tryFromCache so black-box tests can exercise the
// cache-resolution branches (no cache, valid token, expired without refresh)
// without driving the command down its interactive, network-bound flow.
func TryFromCache(
	cmd *cobra.Command,
	cacheDir, cacheKey, issuerURL, clientID string,
	extraScopes []string,
	caFile string,
) (*oidcsvc.TokenResult, error) {
	return tryFromCache(cmd, cacheDir, cacheKey, issuerURL, clientID, extraScopes, caFile)
}

// Sentinel errors exposed for assertions in tests.
var (
	ErrNoCachedToken = errNoCachedToken
	ErrTokenExpired  = errTokenExpired
)
