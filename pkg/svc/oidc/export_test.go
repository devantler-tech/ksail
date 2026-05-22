package oidc

import (
	"context"
	"net/http"
)

// ValidateCallbackRequestForTest exposes validateCallbackRequest for unit testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var ValidateCallbackRequestForTest = validateCallbackRequest

// GeneratePKCEForTest exposes generatePKCE for unit testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var GeneratePKCEForTest = generatePKCE

// GenerateStateForTest exposes generateState for unit testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var GenerateStateForTest = generateState

// BuildHTTPClientForTest exposes buildHTTPClient for unit testing.
func (a *Authenticator) BuildHTTPClientForTest() (*http.Client, error) {
	return a.buildHTTPClient()
}

// NewOIDCProviderForTest exposes newOIDCProvider for unit testing, returning
// only the error since the result type is unexported.
func (a *Authenticator) NewOIDCProviderForTest(ctx context.Context, redirectURL string) error {
	_, err := a.newOIDCProvider(ctx, redirectURL)

	return err
}
