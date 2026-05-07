// Package talosconfig provides a [repairer.Repair] that detects and
// fixes a known single-byte corruption pattern in Talos talosconfig
// (~/.talos/config) certificate-authority bytes.
//
// The corruption is a malformed BasicConstraints SEQUENCE length field
// in the X.509v3 extensions of the self-signed Talos OS CA stored in a
// context. The corrupt byte truncates the inner OCTET STRING wrapping
// the `CA: TRUE` boolean, causing [crypto/x509.ParseCertificate] (and
// therefore [crypto/tls.Config.RootCAs.AppendCertsFromPEM]) to reject
// the certificate. The user-visible symptom from `ksail cluster update`
// is:
//
//	failed to append CA certificate to RootCAs pool
//
// The repair flips one byte (DER offset of the
// `30 0e 06 03 55 1d 13 …` pattern, byte +1: 0x0e → 0x0f) and verifies
// the resulting certificate parses cleanly before writing.
package talosconfig

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/repairer"
	"gopkg.in/yaml.v3"
)

// DefaultPath is the standard talosconfig location respected by Talos
// tooling and by KSail when the provisioner options do not override it.
const DefaultPath = "~/.talos/config"

// repairName is the stable identifier returned by [Repair.Name].
const repairName = "talosconfig-ca"

// corruptedBasicConstraintsPrefix matches the DER bytes of a
// BasicConstraints extension whose SEQUENCE length is 0x0e but should
// be 0x0f. The trailing `0xff` (BOOLEAN TRUE for `CA:TRUE`) ends up
// outside the extension as a result of the off-by-one.
var corruptedBasicConstraintsPrefix = []byte{
	0x30, 0x0e, 0x06, 0x03, 0x55, 0x1d, 0x13, 0x01,
	0x01, 0xff, 0x04, 0x05, 0x30, 0x03, 0x01, 0x01,
}

// ErrPatternNotMatched is returned by [Repair] when a context CA fails
// to parse but does not match the known corruption signature.
var ErrPatternNotMatched = errors.New("malformed CA does not match a known repair pattern")

// Repair implements [repairer.Repair].
type Repair struct {
	// Path is the talosconfig path. Empty means [DefaultPath].
	Path string
	// Now overrides time.Now for deterministic backup filenames in
	// tests. Nil falls back to time.Now.
	Now func() time.Time
}

// Name returns the stable identifier "talosconfig-ca".
func (r *Repair) Name() string { return repairName }

// Run loads the talosconfig file, attempts to repair every context whose
// CA does not parse, writes a timestamped backup before overwriting,
// and returns a single [repairer.Result] summarising the outcome.
func (r *Repair) Run(_ context.Context, logWriter io.Writer) repairer.Result {
	rawPath := r.Path
	if rawPath == "" {
		rawPath = DefaultPath
	}

	path, err := fsutil.ExpandHomePath(rawPath)
	if err != nil {
		return repairer.Result{
			Name:   repairName,
			Status: repairer.StatusSkipped,
			Detail: fmt.Sprintf("could not expand path %q: %v", rawPath, err),
			Err:    err,
		}
	}

	data, err := os.ReadFile(path) //nolint:gosec // user-supplied talosconfig path
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return repairer.Result{
				Name:   repairName,
				Status: repairer.StatusSkipped,
				Detail: fmt.Sprintf("%s does not exist", path),
			}
		}

		return repairer.Result{
			Name:   repairName,
			Status: repairer.StatusSkipped,
			Detail: fmt.Sprintf("could not read %s: %v", path, err),
			Err:    err,
		}
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return repairer.Result{
			Name:   repairName,
			Status: repairer.StatusUnrepairable,
			Detail: fmt.Sprintf("%s: invalid YAML: %v", path, err),
			Err:    err,
		}
	}

	repaired, unrepairable, alreadyValid := r.walkAndRepair(&doc, logWriter)

	if repaired == 0 {
		switch {
		case unrepairable > 0:
			return repairer.Result{
				Name:   repairName,
				Status: repairer.StatusUnrepairable,
				Detail: fmt.Sprintf("%s: %d context(s) malformed but no known repair pattern matches", path, unrepairable),
			}
		case alreadyValid > 0:
			return repairer.Result{
				Name:   repairName,
				Status: repairer.StatusOK,
				Detail: fmt.Sprintf("%s: %d context(s) already valid", path, alreadyValid),
			}
		default:
			return repairer.Result{
				Name:   repairName,
				Status: repairer.StatusOK,
				Detail: fmt.Sprintf("%s: no contexts with a CA found", path),
			}
		}
	}

	out, err := marshalPreserving(&doc)
	if err != nil {
		return repairer.Result{
			Name:   repairName,
			Status: repairer.StatusUnrepairable,
			Detail: fmt.Sprintf("%s: failed to re-marshal YAML: %v", path, err),
			Err:    err,
		}
	}

	backup := fmt.Sprintf("%s.bak.%s", path, r.now().Format("20060102-150405"))
	if err := os.WriteFile(backup, data, 0o600); err != nil {
		return repairer.Result{
			Name:   repairName,
			Status: repairer.StatusUnrepairable,
			Detail: fmt.Sprintf("%s: failed to write backup %s: %v", path, backup, err),
			Err:    err,
		}
	}

	if err := os.WriteFile(path, out, 0o600); err != nil {
		return repairer.Result{
			Name:   repairName,
			Status: repairer.StatusUnrepairable,
			Detail: fmt.Sprintf("%s: failed to write repaired config: %v", path, err),
			Err:    err,
		}
	}

	return repairer.Result{
		Name:       repairName,
		Status:     repairer.StatusRepaired,
		Detail:     fmt.Sprintf("%s: repaired %d CA(s)", path, repaired),
		BackupPath: backup,
	}
}

func (r *Repair) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}

	return time.Now()
}

// walkAndRepair traverses the YAML document looking for `contexts.<name>.ca`
// scalar nodes, attempts to repair each, and returns counts.
func (r *Repair) walkAndRepair(
	doc *yaml.Node,
	logWriter io.Writer,
) (int, int, int) {
	var repaired, unrepairable, alreadyValid int

	walkContextCANodes(doc, func(ctxName string, caNode *yaml.Node) {
		if caNode == nil || caNode.Value == "" {
			return
		}

		caBytes, err := base64.StdEncoding.DecodeString(caNode.Value)
		if err != nil {
			fmt.Fprintf(logWriter, "  [%s] base64 decode failed: %v (skipped)\n", ctxName, err)

			unrepairable++

			return
		}

		block, _ := pem.Decode(caBytes)
		if block == nil || block.Type != "CERTIFICATE" {
			fmt.Fprintf(logWriter, "  [%s] not a PEM CERTIFICATE block (skipped)\n", ctxName)

			unrepairable++

			return
		}

		if _, err := x509.ParseCertificate(block.Bytes); err == nil {
			fmt.Fprintf(logWriter, "  [%s] CA already valid\n", ctxName)

			alreadyValid++

			return
		}

		fixed, ok := tryRepair(block.Bytes)
		if !ok {
			fmt.Fprintf(logWriter, "  [%s] %v\n", ctxName, ErrPatternNotMatched)

			unrepairable++

			return
		}

		if _, err := x509.ParseCertificate(fixed); err != nil {
			fmt.Fprintf(logWriter, "  [%s] repair did not produce a valid cert: %v\n", ctxName, err)

			unrepairable++

			return
		}

		newPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: fixed})
		caNode.Value = base64.StdEncoding.EncodeToString(newPEM)
		caNode.Style = yaml.DoubleQuotedStyle
		caNode.Tag = "!!str"

		fmt.Fprintf(logWriter, "  [%s] repaired ✓\n", ctxName)

		repaired++
	})

	return repaired, unrepairable, alreadyValid
}

// walkContextCANodes finds every `contexts.<name>.ca` scalar node in the
// YAML document and invokes fn with the context name and the value
// node.
func walkContextCANodes(doc *yaml.Node, fn func(name string, caNode *yaml.Node)) {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		walkContextCANodes(doc.Content[0], fn)

		return
	}

	if doc.Kind != yaml.MappingNode {
		return
	}

	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value != "contexts" {
			continue
		}

		ctxs := doc.Content[i+1]
		if ctxs.Kind != yaml.MappingNode {
			continue
		}

		for j := 0; j+1 < len(ctxs.Content); j += 2 {
			ctxName := ctxs.Content[j].Value

			ctxBody := ctxs.Content[j+1]
			if ctxBody.Kind != yaml.MappingNode {
				continue
			}

			for k := 0; k+1 < len(ctxBody.Content); k += 2 {
				if ctxBody.Content[k].Value == "ca" {
					fn(ctxName, ctxBody.Content[k+1])
				}
			}
		}
	}
}

// tryRepair returns a copy of der with the documented byte-bump applied
// when the corruption signature is present.
func tryRepair(der []byte) ([]byte, bool) {
	idx := bytes.Index(der, corruptedBasicConstraintsPrefix)
	if idx < 0 {
		return nil, false
	}

	out := make([]byte, len(der))
	copy(out, der)
	// Bump SEQUENCE length 0x0e -> 0x0f. Total cert length is unchanged;
	// the trailing 0xff (BOOLEAN TRUE) is now correctly inside this
	// extension instead of being misread as the start of the next one.
	out[idx+1] = 0x0f

	return out, true
}

// marshalPreserving re-encodes a yaml.Node with 2-space indentation,
// matching the upstream Talos talosconfig style.
func marshalPreserving(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode yaml: %w", err)
	}

	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close yaml encoder: %w", err)
	}

	return buf.Bytes(), nil
}

// init registers a default [Repair] (using [DefaultPath]) so that any
// binary importing this package picks up the talosconfig CA repair
// automatically.
func init() {
	repairer.Register(&Repair{})
}
