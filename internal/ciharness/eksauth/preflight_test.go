package eksauth_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/ciharness/eksauth"
	"github.com/stretchr/testify/require"
)

func TestVerifyReadOnlyOIDCPreflight(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 5, 10, 0, 0, 0, time.UTC)
	role := "arn:aws:iam::123456789012:role/fixture"
	session := "ksail-eks-preflight-123"
	identity := `{"Account":"123456789012",` +
		`"Arn":"arn:aws:sts::123456789012:assumed-role/fixture/ksail-eks-preflight-123","UserId":"fixture"}`
	expiry := now.Add(119 * time.Minute).Format(time.RFC3339)
	quotedExpiry := `"` + expiry + `"`
	roleWithPath := strings.Replace(role, "role/", "role/team/", 1)

	require.NoError(t, eksauth.Verify(strings.NewReader(identity), role, session, expiry, now))
	require.NoError(
		t,
		eksauth.Verify(strings.NewReader(identity), role, session, quotedExpiry, now),
	)
	require.NoError(
		t,
		eksauth.Verify(strings.NewReader(identity), roleWithPath, session, expiry, now),
	)

	for _, test := range []struct {
		name     string
		identity string
		role     string
		expiry   string
	}{
		{"wrong account", strings.Replace(identity, `"Account":"123456789012"`, `"Account":"999999999999"`, 1), role, expiry},
		{"wrong role", strings.Replace(identity, "assumed-role/fixture/", "assumed-role/other/", 1), role, expiry},
		{"wrong session", strings.Replace(identity, session, "unexpected-session", 1), role, expiry},
		{"malformed role", identity, "not-an-arn", expiry},
		{"malformed identity", "{", role, expiry},
		{"second identity", identity + identity, role, expiry},
		{"oversized response", identity + strings.Repeat(" ", 20_000), role, expiry},
		{"missing expiration", identity, role, ""},
		{"malformed expiration", identity, role, "not-a-time"},
		{"malformed quoted expiration", identity, role, `"` + expiry},
		{"trailing expiration data", identity, role, `"` + expiry + `" "extra"`},
		{"expired session", identity, role, now.Add(-time.Minute).Format(time.RFC3339)},
		{"shortened session", identity, role, now.Add(time.Hour).Format(time.RFC3339)},
		{"excessive session", identity, role, now.Add(3 * time.Hour).Format(time.RFC3339)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := eksauth.Verify(
				strings.NewReader(test.identity),
				test.role,
				session,
				test.expiry,
				now,
			)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "123456789012")
			require.NotContains(t, err.Error(), "unexpected-session")
		})
	}
}

type failedIdentityReader struct{}

var errFailedIdentity = errors.New("private-provider-response")

func (failedIdentityReader) Read([]byte) (int, error) {
	return 0, errFailedIdentity
}

func TestVerifyRejectsUnreadableIdentity(t *testing.T) {
	t.Parallel()

	err := eksauth.Verify(failedIdentityReader{}, "", "", "", time.Now())
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private-provider-response")
}
