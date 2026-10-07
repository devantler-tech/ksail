package addressmask

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/addressmask/addressmasktest"
)

// Documentation-range addresses (RFC 5737, RFC 3849) stand in for real servers.
// They are not "private", so the masker treats them exactly like a public
// server address.
const (
	testNodeAddressV4     = "203.0.113.10"
	testOtherAddressV4    = "198.51.100.7"
	testNodeAddressV6     = "2001:db8::10"
	testEndpointAddressV4 = "192.0.2.44"
)

type maskCase struct {
	name string
	in   string
	want string
}

func maskCases() []maskCase {
	return []maskCase{
		{
			name: "registered address becomes the node name",
			in:   "  ✓ Config applied to 203.0.113.10 (control-plane, no reboot)\n",
			want: "  ✓ Config applied to prod-control-plane-1 (control-plane, no reboot)\n",
		},
		{
			name: "registered IPv6 address becomes the node name",
			in:   "    Rebooting 2001:db8::10...\n",
			want: "    Rebooting prod-worker-1...\n",
		},
		{
			name: "unregistered public address is hidden",
			in:   "endpoint (192.0.2.44) from 203.0.113.10",
			want: "endpoint (<address hidden>) from prod-control-plane-1",
		},
		{
			name: "port and brackets survive",
			in:   "dial 198.51.100.7:6443 and [2001:db8::99]:50000",
			want: "dial <address hidden>:6443 and [<address hidden>]:50000",
		},
		{
			name: "address followed by a separator colon",
			in:   "      2001:db8::99: upgrading",
			want: "      <address hidden>: upgrading",
		},
		{
			name: "IPv4-mapped form of a registered address",
			in:   "from ::ffff:203.0.113.10",
			want: "from prod-control-plane-1",
		},
		{
			name: "local endpoints are left alone",
			in:   "► Talos API → 127.0.0.1:50000, bridge 10.5.0.2, link fe80::1, any ::",
			want: "► Talos API → 127.0.0.1:50000, bridge 10.5.0.2, link fe80::1, any ::",
		},
		{
			name: "address glued to a label or a port",
			in:   "addr:2001:db8:1:2:3:4:5:6 2001:db8:1:2:3:4:5:6:50000 tcp:10:198.51.100.7",
			want: "addr:<address hidden> <address hidden>:50000 tcp:10:<address hidden>",
		},
		{
			name: "scope operators and digests are not addresses",
			in:   "std::bad_alloc node::default sha256:deadbeef:cafe",
			want: "std::bad_alloc node::default sha256:deadbeef:cafe",
		},
		{
			name: "clock times, hardware addresses and versions are not addresses",
			in:   "03:44:37 aa:bb:cc:dd:ee:ff v1.14.2 300.1.2.3 k8s 1.37.1",
			want: "03:44:37 aa:bb:cc:dd:ee:ff v1.14.2 300.1.2.3 k8s 1.37.1",
		},
	}
}

func TestAddressMaskerMask(t *testing.T) {
	t.Parallel()

	masker := &Masker{labels: make(map[netip.Addr]string), enabled: true}
	masker.Register(testNodeAddressV4, "prod-control-plane-1")
	masker.Register(testNodeAddressV6, "prod-worker-1")

	for _, test := range maskCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := masker.Mask(test.in)
			if got != test.want {
				t.Fatalf("Mask(%q)\n got: %q\nwant: %q", test.in, got, test.want)
			}
		})
	}
}

func TestAddressMaskerOptIn(t *testing.T) {
	t.Setenv(ShowAddressesEnvVar, "true")

	masker := New()
	masker.Register(testNodeAddressV4, "prod-control-plane-1")

	line := "Rebooting " + testNodeAddressV4 + " via " + testOtherAddressV4
	if got := masker.Mask(line); got != line {
		t.Fatalf("opted-in output changed: %q", got)
	}
}

func TestAddressMaskerIgnoresUnusableRegistrations(t *testing.T) {
	t.Setenv(ShowAddressesEnvVar, "")

	masker := New()
	masker.Register("address unavailable", "prod-worker-9")
	masker.Register(testNodeAddressV4, "")

	got := masker.Mask("node " + testNodeAddressV4 + " (address unavailable)")

	want := "node " + HiddenLabel + " (address unavailable)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAddressMaskingWriterReportsCallerLength(t *testing.T) {
	t.Setenv(ShowAddressesEnvVar, "")

	var sink bytes.Buffer

	masker := New()
	writer := masker.Writer(&sink)
	payload := []byte("from " + testOtherAddressV4 + "\n")

	written, err := writer.Write(payload)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if written != len(payload) {
		t.Fatalf("Write reported %d bytes, want the caller's %d", written, len(payload))
	}

	if masker.Writer(writer) != writer {
		t.Fatal("wrapping an already masked writer must not stack a second mask")
	}
}

// errTestDial is an error whose text carries an address, as transport errors do.
var errTestDial = &addressBearingError{}

type addressBearingError struct{}

func (*addressBearingError) Error() string {
	return "dial tcp " + testOtherAddressV4 + ":50000: connect: connection refused"
}

// TestAddressMaskerErrorKeepsTheCause pins that hiding an address in an error's
// text does not hide the error itself from errors.Is and errors.As.
func TestAddressMaskerErrorKeepsTheCause(t *testing.T) {
	t.Setenv(ShowAddressesEnvVar, "")

	masker := New()
	masker.Register(testNodeAddressV4, "prod-control-plane-1")

	cause := fmt.Errorf("rebooting node %s: %w", testNodeAddressV4, errTestDial)
	masked := masker.Error(cause)

	want := "rebooting node prod-control-plane-1: dial tcp " + HiddenLabel +
		":50000: connect: connection refused"
	if masked.Error() != want {
		t.Fatalf("got %q, want %q", masked.Error(), want)
	}

	var dial *addressBearingError
	if !errors.Is(masked, cause) || !errors.As(masked, &dial) {
		t.Fatal("the masked error no longer exposes its cause")
	}

	if masker.Error(nil) != nil {
		t.Fatal("a nil error must stay nil")
	}

	wrapped := fmt.Errorf("failed to apply updates: %w", masked)
	if found := addressmasktest.PublicAddressesIn(wrapped.Error()); len(found) > 0 {
		t.Fatalf("a wrapped masked error names %v", found)
	}
}

// syncBuffer is a sink the delayed flush can write to while a test reads it.
type syncBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *syncBuffer) Write(payload []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buffer.Write(payload) //nolint:wrapcheck // test sink
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buffer.String()
}

// splitWriter returns a masking writer whose held text is only passed on by a
// later write or an explicit flush, so a test controls when that happens.
func splitWriter(t *testing.T, sink *syncBuffer) *maskingWriter {
	t.Helper()

	masker := &Masker{labels: make(map[netip.Addr]string), enabled: true}
	masker.Register(testNodeAddressV4, "prod-control-plane-1")
	masker.Register(testNodeAddressV6, "prod-worker-1")

	writer, ok := masker.Writer(sink).(*maskingWriter)
	if !ok {
		t.Fatal("Writer did not return a masking writer")
	}

	writer.flushAfter = time.Hour

	return writer
}

// TestMaskingWriterJoinsAnAddressSplitAcrossWrites writes every address-bearing
// case one byte at a time and at every two-way split: the output must be what a
// single write produces, so no split position lets an address through.
func TestMaskingWriterJoinsAnAddressSplitAcrossWrites(t *testing.T) {
	t.Parallel()

	for _, test := range maskCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			for split := 0; split <= len(test.in); split++ {
				sink := &syncBuffer{}
				writer := splitWriter(t, sink)

				writeAll(t, writer, test.in[:split], test.in[split:])
				writer.flush()

				if got := sink.String(); got != maskedWhole(test.in) {
					t.Fatalf("split at %d\n got: %q\nwant: %q", split, got, maskedWhole(test.in))
				}
			}

			sink := &syncBuffer{}
			writer := splitWriter(t, sink)

			for index := range len(test.in) {
				writeAll(t, writer, test.in[index:index+1])
			}

			writer.flush()

			if got := sink.String(); got != maskedWhole(test.in) {
				t.Fatalf("byte-wise\n got: %q\nwant: %q", got, maskedWhole(test.in))
			}
		})
	}
}

// maskedWhole is what one write of text produces.
func maskedWhole(text string) string {
	masker := &Masker{labels: make(map[netip.Addr]string), enabled: true}
	masker.Register(testNodeAddressV4, "prod-control-plane-1")
	masker.Register(testNodeAddressV6, "prod-worker-1")

	return masker.Mask(text)
}

func writeAll(t *testing.T, writer io.Writer, parts ...string) {
	t.Helper()

	for _, part := range parts {
		written, err := writer.Write([]byte(part))
		if err != nil || written != len(part) {
			t.Fatalf("Write(%q) = %d, %v", part, written, err)
		}
	}
}

// TestMaskingWriterPassesPartialLinesOn pins that only text that could begin an
// address is held back, and that held text is passed on without a further
// write, so a progress indicator is neither delayed for long nor dropped.
func TestMaskingWriterPassesPartialLinesOn(t *testing.T) {
	t.Parallel()

	sink := &syncBuffer{}
	writer := splitWriter(t, sink)

	writeAll(t, writer, "Waiting for nodes...")

	if got := sink.String(); got != "Waiting for nodes..." {
		t.Fatalf("a partial line was held back: %q", got)
	}

	writeAll(t, writer, " attempt 12")

	if got := sink.String(); got != "Waiting for nodes... attempt " {
		t.Fatalf("got %q, want everything but the trailing number", got)
	}

	writer.flushAfter = time.Millisecond
	writeAll(t, writer, "3")

	deadline := time.Now().Add(5 * time.Second)
	for sink.String() != "Waiting for nodes... attempt 123" {
		if time.Now().After(deadline) {
			t.Fatalf("held text was never passed on: %q", sink.String())
		}

		time.Sleep(time.Millisecond)
	}
}

// TestMaskingWriterOptInWritesThrough pins that an operator who asked to see
// addresses gets every write unchanged and at once.
func TestMaskingWriterOptInWritesThrough(t *testing.T) {
	t.Setenv(ShowAddressesEnvVar, "true")

	var sink bytes.Buffer

	writer := New().Writer(&sink)
	writeAll(t, writer, "endpoint "+testOtherAddressV4)

	if got := sink.String(); got != "endpoint "+testOtherAddressV4 {
		t.Fatalf("got %q", got)
	}
}
