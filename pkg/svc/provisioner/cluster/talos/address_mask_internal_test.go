package talosprovisioner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/addressmask"
	"github.com/devantler-tech/ksail/v7/pkg/addressmask/addressmasktest"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/mock"
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

// TestUpdateProgressOutputCarriesNoAddress pins the default output of the
// update path: whatever the provisioner writes, and whatever change records it
// hands back for the caller's summary, must hold no server address.
func TestUpdateProgressOutputCarriesNoAddress(t *testing.T) {
	t.Setenv(addressmask.ShowAddressesEnvVar, "")

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

	if found := addressmasktest.PublicAddressesIn(text); len(found) > 0 {
		t.Fatalf("default update output names %d address(es): %v\n%s", len(found), found, text)
	}

	for _, name := range []string{"prod-control-plane-1", "prod-worker-1", addressmask.HiddenLabel} {
		if !strings.Contains(text, name) {
			t.Errorf("output no longer identifies %q:\n%s", name, text)
		}
	}
}

// TestProvisionerAlwaysWritesThroughTheMask guards the one place the mask is
// attached: a provisioner must never hold a bare writer.
func TestProvisionerAlwaysWritesThroughTheMask(t *testing.T) {
	t.Setenv(addressmask.ShowAddressesEnvVar, "")

	provisioner := NewProvisioner(nil, nil)
	if !addressmask.IsMasking(provisioner.logWriter) {
		t.Fatalf("default log writer is %T, want the masking writer", provisioner.logWriter)
	}

	var sink bytes.Buffer

	provisioner.WithLogWriter(&sink)

	if !addressmask.IsMasking(provisioner.logWriter) {
		t.Fatalf("WithLogWriter left %T, want the masking writer", provisioner.logWriter)
	}

	bare := (&Provisioner{}).WithLogWriter(&sink)
	if !addressmask.IsMasking(bare.logWriter) {
		t.Fatalf("a zero-value provisioner got %T, want the masking writer", bare.logWriter)
	}
}

// errTestDial is an error whose text carries an address, as transport errors do.
var errTestDial = &addressBearingError{}

type addressBearingError struct{}

func (*addressBearingError) Error() string {
	return "dial tcp " + testOtherAddressV4 + ":50000: connect: connection refused"
}

// TestLifecycleErrorsNameNoAddress pins that the errors of the entry points
// outside the update path name no server address either, and still expose their
// cause.
func TestLifecycleErrorsNameNoAddress(t *testing.T) {
	t.Setenv(addressmask.ShowAddressesEnvVar, "")

	infra := provider.NewMockProvider()
	infra.On("StartNodes", mock.Anything, mock.Anything).Return(errTestDial)
	infra.On("StopNodes", mock.Anything, mock.Anything).Return(errTestDial)

	provisioner := NewProvisioner(nil, nil).WithInfraProvider(infra).WithLogWriter(io.Discard)

	lifecycle := map[string]func(context.Context, string) error{
		"start": provisioner.Start,
		"stop":  provisioner.Stop,
	}

	for name, run := range lifecycle {
		err := run(t.Context(), "prod")
		if err == nil {
			t.Fatalf("%s: expected the provider's error", name)
		}

		if found := addressmasktest.PublicAddressesIn(err.Error()); len(found) > 0 {
			t.Errorf("%s: error names %v: %v", name, found, err)
		}

		if !errors.Is(err, errTestDial) {
			t.Errorf("%s: the masked error no longer exposes its cause", name)
		}
	}
}
