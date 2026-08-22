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

	// ErrWriteTruncated is returned (wrapped) by [Client.WriteFile] when the
	// remote received fewer bytes than the client announced. The destination is
	// left untouched — content is staged beside it and renamed into place only
	// after the byte count matches — so a caller seeing this never has a short
	// file masquerading as a complete one.
	ErrWriteTruncated = errors.New("ssh bootstrap: remote file write was truncated")
)
