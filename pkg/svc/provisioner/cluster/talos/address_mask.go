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

	matches := addressCandidatePattern.FindAllStringIndex(text, -1)
	if matches == nil {
		return text
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var masked strings.Builder

	last := 0

	for _, match := range matches {
		masked.WriteString(text[last:match[0]])
		masked.WriteString(m.maskRange(text, match[0], match[1]))

		last = match[1]
	}

	masked.WriteString(text[last:])

	return masked.String()
}

// Error returns an error whose text has server addresses replaced. The original
// error stays reachable, so errors.Is and errors.As behave as before.
func (m *addressMasker) Error(err error) error {
	if err == nil || m == nil || !m.enabled {
		return err
	}

	return &maskedError{err: err, masker: m}
}

// Writer wraps a writer so everything written through it is masked. Each write
// is masked on its own: the provisioner formats every progress line in full
// before writing it. A third-party writer that splits one address across two
// writes is not covered.
func (m *addressMasker) Writer(writer io.Writer) io.Writer {
	if writer == nil {
		return nil
	}

	if masked, ok := writer.(*addressMaskingWriter); ok && masked.masker == m {
		return writer
	}

	return &addressMaskingWriter{masker: m, writer: writer}
}

// maskRange masks text[start:end], one pattern match or what is left of one.
// The match can hold more than the address — a label and a colon before it, a
// port after it — so the longest part that parses as an address is replaced and
// the remainders are searched again. The caller holds the read lock.
func (m *addressMasker) maskRange(text string, start, end int) string {
	if start >= end {
		return ""
	}

	spanStart, spanEnd, addr, found := longestAddress(text, start, end)
	if !found {
		return text[start:end]
	}

	return m.maskRange(text, start, spanStart) +
		m.replacement(addr, text[spanStart:spanEnd]) +
		m.maskRange(text, spanEnd, end)
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

// longestAddress finds the longest part of text[start:end] that is an IP
// literal, cutting only at colons. An IPv6 literal that touches a letter, digit
// or underscore outside itself is not an address but a fragment of a longer
// word ("std::bad_alloc"), and is skipped.
func longestAddress(text string, start, end int) (int, int, netip.Addr, bool) {
	cuts := []int{start, end}

	for index := start; index < end; index++ {
		if text[index] == ':' {
			cuts = append(cuts, index, index+1)
		}
	}

	var (
		bestStart, bestEnd int
		bestAddr           netip.Addr
		found              bool
	)

	for _, from := range cuts {
		for _, until := range cuts {
			if until-from <= bestEnd-bestStart || until > end {
				continue
			}

			addr, err := netip.ParseAddr(text[from:until])
			if err != nil || (addr.Is6() && touchesWord(text, from, until)) {
				continue
			}

			bestStart, bestEnd, bestAddr, found = from, until, addr, true
		}
	}

	return bestStart, bestEnd, bestAddr, found
}

// touchesWord reports whether text[from:until] is directly preceded or followed
// by a letter, digit or underscore.
func touchesWord(text string, from, until int) bool {
	return (from > 0 && isWordByte(text[from-1])) || (until < len(text) && isWordByte(text[until]))
}

func isWordByte(char byte) bool {
	return char == '_' ||
		(char >= '0' && char <= '9') ||
		(char >= 'a' && char <= 'z') ||
		(char >= 'A' && char <= 'Z')
}

// isPublicAddress reports whether an address is routable beyond the local
// machine or a private network.
func isPublicAddress(addr netip.Addr) bool {
	return addr.IsGlobalUnicast() && !addr.IsPrivate()
}

// maskedError prints an error with server addresses replaced.
type maskedError struct {
	err    error
	masker *addressMasker
}

func (e *maskedError) Error() string { return e.masker.Mask(e.err.Error()) }

func (e *maskedError) Unwrap() error { return e.err }

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
