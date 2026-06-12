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
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
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

// filePermUserRW is the standard permission for user-private files
// (read/write owner only). Used for the talosconfig and its backup.
const filePermUserRW os.FileMode = 0o600

// yamlIndentWidth matches the upstream Talos talosconfig 2-space style.
const yamlIndentWidth = 2

// backupPathRetries bounds [uniqueBackupPath] retries before giving up.
const backupPathRetries = 8

// backupRandSuffixBytes controls the entropy of the backup-path random
// suffix; 4 bytes (32 bits) is overwhelmingly sufficient given retries.
const backupRandSuffixBytes = 4

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

// errBackupPathAllocFailed is returned by [Repair.uniqueBackupPath]
// when it cannot allocate a non-colliding backup path within a bounded
// number of attempts.
var errBackupPathAllocFailed = errors.New("could not allocate unique backup path after retries")

// Repair implements [repairer.Repair].
type Repair struct {
	// Path is the talosconfig path. Empty means [DefaultPath].
	Path string
	// Now overrides time.Now for deterministic backup filenames in
	// tests. Nil falls back to time.Now.
	Now func() time.Time
}

// DefaultRepairs returns the standard set of repairs run by
// `ksail cluster repair`: currently just the talosconfig CA repair
// (using [DefaultPath]). It lives in this package rather than the
// parent repairer package because the parent cannot import this
// package without an import cycle.
func DefaultRepairs() []repairer.Repair {
	return []repairer.Repair{&Repair{}}
}

// Name returns the stable identifier "talosconfig-ca".
func (r *Repair) Name() string { return repairName }

// Run loads the talosconfig file, attempts to repair every context whose
// CA does not parse, writes a timestamped backup before overwriting,
// and returns a single [repairer.Result] summarising the outcome.
func (r *Repair) Run(_ context.Context, logWriter io.Writer) repairer.Result {
	path, data, result, loaded := r.loadConfig()
	if !loaded {
		return result
	}

	doc, parseResult, parsed := parseYAML(path, data)
	if !parsed {
		return parseResult
	}

	repaired, unrepairable, alreadyValid := r.walkAndRepair(doc, logWriter)

	if repaired == 0 {
		return r.summarizeNoRepair(path, unrepairable, alreadyValid)
	}

	result = r.persistRepairedConfig(path, doc, data, repaired)
	if result.Status != repairer.StatusRepaired {
		return result
	}

	if unrepairable > 0 {
		result.Status = repairer.StatusUnrepairable
		result.Detail = fmt.Sprintf(
			"%s: repaired %d CA(s), but %d context(s) remain unrepairable",
			path, repaired, unrepairable,
		)
	}

	return result
}

// loadConfig resolves [Repair.Path], canonicalizes it, and reads the
// raw bytes. The third return is the result to surface when loaded is
// false; loaded is true only when the file was successfully read.
func (r *Repair) loadConfig() (string, []byte, repairer.Result, bool) {
	rawPath := r.Path
	if rawPath == "" {
		rawPath = DefaultPath
	}

	expanded, err := fsutil.ExpandHomePath(rawPath)
	if err != nil {
		return "", nil, repairer.Result{
			Name:   repairName,
			Status: repairer.StatusSkipped,
			Detail: fmt.Sprintf("could not expand path %q: %v", rawPath, err),
			Err:    err,
		}, false
	}

	// Canonicalize the user-supplied path so we never read or write
	// through symlinks. EvalCanonicalPath falls back to the parent
	// directory when the file itself does not yet exist, so a missing
	// talosconfig is still reported through the os.ReadFile branch
	// below rather than failing canonicalization.
	path, err := fsutil.EvalCanonicalPath(expanded)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, repairer.Result{
				Name:   repairName,
				Status: repairer.StatusSkipped,
				Detail: expanded + " does not exist",
			}, false
		}

		return "", nil, repairer.Result{
			Name:   repairName,
			Status: repairer.StatusSkipped,
			Detail: fmt.Sprintf("could not canonicalize %q: %v", expanded, err),
			Err:    err,
		}, false
	}

	data, err := os.ReadFile(path) //nolint:gosec // path canonicalized via EvalCanonicalPath
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, repairer.Result{
				Name:   repairName,
				Status: repairer.StatusSkipped,
				Detail: path + " does not exist",
			}, false
		}

		return "", nil, repairer.Result{
			Name:   repairName,
			Status: repairer.StatusSkipped,
			Detail: fmt.Sprintf("could not read %s: %v", path, err),
			Err:    err,
		}, false
	}

	return path, data, repairer.Result{}, true
}

// parseYAML decodes data into a YAML document tree. The third return
// is the result to surface when parsed is false.
func parseYAML(path string, data []byte) (*yaml.Node, repairer.Result, bool) {
	var doc yaml.Node

	err := yaml.Unmarshal(data, &doc)
	if err != nil {
		return nil, repairer.Result{
			Name:   repairName,
			Status: repairer.StatusUnrepairable,
			Detail: fmt.Sprintf("%s: invalid YAML: %v", path, err),
			Err:    err,
		}, false
	}

	return &doc, repairer.Result{}, true
}

// summarizeNoRepair produces a result for the "nothing repaired" case,
// distinguishing between unrepairable contexts, already-valid contexts,
// and a config with no CA fields at all.
func (r *Repair) summarizeNoRepair(path string, unrepairable, alreadyValid int) repairer.Result {
	switch {
	case unrepairable > 0:
		return repairer.Result{
			Name:   repairName,
			Status: repairer.StatusUnrepairable,
			Detail: fmt.Sprintf(
				"%s: %d context(s) malformed but no known repair pattern matches",
				path, unrepairable,
			),
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
			Detail: path + ": no contexts with a CA found",
		}
	}
}

// persistRepairedConfig marshals the repaired YAML, writes a backup of
// the original bytes, and atomically replaces the original file.
func (r *Repair) persistRepairedConfig(
	path string,
	doc *yaml.Node,
	originalData []byte,
	repaired int,
) repairer.Result {
	out, err := marshalPreserving(doc)
	if err != nil {
		return repairer.Result{
			Name:   repairName,
			Status: repairer.StatusUnrepairable,
			Detail: fmt.Sprintf("%s: failed to re-marshal YAML: %v", path, err),
			Err:    err,
		}
	}

	backup, err := r.uniqueBackupPath(path)
	if err != nil {
		return repairer.Result{
			Name:   repairName,
			Status: repairer.StatusUnrepairable,
			Detail: fmt.Sprintf("%s: failed to allocate backup path: %v", path, err),
			Err:    err,
		}
	}

	err = fsutil.AtomicWriteFile(backup, originalData, filePermUserRW)
	if err != nil {
		return repairer.Result{
			Name:   repairName,
			Status: repairer.StatusUnrepairable,
			Detail: fmt.Sprintf("%s: failed to write backup %s: %v", path, backup, err),
			Err:    err,
		}
	}

	err = fsutil.AtomicWriteFile(path, out, filePermUserRW)
	if err != nil {
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

// uniqueBackupPath returns a backup path that is guaranteed not to
// collide with an existing file, even when multiple repairs run within
// the same second. The format is `<path>.bak.<UTC timestamp ns>-<rand>`.
func (r *Repair) uniqueBackupPath(path string) (string, error) {
	for attempt := range backupPathRetries {
		_ = attempt

		var randBytes [backupRandSuffixBytes]byte

		_, err := rand.Read(randBytes[:])
		if err != nil {
			return "", fmt.Errorf("read random bytes: %w", err)
		}

		candidate := fmt.Sprintf(
			"%s.bak.%s-%s",
			path,
			r.now().UTC().Format("20060102-150405.000000000"),
			hex.EncodeToString(randBytes[:]),
		)

		_, statErr := os.Stat(candidate)
		if errors.Is(statErr, os.ErrNotExist) {
			return candidate, nil
		} else if statErr != nil {
			return "", fmt.Errorf("stat backup candidate: %w", statErr)
		}
	}

	return "", errBackupPathAllocFailed
}

// walkAndRepair traverses the YAML document looking for `contexts.<name>.ca`
// scalar nodes, attempts to repair each, and returns counts.
func (r *Repair) walkAndRepair(
	doc *yaml.Node,
	logWriter io.Writer,
) (int, int, int) {
	var repaired, unrepairable, alreadyValid int

	walkContextCANodes(doc, func(ctxName string, caNode *yaml.Node) {
		switch tryRepairContextCA(ctxName, caNode, logWriter) {
		case caStatusRepaired:
			repaired++
		case caStatusAlreadyValid:
			alreadyValid++
		case caStatusUnrepairable:
			unrepairable++
		case caStatusEmpty:
			// nothing to count
		}
	})

	return repaired, unrepairable, alreadyValid
}

// caRepairStatus enumerates the per-context outcomes considered by
// [walkAndRepair].
type caRepairStatus int

const (
	caStatusEmpty caRepairStatus = iota
	caStatusRepaired
	caStatusAlreadyValid
	caStatusUnrepairable
)

// tryRepairContextCA inspects a single context's `ca` scalar node,
// repairs it in place when the corruption signature matches, and
// returns the outcome category.
func tryRepairContextCA(ctxName string, caNode *yaml.Node, logWriter io.Writer) caRepairStatus {
	if caNode == nil || caNode.Value == "" {
		return caStatusEmpty
	}

	caBytes, err := base64.StdEncoding.DecodeString(caNode.Value)
	if err != nil {
		_, _ = fmt.Fprintf(logWriter, "  [%s] base64 decode failed: %v (skipped)\n", ctxName, err)

		return caStatusUnrepairable
	}

	block, _ := pem.Decode(caBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		_, _ = fmt.Fprintf(logWriter, "  [%s] not a PEM CERTIFICATE block (skipped)\n", ctxName)

		return caStatusUnrepairable
	}

	_, parseErr := x509.ParseCertificate(block.Bytes)
	if parseErr == nil {
		_, _ = fmt.Fprintf(logWriter, "  [%s] CA already valid\n", ctxName)

		return caStatusAlreadyValid
	}

	fixed, ok := tryRepair(block.Bytes)
	if !ok {
		_, _ = fmt.Fprintf(logWriter, "  [%s] %v\n", ctxName, ErrPatternNotMatched)

		return caStatusUnrepairable
	}

	_, postErr := x509.ParseCertificate(fixed)
	if postErr != nil {
		_, _ = fmt.Fprintf(
			logWriter,
			"  [%s] repair did not produce a valid cert: %v\n",
			ctxName, postErr,
		)

		return caStatusUnrepairable
	}

	newPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: fixed})
	caNode.Value = base64.StdEncoding.EncodeToString(newPEM)
	caNode.Style = yaml.DoubleQuotedStyle
	caNode.Tag = "!!str"

	_, _ = fmt.Fprintf(logWriter, "  [%s] repaired ✓\n", ctxName)

	return caStatusRepaired
}

// walkContextCANodes finds every `contexts.<name>.ca` scalar node in the
// YAML document and invokes visit with the context name and the value
// node.
func walkContextCANodes(doc *yaml.Node, visit func(name string, caNode *yaml.Node)) {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		walkContextCANodes(doc.Content[0], visit)

		return
	}

	if doc.Kind != yaml.MappingNode {
		return
	}

	contextsNode := findContextsMapping(doc)
	if contextsNode == nil {
		return
	}

	visitContextCANodes(contextsNode, visit)
}

// findContextsMapping returns the value node for the top-level
// `contexts` key when it is itself a mapping; otherwise nil.
func findContextsMapping(doc *yaml.Node) *yaml.Node {
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value != "contexts" {
			continue
		}

		ctxs := doc.Content[i+1]
		if ctxs.Kind == yaml.MappingNode {
			return ctxs
		}
	}

	return nil
}

// visitContextCANodes iterates the children of the `contexts` mapping,
// invoking visit with each context's name and the inner `ca` scalar node.
func visitContextCANodes(ctxs *yaml.Node, visit func(name string, caNode *yaml.Node)) {
	for j := 0; j+1 < len(ctxs.Content); j += 2 {
		ctxName := ctxs.Content[j].Value

		ctxBody := ctxs.Content[j+1]
		if ctxBody.Kind != yaml.MappingNode {
			continue
		}

		for k := 0; k+1 < len(ctxBody.Content); k += 2 {
			if ctxBody.Content[k].Value == "ca" {
				visit(ctxName, ctxBody.Content[k+1])
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
	enc.SetIndent(yamlIndentWidth)

	err := enc.Encode(doc)
	if err != nil {
		return nil, fmt.Errorf("encode yaml: %w", err)
	}

	err = enc.Close()
	if err != nil {
		return nil, fmt.Errorf("close yaml encoder: %w", err)
	}

	return buf.Bytes(), nil
}
