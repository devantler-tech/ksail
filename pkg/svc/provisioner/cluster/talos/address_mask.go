package talosprovisioner

import (
	"io"
	"net/netip"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// ShowAddressesEnvVar names the environment variable that opts back in to
// printing server addresses. Progress output hides them by default because it
// is routinely captured by public CI logs, where a server's address discloses
// part of the cluster's layout without the operator choosing to publish it.
const ShowAddressesEnvVar = "KSAIL_SHOW_ADDRESSES"

// hiddenAddressLabel replaces an address the provisioner has no name for.
const hiddenAddressLabel = "<address hidden>"

// addressCandidatePattern finds text that may be an IP literal. Every match is
// then parsed, so the pattern only has to be generous: two or more colon-ended
// hexadecimal groups followed by a dotted quad or a last group, or a dotted quad
// on its own. The quad is tried before the last group so an IPv4-mapped address
// is taken whole.
var addressCandidatePattern = regexp.MustCompile(
	`(?:[0-9A-Fa-f]{0,4}:){2,}(?:(?:[0-9]{1,3}\.){3}[0-9]{1,3}|[0-9A-Fa-f]{0,4})` +
		`|(?:[0-9]{1,3}\.){3}[0-9]{1,3}`,
)

// addressMasker rewrites server addresses out of text meant for an operator's
// terminal or a CI log. An address registered with a name is replaced by that
// name, so a line still says which node it concerns; any other publicly
// routable address is replaced by a fixed placeholder. Loopback, private and
// link-local addresses that were never registered are left alone: they are the
// local endpoints of container-based clusters and name no server.
type addressMasker struct {
	mu      sync.RWMutex
	labels  map[netip.Addr]string
	enabled bool
}

// newAddressMasker returns a masker that hides addresses unless the operator
// opted back in through ShowAddressesEnvVar.
func newAddressMasker() *addressMasker {
	show, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv(ShowAddressesEnvVar)))

	return &addressMasker{
		labels:  make(map[netip.Addr]string),
		enabled: !show,
	}
}

// Register names an address, so later output reports the name instead. An
// unparsable address or an empty name is ignored: the address then falls back
// to the generic rule, which never prints it when it is publicly routable.
func (m *addressMasker) Register(address, name string) {
	if m == nil || name == "" {
		return
	}

	addr, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.labels[addr.Unmap()] = name
}

// Mask returns text with every server address replaced.
func (m *addressMasker) Mask(text string) string {
	if m == nil || !m.enabled || text == "" {
		return text
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	return addressCandidatePattern.ReplaceAllStringFunc(text, m.maskCandidate)
}

// Writer wraps a writer so everything written through it is masked. Each write
// is masked on its own, which is sufficient for the provisioner's progress
// output: every line is formatted in full before it is written.
func (m *addressMasker) Writer(writer io.Writer) io.Writer {
	if writer == nil {
		return nil
	}

	if masked, ok := writer.(*addressMaskingWriter); ok && masked.masker == m {
		return writer
	}

	return &addressMaskingWriter{masker: m, writer: writer}
}

// maskCandidate replaces one pattern match when it really is an address that
// must not be printed, and returns it unchanged otherwise. A candidate that
// ends in a separator colon ("from 2001:db8::1:") is retried without it.
func (m *addressMasker) maskCandidate(candidate string) string {
	suffix := ""
	literal := candidate

	for {
		addr, err := netip.ParseAddr(literal)
		if err == nil {
			return m.replacement(addr, literal) + suffix
		}

		if !strings.HasSuffix(literal, ":") || strings.HasSuffix(literal, "::") {
			return candidate
		}

		literal = strings.TrimSuffix(literal, ":")
		suffix = ":" + suffix
	}
}

// replacement picks what to print for a parsed address. The caller holds the
// read lock.
func (m *addressMasker) replacement(addr netip.Addr, literal string) string {
	addr = addr.Unmap()

	if name, ok := m.labels[addr]; ok {
		return name
	}

	if isPublicAddress(addr) {
		return hiddenAddressLabel
	}

	return literal
}

// isPublicAddress reports whether an address is routable beyond the local
// machine or a private network.
func isPublicAddress(addr netip.Addr) bool {
	return addr.IsGlobalUnicast() && !addr.IsPrivate()
}

// addressMaskingWriter masks each write before passing it on.
type addressMaskingWriter struct {
	masker *addressMasker
	writer io.Writer
}

// Write masks the payload and reports the caller's byte count on success, as
// io.Writer requires even though the masked payload may differ in length.
func (w *addressMaskingWriter) Write(payload []byte) (int, error) {
	masked := w.masker.Mask(string(payload))

	_, err := io.WriteString(w.writer, masked)
	if err != nil {
		return 0, err //nolint:wrapcheck // a transparent writer returns the sink's error as is
	}

	return len(payload), nil
}
