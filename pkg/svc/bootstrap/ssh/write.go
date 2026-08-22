package sshbootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
)

const (
	// DefaultSecretFileMode is the mode [Client.WriteFile] applies when the
	// caller passes a zero mode. Bootstrap writes carry cluster signing
	// material, so the default is owner-only rather than whatever the login
	// shell's umask happens to yield.
	DefaultSecretFileMode fs.FileMode = 0o600

	// WriteTruncatedExitCode is the status the remote script exits with when it
	// received fewer bytes than the client announced. It sits outside the range
	// shells use for their own failures (1, 2, 126, 127) so a short transfer is
	// never mistaken for a command that merely failed.
	WriteTruncatedExitCode = 65

	// stagingSuffix names the file content streams into before it is published.
	// It sits beside the destination so the final rename is atomic — same
	// directory means same filesystem.
	stagingSuffix = ".ksail-partial"
)

// WriteFile places content at remotePath with the given mode, creating parent
// directories as needed. A zero mode means [DefaultSecretFileMode].
//
// Content streams over the session's input channel rather than the command
// line, so secret material never reaches the node's process arguments, its
// /proc entry, or the login shell's history. That is the property that makes
// this usable for cluster PKI, which cloud-init user-data cannot carry because
// the provider can read it back.
//
// The write is staged beside the destination and published with an atomic
// rename only after the remote has verified it received every byte, so an
// interrupted or truncated transfer returns an error wrapping
// [ErrWriteTruncated] and leaves the destination untouched — never a short
// file that looks complete.
func (c *Client) WriteFile(
	ctx context.Context,
	remotePath string,
	content []byte,
	mode fs.FileMode,
) error {
	if mode == 0 {
		mode = DefaultSecretFileMode
	}

	script := writeScript(remotePath, len(content), mode)

	result, err := c.exec(ctx, script, bytes.NewReader(content))
	if err == nil {
		return nil
	}

	if errors.Is(err, ErrCommandFailed) && result.ExitCode == WriteTruncatedExitCode {
		return fmt.Errorf(
			"%w: %s: remote received fewer than %d byte(s); destination left unchanged",
			ErrWriteTruncated, remotePath, len(content),
		)
	}

	return fmt.Errorf("write remote file %s: %w", remotePath, err)
}

// writeScript renders the remote half of [Client.WriteFile]. Every path is
// single-quoted against shell interpretation, and the content itself is absent
// by construction — it arrives on stdin.
func writeScript(remotePath string, size int, mode fs.FileMode) string {
	staging := shellQuote(remotePath + stagingSuffix)

	return strings.Join([]string{
		// Abort on the first failure so a failed mkdir or chmod can never fall
		// through to the rename that publishes the file.
		"set -eu",
		"mkdir -p -- " + shellQuote(path.Dir(remotePath)),
		// Create the staging file owner-only from the outset: chmod below fixes
		// the mode, but only after the bytes have landed, and a secret must not
		// be world-readable even for that window.
		"umask 077",
		"cat > " + staging,
		// Count what actually arrived. $n is deliberately unquoted so any padding
		// some wc implementations emit is split away; an empty or non-numeric
		// value makes the test false, which takes the discard branch — the
		// fail-closed direction.
		"n=$(wc -c < " + staging + ")",
		"[ $n -eq " + strconv.Itoa(size) + " ] || { rm -f -- " + staging +
			"; exit " + strconv.Itoa(WriteTruncatedExitCode) + "; }",
		// Set the mode before publishing, so the file is never visible at its
		// final path with the wrong permissions.
		"chmod " + fmt.Sprintf("%04o", mode.Perm()) + " " + staging,
		"mv -f -- " + staging + " " + shellQuote(remotePath),
	}, "\n")
}
