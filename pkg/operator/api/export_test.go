package api

import (
	"net/http"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewAuthTestServer returns a Server with session-based OIDC auth enabled but without a live OIDC
// provider. It exercises the auth guard, the config endpoint, and session cookies (which need only
// the session secret); the provider-backed login/callback flow is not covered here.
func NewAuthTestServer(kubeClient client.Client, secret []byte) *Server {
	return &Server{
		Client: kubeClient,
		OIDC:   OIDCConfig{IssuerURL: "https://issuer.test", SessionSecret: secret},
		auth:   &authenticator{config: OIDCConfig{SessionSecret: secret, SessionTTL: time.Hour}},
	}
}

// NewSessionCookie returns a valid signed session cookie for the given subject.
func (s *Server) NewSessionCookie(subject string) *http.Cookie {
	return s.auth.signedCookie(
		sessionCookieName,
		mustMarshal(sessionClaims{Subject: subject, Expiry: time.Now().Add(time.Hour).Unix()}),
		"/",
		int(time.Hour.Seconds()),
	)
}
