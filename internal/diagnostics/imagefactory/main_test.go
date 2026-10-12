package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterRequiresVerifiedTLSAndExactSchematic(t *testing.T) {
	t.Parallel()

	// Independent literal, not the probe's own expected-ID helper.
	payload := "customization: {}\n"
	digest := sha256.Sum256([]byte(payload))

	schematicID := hex.EncodeToString(digest[:])
	for _, testCase := range []struct {
		name, responseID, want string
		trust, cancel          bool
	}{
		{"valid", schematicID, "ok", true, false},
		{"wrong-id", "incorrect", "id_mismatch", true, false},
		{"untrusted-certificate", schematicID, "certificate", false, false},
		{"cancelled", schematicID, "cancelled", true, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewTLSServer(
				http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					assert.Equal(t, http.MethodPost, request.Method)
					assert.Equal(t, "/schematics", request.URL.Path)
					body, err := io.ReadAll(request.Body)
					assert.NoError(t, err)
					assert.Equal(t, payload, string(body))
					err = json.NewEncoder(writer).
						Encode(map[string]string{"id": testCase.responseID, "schematic": payload})
					assert.NoError(t, err)
				}),
			)
			defer server.Close()

			base, ok := http.DefaultTransport.(*http.Transport)
			require.True(t, ok)

			transport := base.Clone()

			if testCase.trust {
				pool := x509.NewCertPool()
				pool.AddCert(server.Certificate())
				serverTransport, ok := server.Client().Transport.(*http.Transport)
				require.True(t, ok)

				transport.TLSClientConfig = serverTransport.TLSClientConfig.Clone()
				transport.TLSClientConfig.InsecureSkipVerify = false
				transport.TLSClientConfig.RootCAs = pool
			}
			defer transport.CloseIdleConnections()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			if testCase.cancel {
				cancel()
			}

			result := register(ctx, server.URL, &http.Client{Transport: transport})
			require.Equal(t, testCase.want, result.Outcome)

			if testCase.want == "ok" {
				require.True(t, result.TLSVerified)
				require.True(t, result.IDMatches)
			}
		})
	}
}

func TestTransportComparisonsPreserveVerifiedTLS(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"http1", "pre126-curves", "ipv4", "small-writes", "classical-control"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			transport := transportFor(mode)

			require.NotNil(t, transport)
			defer transport.CloseIdleConnections()

			if transport.TLSClientConfig != nil {
				require.False(t, transport.TLSClientConfig.InsecureSkipVerify)
			}

			if mode == "pre126-curves" {
				require.Equal(t, []tls.CurveID{
					tls.X25519MLKEM768, tls.X25519, tls.CurveP256, tls.CurveP384, tls.CurveP521,
				}, transport.TLSClientConfig.CurvePreferences)
			}

			if mode == "http1" {
				require.True(t, transport.Protocols.HTTP1())
				require.False(t, transport.Protocols.HTTP2())
			}
		})
	}
}
