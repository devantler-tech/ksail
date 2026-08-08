package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
)

// cipherStub implements api.CipherService with canned responses.
type cipherStub struct {
	stubClusterService
}

func (cipherStub) EncryptSecret(_ context.Context, _, _, _ string) (string, error) {
	return "data: ENC[sops]\nsops:\n  age: []\n", nil
}

func (cipherStub) DecryptSecret(_ context.Context, _, _ string) (string, error) {
	return "password: s3cret\n", nil
}

func (cipherStub) CipherRecipients(_ context.Context) ([]string, error) {
	return []string{"age1example"}, nil
}

func TestConfigReportsSecretsCipher(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: cipherStub{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"secretsCipher":true`)
}

func TestCipherRecipientsEndpoint(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: cipherStub{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/secrets/recipients", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "age1example")
}

func TestSecretEncryptEndpoint(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: cipherStub{}}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/secrets/encrypt",
		`{"plaintext":"password: s3cret","recipient":"age1example"}`,
	)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "ENC[sops]")
}

func TestSecretDecryptEndpoint(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: cipherStub{}}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/secrets/decrypt",
		`{"encrypted":"data: ENC[sops]"}`,
	)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "password: s3cret")
}

func TestSecretDecryptRejectsNonJSONContentType(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: cipherStub{}}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/secrets/decrypt",
		strings.NewReader(`{"encrypted":"data: ENC[sops]"}`),
	)
	request.Header.Set("Content-Type", "text/plain")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusUnsupportedMediaType, recorder.Code)
}

func TestSecretEncryptBlockedWhenReadOnly(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: cipherStub{}, ReadOnly: true}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/secrets/encrypt",
		`{"plaintext":"k: v","recipient":"age1example"}`,
	)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
}
