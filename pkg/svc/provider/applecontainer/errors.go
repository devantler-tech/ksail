package applecontainer

import "errors"

// Errors for Apple container provider operations.
var (
	// ErrCLINotFound is returned when the `container` CLI is not installed or not on PATH.
	ErrCLINotFound = errors.New(
		"apple container CLI not found: install it from https://github.com/apple/container " +
			"(requires macOS on Apple silicon)",
	)

	// ErrServiceNotRunning is returned when the CLI is installed but its system service is not
	// answering.
	ErrServiceNotRunning = errors.New(
		"apple container service is not running: start it with `container system start`",
	)

	// ErrCommandFailed is returned when a `container` CLI invocation exits with an error.
	ErrCommandFailed = errors.New("apple container command failed")

	// ErrInvalidOutput is returned when the CLI prints output this provider cannot parse.
	ErrInvalidOutput = errors.New("apple container CLI printed unexpected output")

	// ErrInvalidNodeSpec is returned when a node cannot be created from the given specification.
	ErrInvalidNodeSpec = errors.New("invalid node specification")

	// ErrNodeNotFound is returned when a named node does not exist in the cluster.
	ErrNodeNotFound = errors.New("node not found")

	// ErrNodeExists is returned when a container with the requested node name already exists.
	ErrNodeExists = errors.New("a container with this name already exists")

	// ErrNoAddress is returned when a node has no usable IPv4 address, for example because it is
	// stopped.
	ErrNoAddress = errors.New("node has no IPv4 address")
)
