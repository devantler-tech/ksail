package talosprovisioner_test

import (
	"bytes"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	hetzner "github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// labelListingCounter counts the cluster-label floating-IP listings the stray check makes.
type labelListingCounter struct {
	next     http.RoundTripper
	listings *atomic.Int32
}

// RoundTrip counts a label-selected floating-IP listing and passes every request on.
func (counter labelListingCounter) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Path == "/floating_ips" && request.URL.Query().Get("label_selector") != "" {
		counter.listings.Add(1)
	}

	return counter.next.RoundTrip(request) //nolint:wrapcheck // a transparent test transport
}

// runNoChangeFloatingIPStep runs the update's floating-IP step for an update that changes
// neither the endpoint nor the control plane, against a Hetzner API that answers the
// cluster-label listing with labelStatus and labelBody. It returns the provisioner output,
// the number of attach calls and the number of label listings.
// strayCheckProvisioner is a Hetzner Talos provisioner whose API answers the cluster-label
// floating-IP listing with labelStatus and labelBody, with counters for that listing and for
// attach calls.
type strayCheckProvisioner struct {
	provisioner   *talosprovisioner.Provisioner
	output        *bytes.Buffer
	assignCalls   *atomic.Int32
	labelListings *atomic.Int32
}

func newStrayCheckProvisioner(
	t *testing.T,
	floatingIPEnabled bool,
	labelStatus int,
	labelBody string,
) strayCheckProvisioner {
	t.Helper()

	var assignCalls, labelListings atomic.Int32

	server := strayFloatingIPEndpointTestServer(t, labelStatus, labelBody, &assignCalls)
	client := hcloud.NewClient(
		hcloud.WithToken("test-token"),
		hcloud.WithEndpoint(server.URL),
		hcloud.WithHTTPClient(&http.Client{Transport: labelListingCounter{
			next:     http.DefaultTransport,
			listings: &labelListings,
		}}),
	)

	output := &bytes.Buffer{}

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  floatingIPEnabled,
		FloatingIPLocation: "fsn1",
		TokenEnvVar:        testFloatingIPTokenEnvVar,
	}).WithInfraProvider(hetzner.NewProvider(client)).WithLogWriter(output)

	return strayCheckProvisioner{
		provisioner: provisioner, output: output,
		assignCalls: &assignCalls, labelListings: &labelListings,
	}
}

func runNoChangeFloatingIPStep(
	t *testing.T,
	floatingIPEnabled bool,
	labelStatus int,
	labelBody string,
) (string, int32, int32) {
	t.Helper()

	stray := newStrayCheckProvisioner(t, floatingIPEnabled, labelStatus, labelBody)

	spec := &v1alpha1.ClusterSpec{ControlPlanes: 1, Workers: 1}

	require.NoError(t, stray.provisioner.RunUpdateApplyStepForTest(
		t.Context(), "reconcile floating IP endpoint", "fip-cluster",
		spec, spec, clusterupdate.NewEmptyUpdateResult(), clusterupdate.NewEmptyUpdateResult(),
	), "the stray check must never fail the update")

	return stray.output.String(), stray.assignCalls.Load(), stray.labelListings.Load()
}

// TestUpdateApplyStep_NoChangeUpdateWarnsAboutStrayFloatingIP verifies that an update which
// leaves the endpoint and the control plane alone still reports a stray cluster-labelled
// floating IP (ksail#7695), without attaching or preparing anything.
func TestUpdateApplyStep_NoChangeUpdateWarnsAboutStrayFloatingIP(t *testing.T) {
	t.Parallel()

	output, assigns, listings := runNoChangeFloatingIPStep(t, true, http.StatusOK,
		`{"floating_ips":[`+fipUpdateOwnedFloatingIPJSON+`,`+fipStrayFloatingIPJSON+`]}`)

	assert.Equal(t, int32(1), listings, "the stray check must run on a no-change update")
	assert.Zero(t, assigns, "a no-change update must not attach the floating IP")
	assert.Contains(t, output, "fip-cluster-floating-ip-old")
	assert.NotContains(t, output, "No stray floating IPs")
}

// TestUpdateApplyStep_NoChangeUpdateSaysNoStrayFloatingIPFound verifies the check leaves a
// line when it finds nothing, so a log without a warning reads as checked rather than skipped.
func TestUpdateApplyStep_NoChangeUpdateSaysNoStrayFloatingIPFound(t *testing.T) {
	t.Parallel()

	output, assigns, listings := runNoChangeFloatingIPStep(t, true, http.StatusOK,
		`{"floating_ips":[`+fipUpdateOwnedFloatingIPJSON+`]}`)

	assert.Equal(t, int32(1), listings)
	assert.Zero(t, assigns)
	assert.Contains(t, output, "No stray floating IPs labelled for this cluster")
	assert.NotContains(t, output, "is labelled for this cluster but not managed by it")
}

// TestUpdateApplyStep_NoChangeUpdateReportsFailedStrayLookup verifies a failed listing is
// reported, not read as a clean account, and does not fail the update.
func TestUpdateApplyStep_NoChangeUpdateReportsFailedStrayLookup(t *testing.T) {
	t.Parallel()

	output, assigns, listings := runNoChangeFloatingIPStep(t, true, http.StatusUnauthorized,
		`{"error":{"code":"unauthorized","message":"unable to authenticate"}}`)

	assert.Equal(t, int32(1), listings)
	assert.Zero(t, assigns)
	assert.Contains(t, output, "Failed to check for stray floating IPs")
	assert.NotContains(t, output, "No stray floating IPs")
}

// TestUpdateApplyStep_NoChangeUpdateSkipsStrayCheckWithoutFloatingIP verifies that a cluster
// without a floating IP endpoint makes no floating-IP call on a no-change update.
func TestUpdateApplyStep_NoChangeUpdateSkipsStrayCheckWithoutFloatingIP(t *testing.T) {
	t.Parallel()

	output, assigns, listings := runNoChangeFloatingIPStep(t, false, http.StatusOK,
		`{"floating_ips":[`+fipStrayFloatingIPJSON+`]}`)

	assert.Zero(t, listings)
	assert.Zero(t, assigns)
	assert.Empty(t, output)
}

// TestAuditUpdate_UnchangedUpdateRunsStrayCheck verifies that an update with no changes at
// all, which runs only the audit, still runs the stray check and says so (ksail#7695).
func TestAuditUpdate_UnchangedUpdateRunsStrayCheck(t *testing.T) {
	t.Parallel()

	stray := newStrayCheckProvisioner(t, true, http.StatusOK,
		`{"floating_ips":[`+fipUpdateOwnedFloatingIPJSON+`]}`)

	err := stray.provisioner.AuditUpdate(
		t.Context(),
		"fip-cluster",
		clusterupdate.NewEmptyUpdateResult(),
	)

	require.NoError(t, err)
	assert.Equal(t, int32(1), stray.labelListings.Load(), "the audit must run the stray check")
	assert.Zero(t, stray.assignCalls.Load(), "the audit must not attach the floating IP")
	assert.Contains(t, stray.output.String(), "No stray floating IPs labelled for this cluster")
}

// TestAuditUpdate_UnchangedUpdateWarnsAboutStrayFloatingIP verifies the audit reports a stray
// cluster-labelled floating IP on an update with no changes at all.
func TestAuditUpdate_UnchangedUpdateWarnsAboutStrayFloatingIP(t *testing.T) {
	t.Parallel()

	stray := newStrayCheckProvisioner(t, true, http.StatusOK,
		`{"floating_ips":[`+fipUpdateOwnedFloatingIPJSON+`,`+fipStrayFloatingIPJSON+`]}`)

	err := stray.provisioner.AuditUpdate(
		t.Context(),
		"fip-cluster",
		clusterupdate.NewEmptyUpdateResult(),
	)

	require.NoError(t, err)
	assert.Contains(t, stray.output.String(), "fip-cluster-floating-ip-old")
}

// TestAuditUpdate_SkipsStrayCheckWithoutFloatingIP verifies the audit makes no floating-IP
// call when the floating IP is disabled.
func TestAuditUpdate_SkipsStrayCheckWithoutFloatingIP(t *testing.T) {
	t.Parallel()

	stray := newStrayCheckProvisioner(t, false, http.StatusOK, `{"floating_ips":[]}`)

	err := stray.provisioner.AuditUpdate(
		t.Context(),
		"fip-cluster",
		clusterupdate.NewEmptyUpdateResult(),
	)

	require.NoError(t, err)
	assert.Zero(t, stray.labelListings.Load())
	assert.NotContains(t, stray.output.String(), "stray floating IP")
}
