package hetznerbase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
)

// metadataUserDataEndpoint is the Hetzner instance metadata document returning
// the user-data the provider actually stored for a server.
//
// This is the only provider-visible view of that value. The Cloud API accepts
// user_data on server create and rebuild but never returns it: hcloud-go
// exposes it on ServerCreateOpts and ServerRebuildOpts and not on Server, and
// its metadata client offers hostname, instance ID, region and private networks
// with no user-data accessor. So a deployed node's copy is only observable from
// the node itself, which is why verification is expressed as a command rather
// than an API read.
const metadataUserDataEndpoint = "http://169.254.169.254/hetzner/v1/userdata"

// metadataFetchTimeoutSeconds bounds the metadata request so a node whose
// link-local route is black-holed fails instead of hanging a verification run.
const metadataFetchTimeoutSeconds = 10

// ErrNodeUserDataUnreadable is returned when a node's provider-visible
// user-data could not be observed at all: the fetch failed, or it produced an
// empty document.
//
// It is deliberately distinct from [ErrPrivateKeyInUserData] and
// [ErrSigningTransportInUserData]. Those two report that signing material WAS
// found; this one reports that nothing was looked at, which must never be
// reported as a clean node. That distinction is the whole value of the check —
// an unobserved node and a verified-clean node are the same silence, and only
// one of them is evidence.
var ErrNodeUserDataUnreadable = errors.New(
	"hetzner: node provider-visible user-data could not be read",
)

// NodeCommand runs a single shell command on a node and returns its standard
// output. A non-zero exit or a transport failure must return a non-nil error so
// the caller cannot mistake an unexecuted command for an empty result.
//
// It is a function rather than an interface so callers adapt their own client
// without this package depending on a transport: the bootstrap SSH client's Run
// method fits behind a two-line adapter.
type NodeCommand func(ctx context.Context, command string) ([]byte, error)

// VerifyNoSigningMaterial reports whether userData carries cluster signing
// material, applying the same guards the create path applies.
//
// It differs from the create path in what it deliberately leaves out. The
// provider-acceptance ceiling and the user-data shape check are decisions about
// what may be SENT; a node that has already booted is past both, so applying
// them to a readback would report a leak-free node as a failure for a reason
// unrelated to leakage. Gzip expansion is kept, because compression hides a
// marker just as effectively on the way back as on the way out.
//
// The input may come from anywhere — a live node, an audit capture, a
// provisioner under test — which is the point: the create-path guard can only
// inspect user-data this process just composed, and so can say nothing about
// what a running cluster actually carries.
func VerifyNoSigningMaterial(userData string) error {
	inspected, err := expandRawGzipUserData(userData)
	if err != nil {
		return err
	}

	err = validatePEMPrivateKeys(inspected)
	if err != nil {
		return err
	}

	return validateSigningTransport(inspected)
}

// VerifyNodeUserData fetches a node's provider-visible user-data from the
// Hetzner metadata service and verifies it carries no cluster signing material.
//
// An empty document is refused rather than accepted. cURL exits zero for a
// request that returns nothing, and an empty string trivially contains no
// signing material, so treating it as clean would turn "we could not see the
// user-data" into the strongest result this function can report.
func VerifyNodeUserData(ctx context.Context, run NodeCommand) error {
	if run == nil {
		return fmt.Errorf("%w: no command runner was supplied", ErrNodeUserDataUnreadable)
	}

	stdout, err := run(ctx, metadataUserDataCommand())
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNodeUserDataUnreadable, err)
	}

	if len(bytes.TrimSpace(stdout)) == 0 {
		return fmt.Errorf(
			"%w: %s returned an empty document",
			ErrNodeUserDataUnreadable, metadataUserDataEndpoint,
		)
	}

	return VerifyNoSigningMaterial(string(stdout))
}

// metadataUserDataCommand builds the metadata fetch. --fail turns an HTTP error
// status into a non-zero exit so a proxy or error page cannot be inspected as
// though it were user-data, and --show-error keeps the cause on stderr for the
// caller to surface.
func metadataUserDataCommand() string {
	return fmt.Sprintf(
		"curl --fail --silent --show-error --max-time %d -- %s",
		metadataFetchTimeoutSeconds, metadataUserDataEndpoint,
	)
}
