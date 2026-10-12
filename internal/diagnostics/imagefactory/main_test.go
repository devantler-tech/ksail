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
	id := hex.EncodeToString(digest[:])
	for _, tc := range []struct {
		name, responseID, want string
		trust, cancel          bool
	}{
		{"valid", id, "ok", true, false},
		{"wrong-id", "incorrect", "id_mismatch", true, false},
		{"untrusted-certificate", id, "certificate", false, false},
		{"cancelled", id, "cancelled", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/schematics", r.URL.Path)
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				assert.Equal(t, payload, string(body))
				err = json.NewEncoder(w).Encode(map[string]string{"id": tc.responseID, "schematic": payload})
				assert.NoError(t, err)
			}))
			defer server.Close()

			transport := http.DefaultTransport.(*http.Transport).Clone()
			if tc.trust {
				pool := x509.NewCertPool()
				pool.AddCert(server.Certificate())
				transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
				transport.TLSClientConfig.InsecureSkipVerify = false
				transport.TLSClientConfig.RootCAs = pool
			}
			defer transport.CloseIdleConnections()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			result := register(ctx, server.URL, &http.Client{Transport: transport})
			require.Equal(t, tc.want, result.Outcome)
			if tc.want == "ok" {
				require.True(t, result.TLSVerified)
				require.True(t, result.IDMatches)
			}
		})
	}
}

func TestTransportComparisonsPreserveVerifiedTLS(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"http1", "pre126-curves", "ipv4"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			transport := transportFor(mode)
			defer transport.CloseIdleConnections()
			if transport.TLSClientConfig != nil {
				require.False(t, transport.TLSClientConfig.InsecureSkipVerify)
			}
			if mode == "pre126-curves" {
				require.Equal(t, []tls.CurveID{tls.X25519MLKEM768, tls.X25519, tls.CurveP256, tls.CurveP384, tls.CurveP521}, transport.TLSClientConfig.CurvePreferences)
			}
			if mode == "http1" {
				require.True(t, transport.Protocols.HTTP1())
				require.False(t, transport.Protocols.HTTP2())
			}
		})
	}
}
