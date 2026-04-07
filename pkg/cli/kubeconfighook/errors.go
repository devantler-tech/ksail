package kubeconfighook

import "errors"

var (
	errNotJWT     = errors.New("not a JWT token")
	errNoExpClaim = errors.New("JWT token has no exp claim")
)
