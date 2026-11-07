package talosprovisioner_test

import (
	"bytes"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	hetzner "github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
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
func runNoChangeFloatingIPStep(
	t *testing.T,
	floatingIPEnabled bool,
	labelStatus int,
	labelBody string,
) (string, int32, int32) {
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

	var output bytes.Buffer

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  floatingIPEnabled,
		FloatingIPLocation: "fsn1",
		TokenEnvVar:        testFloatingIPTokenEnvVar,
	}).WithInfraProvider(hetzner.NewProvider(client)).WithLogWriter(&output)

	spec := &v1alpha1.ClusterSpec{ControlPlanes: 1, Workers: 1}

	require.NoError(t, provisioner.RunUpdateApplyStepForTest(
		t.Context(), "reconcile floating IP endpoint", "fip-cluster",
		spec, spec, clusterupdate.NewEmptyUpdateResult(), clusterupdate.NewEmptyUpdateResult(),
	), "the stray check must never fail the update")

	return output.String(), assignCalls.Load(), labelListings.Load()
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
