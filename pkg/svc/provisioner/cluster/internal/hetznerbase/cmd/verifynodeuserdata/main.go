// Package main checks a running Hetzner node for cluster signing material in
// the user-data the provider holds for it.
//
// The provisioner refuses to send signing material in user-data, but that
// guard only sees what this process composes. This command asks the node
// itself, through the Hetzner metadata service, so a system test can prove the
// property on a real cluster.
//
// Everything after "--" is the command that reaches the node. The metadata
// fetch is appended to it as the final argument. For example,
// "kubectl exec <pod> -- sh -c" runs the fetch in a pod on the node's host
// network.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
)

const (
	exitVerified = 0
	exitFailed   = 1
	exitUsage    = 2
)

const usage = "usage: verifynodeuserdata -- <command that reaches the node> [args...]"

var errMissingNodeCommand = errors.New(usage)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// run verifies the node reached by the command after "--" and returns the
// process exit code: verified, failed (including a node that could not be
// read), or a usage error.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	prefix, err := nodeCommandPrefix(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)

		return exitUsage
	}

	err = hetznerbase.VerifyNodeUserData(ctx, execNodeCommand(prefix))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "verify node user-data: %v\n", err)

		return exitFailed
	}

	_, _ = fmt.Fprintln(
		stdout, "✅ the node's provider-visible user-data carries no cluster signing material",
	)

	return exitVerified
}

func nodeCommandPrefix(args []string) ([]string, error) {
	if len(args) < 2 || args[0] != "--" {
		return nil, errMissingNodeCommand
	}

	return args[1:], nil
}

// execNodeCommand runs prefix with the node command appended as its last
// argument. Standard output and standard error are kept apart, because the
// verifier treats a diagnostic on standard error as an incomplete read.
func execNodeCommand(prefix []string) hetznerbase.NodeCommand {
	return func(ctx context.Context, command string) ([]byte, []byte, error) {
		// #nosec G204 -- the program and its arguments are the operator's own
		// command line after "--", and os/exec passes them literally with no
		// shell in between.
		cmd := exec.CommandContext(ctx, prefix[0], append(slices.Clone(prefix[1:]), command)...)

		var stdout, stderr bytes.Buffer

		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err != nil {
			return stdout.Bytes(), stderr.Bytes(), fmt.Errorf(
				"run %s: %w: %s", prefix[0], err, bytes.TrimSpace(stderr.Bytes()),
			)
		}

		return stdout.Bytes(), stderr.Bytes(), nil
	}
}
