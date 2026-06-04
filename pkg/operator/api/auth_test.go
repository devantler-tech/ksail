package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
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

	value := api.SignValue(secret, payload)

	got, err := api.VerifySignedValue(secret, value)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestVerifySignedValueRejectsTampering(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	value := api.SignValue(secret, []byte(`{"sub":"alice"}`))

	_, err := api.VerifySignedValue([]byte("a-different-secret-of-the-key-len"), value)
	require.ErrorIs(t, err, api.ErrInvalidCookie)

	_, err = api.VerifySignedValue(secret, value+"x")
	require.ErrorIs(t, err, api.ErrInvalidCookie)

	_, err = api.VerifySignedValue(secret, "no-separator")
	require.ErrorIs(t, err, api.ErrInvalidCookie)
}

func TestCurrentUserRejectsExpiredSession(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	auth := api.NewConfigAuthenticator(api.OIDCConfig{SessionSecret: secret, SessionTTL: time.Hour})

	expired := auth.SignedCookie(
		api.SessionCookieName,
		api.MustMarshal(
			api.SessionClaims{Subject: testSubject, Expiry: time.Now().Add(-time.Minute).Unix()},
		),
		"/",
		0,
	)

	request := requestWithCookie(expired)

	_, ok := auth.CurrentUser(request)
	assert.False(t, ok, "expired session must be rejected")
}

func TestCurrentUserAcceptsValidSession(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	auth := api.NewConfigAuthenticator(api.OIDCConfig{SessionSecret: secret, SessionTTL: time.Hour})

	valid := auth.SignedCookie(
		api.SessionCookieName,
		api.MustMarshal(
			api.SessionClaims{Subject: testSubject, Expiry: time.Now().Add(time.Hour).Unix()},
		),
		"/",
		3600,
	)

	claims, ok := auth.CurrentUser(requestWithCookie(valid))
	require.True(t, ok)
	assert.Equal(t, testSubject, claims.Subject)
}
