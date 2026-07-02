package sshbootstrap

import "errors"

var (
	// ErrMissingAddr is returned when Options.Addr is empty. The dialer needs the
	// server's host:port endpoint; for Hetzner nodes this is the server's public
	// IPv4 with the SSH port.
	ErrMissingAddr = errors.New("ssh bootstrap: server address is required")

	// ErrMissingUser is returned when Options.User is empty. Cloud images boot
	// with a distribution-specific login (root on Hetzner's stock images), so the
	// caller must always name one — there is no safe default.
	ErrMissingUser = errors.New("ssh bootstrap: user is required")

	// ErrMissingSigner is returned when Options.Signer is nil. The client
	// authenticates exclusively with the per-cluster keypair
	// ([GenerateKeyPair]); password authentication is deliberately unsupported.
	ErrMissingSigner = errors.New("ssh bootstrap: signer is required")

	// ErrMissingHostKeyCallback is returned when Options.HostKeyCallback is nil.
	// Requiring an explicit callback forces the caller to choose a host-key
	// policy; silently defaulting to accept-anything would invite
	// man-in-the-middle attacks on the kubeconfig fetch.
	ErrMissingHostKeyCallback = errors.New(
		"ssh bootstrap: host key callback is required",
	)

	// ErrCommandFailed is returned (wrapped, with the command, exit code, and
	// stderr) when a remote command runs but exits non-zero. Callers branch on it
	// with errors.Is and read the exit code from [RunResult.ExitCode].
	ErrCommandFailed = errors.New("ssh bootstrap: remote command failed")
)
