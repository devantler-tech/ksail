package api_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/stretchr/testify/assert"
)

// errCipherBackend is returned by failingCipherStub to drive the cipher handlers' error paths.
var errCipherBackend = errors.New("cipher backend boom")

// failingCipherStub implements api.CipherService and always fails, exercising the writeClientError
// branches of the cipher/secret handlers (a generic error maps to 500).
type failingCipherStub struct {
	stubClusterService
}

func (failingCipherStub) EncryptSecret(_ context.Context, _, _, _ string) (string, error) {
	return "", errCipherBackend
}

func (failingCipherStub) DecryptSecret(_ context.Context, _, _ string) (string, error) {
	return "", errCipherBackend
}

func (failingCipherStub) CipherRecipients(_ context.Context) ([]string, error) {
	return nil, errCipherBackend
}

// nilRecipientCipherStub returns a nil recipients slice (and succeeds otherwise) to exercise the
// handleCipherRecipients branch that normalizes nil to an empty slice so the JSON is `[]`, not `null`.
type nilRecipientCipherStub struct {
	stubClusterService
}

func (nilRecipientCipherStub) EncryptSecret(_ context.Context, _, _, _ string) (string, error) {
	return "", nil
}

func (nilRecipientCipherStub) DecryptSecret(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

func (nilRecipientCipherStub) CipherRecipients(_ context.Context) ([]string, error) {
	return nil, nil
}

func TestCipherRecipientsServiceError(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: failingCipherStub{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/secrets/recipients", "")

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "cipher backend boom")
}

func TestCipherRecipientsNilNormalizesToEmptyArray(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: nilRecipientCipherStub{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/secrets/recipients", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"recipients":[]`)
	assert.NotContains(t, recorder.Body.String(), "null")
}

func TestSecretEncryptServiceError(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: failingCipherStub{}}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/secrets/encrypt",
		`{"plaintext":"password: s3cret","recipient":"age1example"}`,
	)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "cipher backend boom")
}

func TestSecretDecryptServiceError(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: failingCipherStub{}}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/secrets/decrypt",
		`{"encrypted":"data: ENC[sops]"}`,
	)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "cipher backend boom")
}

func TestSecretEncryptMalformedJSON(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: cipherStub{}}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/secrets/encrypt",
		`{"plaintext": not-json`,
	)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestSecretDecryptMalformedJSON(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: cipherStub{}}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/secrets/decrypt",
		`not even json`,
	)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}
