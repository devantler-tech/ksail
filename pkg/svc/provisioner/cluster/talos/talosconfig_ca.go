package talosprovisioner

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// ErrMalformedTalosConfigCA is returned when the CA bytes for the
// current context in a saved talosconfig fail X.509 parsing. The cause
// is wrapped so callers can use errors.Is/As.
var ErrMalformedTalosConfigCA = errors.New("malformed CA in talosconfig")

// ErrCANotPEMCertificate is the wrapped cause for a talosconfig CA that
// decodes from base64 but is not a PEM CERTIFICATE block.
var ErrCANotPEMCertificate = errors.New("CA is not a PEM CERTIFICATE block")

// MalformedTalosConfigCAError carries the talosconfig path, current
// context name, and the underlying x509 parse error, plus a single
// human-readable message that points at `ksail cluster repair`.
type MalformedTalosConfigCAError struct {
	Path    string
	Context string
	Cause   error
}

// Error implements [error].
func (e *MalformedTalosConfigCAError) Error() string {
	ctx := e.Context
	if ctx == "" {
		ctx = "(unset)"
	}

	return fmt.Sprintf(
		"malformed CA in talosconfig %s (context %q): %v\n"+
			"Run `ksail cluster repair` to attempt automatic recovery, "+
			"or restore a backup of %s.",
		e.Path, ctx, e.Cause, e.Path,
	)
}

// Unwrap returns the underlying x509 parse error so errors.Is/As work.
func (e *MalformedTalosConfigCAError) Unwrap() error { return e.Cause }

// Is reports the sentinel ErrMalformedTalosConfigCA as a match.
func (e *MalformedTalosConfigCAError) Is(target error) bool {
	return target == ErrMalformedTalosConfigCA
}

// validateCurrentContextCA checks that the CA stored under the current
// context of cfg parses as a valid X.509 certificate. It returns a
// [*MalformedTalosConfigCAError] when the CA is structurally broken in
// a way that would make [crypto/tls] reject it (the symptom users see
// is "failed to append CA certificate to RootCAs pool").
//
// Benign cases — a nil config, a missing/blank current context, or an
// empty CA — return nil. Those situations are handled by the caller's
// regular client-construction path (or surfaced as the existing
// "no current context" error from the Talos library) and are not
// indicative of CA corruption.
func validateCurrentContextCA(cfg *clientconfig.Config, path string) error {
	if cfg == nil {
		return nil
	}

	ctxName := cfg.Context

	ctx, ok := cfg.Contexts[ctxName]
	if !ok || ctx == nil {
		return nil
	}

	if len(ctx.CA) == 0 {
		return nil
	}

	caBytes, err := base64.StdEncoding.DecodeString(ctx.CA)
	if err != nil {
		return &MalformedTalosConfigCAError{
			Path:    path,
			Context: ctxName,
			Cause:   fmt.Errorf("CA base64 decode: %w", err),
		}
	}

	block, _ := pem.Decode(caBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		return &MalformedTalosConfigCAError{
			Path:    path,
			Context: ctxName,
			Cause:   ErrCANotPEMCertificate,
		}
	}

	_, err = x509.ParseCertificate(block.Bytes)
	if err != nil {
		return &MalformedTalosConfigCAError{
			Path:    path,
			Context: ctxName,
			Cause:   err,
		}
	}

	return nil
}
