package talosprovisioner

import (
	"bytes"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
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

	masker := &addressMasker{labels: make(map[netip.Addr]string), enabled: true}
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

	masker := newAddressMasker()
	masker.Register(testNodeAddressV4, "prod-control-plane-1")

	line := "Rebooting " + testNodeAddressV4 + " via " + testOtherAddressV4
	if got := masker.Mask(line); got != line {
		t.Fatalf("opted-in output changed: %q", got)
	}
}

func TestAddressMaskerIgnoresUnusableRegistrations(t *testing.T) {
	t.Setenv(ShowAddressesEnvVar, "")

	masker := newAddressMasker()
	masker.Register("address unavailable", "prod-worker-9")
	masker.Register(testNodeAddressV4, "")

	got := masker.Mask("node " + testNodeAddressV4 + " (address unavailable)")

	want := "node " + hiddenAddressLabel + " (address unavailable)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAddressMaskingWriterReportsCallerLength(t *testing.T) {
	t.Setenv(ShowAddressesEnvVar, "")

	var sink bytes.Buffer

	masker := newAddressMasker()
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

// TestUpdateProgressOutputCarriesNoAddress pins the default output of the
// update path: whatever the provisioner writes, and whatever change records it
// hands back for the caller's summary, must hold no server address.
func TestUpdateProgressOutputCarriesNoAddress(t *testing.T) {
	t.Setenv(ShowAddressesEnvVar, "")

	var output bytes.Buffer

	provisioner := NewProvisioner(nil, nil).WithLogWriter(&output)
	provisioner.addressMask.Register(testNodeAddressV4, "prod-control-plane-1")
	provisioner.addressMask.Register(testNodeAddressV6, "prod-worker-1")

	controlPlane := nodeWithRole{IP: testNodeAddressV4, Role: RoleControlPlane}
	worker := nodeWithRole{IP: testNodeAddressV6, Role: RoleWorker}
	unnamed := nodeWithRole{IP: testOtherAddressV4, Role: RoleWorker}
	result := &clusterupdate.UpdateResult{}

	// The real reporting helpers of the update path.
	provisioner.recordNodeConfigFailure(controlPlane, result, "apply control-plane config: dial "+
		testNodeAddressV4+":50000: i/o timeout")
	provisioner.recordNodeConfigFailure(worker, result, "fetch worker config")
	provisioner.recordNodeConfigFailure(unnamed, result, "fetch worker config")
	recordFailedChange(result, RoleWorker, testOtherAddressV4, errTestDial)

	// Every other progress line of the package reaches the operator through the
	// same writer, so the remaining address-bearing shapes are written to it.
	for _, line := range []string{
		"  ✓ Synced cluster secrets and endpoint (" + testEndpointAddressV4 + ") from " +
			testNodeAddressV4 + "\n",
		"  ✓ Config applied to " + testNodeAddressV4 + " (control-plane, no reboot)\n",
		"    Rebooting " + testNodeAddressV6 + "...\n",
		"      " + testNodeAddressV6 + ": upgrade in progress\n",
		"  ✓ Floating IP " + testEndpointAddressV4 + " attached to prod-control-plane-1\n",
		"  ⏳ Waiting for kube-apiserver to restart on endpoint " + testEndpointAddressV4 + "\n",
	} {
		_, err := provisioner.logWriter.Write([]byte(line))
		if err != nil {
			t.Fatalf("write progress line: %v", err)
		}
	}

	provisioner.maskUpdateResult(result)

	var summary strings.Builder

	summary.WriteString(output.String())

	for _, changes := range [][]clusterupdate.Change{result.AppliedChanges, result.FailedChanges} {
		for _, change := range changes {
			summary.WriteString("\n" + change.OldValue)
			summary.WriteString("\n" + change.NewValue)
			summary.WriteString("\n" + change.Reason)
		}
	}

	text := summary.String()

	if found := addressesIn(text); len(found) > 0 {
		t.Fatalf("default update output names %d address(es): %v\n%s", len(found), found, text)
	}

	for _, name := range []string{"prod-control-plane-1", "prod-worker-1", hiddenAddressLabel} {
		if !strings.Contains(text, name) {
			t.Errorf("output no longer identifies %q:\n%s", name, text)
		}
	}
}

// TestProvisionerAlwaysWritesThroughTheMask guards the one place the mask is
// attached: a provisioner must never hold a bare writer.
func TestProvisionerAlwaysWritesThroughTheMask(t *testing.T) {
	t.Setenv(ShowAddressesEnvVar, "")

	provisioner := NewProvisioner(nil, nil)
	if _, ok := provisioner.logWriter.(*addressMaskingWriter); !ok {
		t.Fatalf("default log writer is %T, want the masking writer", provisioner.logWriter)
	}

	var sink bytes.Buffer

	provisioner.WithLogWriter(&sink)

	if _, ok := provisioner.logWriter.(*addressMaskingWriter); !ok {
		t.Fatalf("WithLogWriter left %T, want the masking writer", provisioner.logWriter)
	}

	bare := (&Provisioner{}).WithLogWriter(&sink)
	if _, ok := bare.logWriter.(*addressMaskingWriter); !ok {
		t.Fatalf("a zero-value provisioner got %T, want the masking writer", bare.logWriter)
	}
}

// errTestDial is an error whose text carries an address, as transport errors do.
var errTestDial = &addressBearingError{}

type addressBearingError struct{}

func (*addressBearingError) Error() string {
	return "dial tcp " + testOtherAddressV4 + ":50000: connect: connection refused"
}

// addressesIn finds publicly routable addresses in text without using the
// masker's own pattern, so a gap in that pattern cannot hide from this check.
func addressesIn(text string) []string {
	var found []string

	fields := strings.FieldsFunc(text, func(r rune) bool {
		return !strings.ContainsRune("0123456789abcdefABCDEF:.", r)
	})

	for _, field := range fields {
		for _, candidate := range []string{field, strings.TrimRight(field, ":."), portless(field)} {
			addr, err := netip.ParseAddr(candidate)
			if err == nil && isPublicAddress(addr.Unmap()) {
				found = append(found, candidate)

				break
			}
		}
	}

	return found
}

// portless drops a trailing ":port" from a dotted-quad field.
func portless(field string) string {
	if strings.Count(field, ":") != 1 {
		return field
	}

	host, _, _ := strings.Cut(field, ":")

	return host
}

// TestAddressMaskerErrorKeepsTheCause pins that hiding an address in an error's
// text does not hide the error itself from errors.Is and errors.As.
func TestAddressMaskerErrorKeepsTheCause(t *testing.T) {
	t.Setenv(ShowAddressesEnvVar, "")

	masker := newAddressMasker()
	masker.Register(testNodeAddressV4, "prod-control-plane-1")

	cause := fmt.Errorf("rebooting node %s: %w", testNodeAddressV4, errTestDial)
	masked := masker.Error(cause)

	want := "rebooting node prod-control-plane-1: dial tcp " + hiddenAddressLabel +
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
	if found := addressesIn(wrapped.Error()); len(found) > 0 {
		t.Fatalf("a wrapped masked error names %v", found)
	}
}
