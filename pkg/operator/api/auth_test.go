package api //nolint:testpackage // white-box tests for unexported cookie-signing helpers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requestWithCookie(cookie *http.Cookie) *http.Request {
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/clusters",
		nil,
	)
	request.AddCookie(cookie)

	return request
}

func TestSignedValueRoundTrip(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	payload := []byte(`{"sub":"alice"}`)

	value := signValue(secret, payload)

	got, err := verifySignedValue(secret, value)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestVerifySignedValueRejectsTampering(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	value := signValue(secret, []byte(`{"sub":"alice"}`))

	_, err := verifySignedValue([]byte("a-different-secret-of-the-key-len"), value)
	require.ErrorIs(t, err, errInvalidCookie)

	_, err = verifySignedValue(secret, value+"x")
	require.ErrorIs(t, err, errInvalidCookie)

	_, err = verifySignedValue(secret, "no-separator")
	require.ErrorIs(t, err, errInvalidCookie)
}

func TestCurrentUserRejectsExpiredSession(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	auth := &authenticator{config: OIDCConfig{SessionSecret: secret, SessionTTL: time.Hour}}

	expired := auth.signedCookie(
		sessionCookieName,
		mustMarshal(
			sessionClaims{Subject: testSubject, Expiry: time.Now().Add(-time.Minute).Unix()},
		),
		"/",
		0,
	)

	request := requestWithCookie(expired)

	_, ok := auth.currentUser(request)
	assert.False(t, ok, "expired session must be rejected")
}

func TestCurrentUserAcceptsValidSession(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	auth := &authenticator{config: OIDCConfig{SessionSecret: secret, SessionTTL: time.Hour}}

	valid := auth.signedCookie(
		sessionCookieName,
		mustMarshal(sessionClaims{Subject: testSubject, Expiry: time.Now().Add(time.Hour).Unix()}),
		"/",
		3600,
	)

	claims, ok := auth.currentUser(requestWithCookie(valid))
	require.True(t, ok)
	assert.Equal(t, testSubject, claims.Subject)
}
