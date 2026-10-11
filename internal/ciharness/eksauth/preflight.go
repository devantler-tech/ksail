// Package eksauth verifies read-only prerequisites for the disposable EKS trial.
package eksauth

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
)

const maxIdentityBytes = 16 * 1024

var (
	rolePattern = regexp.MustCompile(
		`^arn:(aws(?:-[a-z0-9-]+)?):iam::([0-9]{12}):role/` +
			`(?:[A-Za-z0-9+=,.@_/-]+/)?([A-Za-z0-9+=,.@_-]+)$`,
	)
	errUnavailable  = errors.New("AWS identity response is unavailable or oversized")
	errMalformed    = errors.New("AWS identity response is malformed")
	errExpectedRole = errors.New("expected AWS role or session is invalid")
	errIdentity     = errors.New(
		"AWS caller identity does not match the selected trial role and session",
	)
	errExpiration = errors.New("AWS session expiration is missing or malformed")
	errLifetime   = errors.New("AWS session does not provide the requested two-hour lifetime")
)

type callerIdentity struct {
	Account string `json:"account"`
	ARN     string `json:"arn"`
}

// Verify checks native STS identity and actual expiration without returning
// provider identifiers or credential metadata in an error.
func Verify(reader io.Reader, role, session, expiry string, now time.Time) error {
	identity, err := readIdentity(reader)
	if err != nil {
		return err
	}

	selected := rolePattern.FindStringSubmatch(role)
	if len(selected) != 4 || session == "" {
		return errExpectedRole
	}

	expected := "arn:" + selected[1] + ":sts::" + selected[2] +
		":assumed-role/" + selected[3] + "/" + session
	if identity.Account != selected[2] || identity.ARN != expected {
		return errIdentity
	}

	expires, err := readExpiration(expiry)
	if err != nil {
		return errExpiration
	}

	// Allow two minutes for the action and validation round trip, while rejecting
	// shortened sessions and an expiration inconsistent with the requested 2h.
	remaining := expires.Sub(now)
	if remaining < 118*time.Minute || remaining > 122*time.Minute {
		return errLifetime
	}

	return nil
}

func readIdentity(reader io.Reader) (callerIdentity, error) {
	var identity callerIdentity

	data, err := io.ReadAll(io.LimitReader(reader, maxIdentityBytes+1))
	if err != nil || len(data) > maxIdentityBytes {
		return identity, errUnavailable
	}

	if json.Unmarshal(data, &identity) != nil {
		return identity, errMalformed
	}

	return identity, nil
}

func readExpiration(expiry string) (time.Time, error) {
	// The pinned credentials action serializes its SDK Date output as a JSON
	// string. Accept that native wire form as well as a plain RFC3339 value.
	if strings.HasPrefix(expiry, `"`) {
		var decoded string
		if json.Unmarshal([]byte(expiry), &decoded) != nil {
			return time.Time{}, errExpiration
		}

		expiry = decoded
	}

	expires, err := time.Parse(time.RFC3339, expiry)
	if err != nil {
		return time.Time{}, errExpiration
	}

	return expires, nil
}
