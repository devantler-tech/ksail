package talosprovisioner_test

import (
	"context"
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// floatingIPDisableFixture is a running Hetzner cluster whose nodes still carry
// the floating-IP endpoint and HCloud VIP while ksail.yaml now sets
// `floatingIPEnabled: false` — the state the disable transition starts from.
type floatingIPDisableFixture struct {
	provisioner     *talosprovisioner.Provisioner
	calls           *fipUpdateCalls
	running         talosconfig.Provider
	diff            *clusterupdate.UpdateResult
	kubeconfigPath  string
	talosconfigPath string
}

// newFloatingIPDisableFixture renders the running control-plane config with
// the floating IP enabled, then builds the disabled provisioner that updates
// it and detects the disable change through the real detector.
func newFloatingIPDisableFixture(t *testing.T) *floatingIPDisableFixture {
	t.Helper()

	t.Setenv(testFloatingIPTokenEnvVar, "vip-test-token")

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, true, calls)
	hzProvider := newFipUpdateProvider(server.URL)

	enabled := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
		TokenEnvVar:        testFloatingIPTokenEnvVar,
	}).WithInfraProvider(hzProvider)
	require.NoError(t, enabled.UpdateConfigsWithEndpointForTest(
		t.Context(), hzProvider, "fip-cluster",
		[]*hcloud.Server{controlPlaneServer(11, "fip-cluster-cp-0", "203.0.113.5")},
	))

	running := enabled.TalosConfigsForTest().ControlPlane()
	require.True(t, hasHCloudVIP(running), "fixture must start from a VIP-carrying node")

	kubeconfigPath := t.TempDir() + "/kubeconfig"
	talosconfigPath := writeFloatingIPTalosconfig(t)
	provisioner := newFloatingIPTestProvisionerWithOptions(t, v1alpha1.OptionsHetzner{
		FloatingIPLocation: "fsn1",
		TokenEnvVar:        testFloatingIPTokenEnvVar,
	}, talosprovisioner.NewOptions().
		WithKubeconfigPath(kubeconfigPath).
		WithTalosconfigPath(talosconfigPath)).
		WithInfraProvider(hzProvider).
		WithNodeConfigFetcherForTest(
			func(context.Context, string) (talosconfig.Provider, error) {
				return running, nil
			},
		)
	provisioner.WithTalosClientFactoryForTest(
		func(context.Context, string) (talosprovisioner.KubeconfigFetcherForTest, error) {
			return &mockKubeconfigFetcher{kubeconfig: minimalKubeconfigBytes()}, nil
		},
	)

	diff := clusterupdate.NewEmptyUpdateResult()
	require.NoError(t,
		provisioner.MergeFloatingIPChangesForTest(t.Context(), "fip-cluster", diff))
	require.Len(t, diff.InPlaceChanges, 1, "the disable transition must be detected")

	return &floatingIPDisableFixture{
		provisioner:     provisioner,
		calls:           calls,
		running:         running,
		diff:            diff,
		kubeconfigPath:  kubeconfigPath,
		talosconfigPath: talosconfigPath,
	}
}

// runStep runs one named update apply step against the fixture.
func (f *floatingIPDisableFixture) runStep(
	t *testing.T,
	name string,
	result *clusterupdate.UpdateResult,
) {
	t.Helper()

	spec := &v1alpha1.ClusterSpec{ControlPlanes: 1}
	require.NoError(t, f.provisioner.RunUpdateApplyStepForTest(
		t.Context(), name, "fip-cluster", spec, spec, f.diff, result,
	))
}

// writeFloatingIPTalosconfig saves a talosconfig whose cluster context dials
// the floating IP, next to an unrelated context that lists the same address
// and must be left alone.
func writeFloatingIPTalosconfig(t *testing.T) string {
	t.Helper()

	path := t.TempDir() + "/talosconfig"
	config := &clientconfig.Config{
		Context: "fip-cluster",
		Contexts: map[string]*clientconfig.Context{
			"fip-cluster": {
				Endpoints: []string{"192.0.2.10", "203.0.113.5"},
				Nodes:     []string{"203.0.113.5"},
			},
			"other-cluster": {
				Endpoints: []string{"192.0.2.10"},
			},
		},
	}
	require.NoError(t, config.Save(path))

	return path
}

// hasHCloudVIP reports whether any network device in config carries an HCloud
// VIP declaration.
func hasHCloudVIP(config talosconfig.Provider) bool {
	for _, device := range config.Machine().Network().Devices() {
		if vip := device.VIPConfig(); vip != nil && vip.HCloud() != nil {
			return true
		}
	}

	return false
}

// TestUpdateApplySteps_FloatingIPDisableRevertsNodesThenReleases walks the
// disable transition through the real update steps (#6032): the pushed node
// config drops the VIP and returns to the direct control-plane endpoint instead
// of grafting the running floating-IP state back, the kubeconfig moves off the
// address, and only then is the ksail-owned floating IP released.
func TestUpdateApplySteps_FloatingIPDisableRevertsNodesThenReleases(t *testing.T) {
	fixture := newFloatingIPDisableFixture(t)
	result := clusterupdate.NewEmptyUpdateResult()

	fixture.runStep(t, "reconcile floating IP endpoint", result)

	desired, err := fixture.provisioner.BuildDesiredNodeConfigForTest(
		fixture.running, fixture.running, talosprovisioner.RoleControlPlane,
	)
	require.NoError(t, err)
	assert.Equal(t, "203.0.113.5", desired.Cluster().Endpoint().Hostname(),
		"the pushed config must use the direct control-plane endpoint")
	assert.False(t, hasHCloudVIP(desired),
		"the pushed config must not graft the running HCloud VIP back")
	assert.Equal(t, int32(0), fixture.calls.del.Load(),
		"nothing may be released before nodes have left the address")

	fixture.runStep(t, "refresh floating IP kubeconfig", result)

	written, err := os.ReadFile(fixture.kubeconfigPath)
	require.NoError(t, err)
	assert.Contains(t, string(written), "https://203.0.113.5:6443")
	assert.NotContains(t, string(written), "192.0.2.10")

	fixture.runStep(t, "release disabled floating IP", result)

	assert.Equal(t, int32(1), fixture.calls.del.Load(),
		"the ksail-owned floating IP must be released once clients have moved")

	saved, err := clientconfig.Open(fixture.talosconfigPath)
	require.NoError(t, err)
	assert.Equal(t, []string{"203.0.113.5"}, saved.Contexts["fip-cluster"].Endpoints,
		"talosctl must stop dialing the released address")
	assert.Equal(t, []string{"192.0.2.10"}, saved.Contexts["other-cluster"].Endpoints,
		"another cluster's context must not be rewritten")
}

// TestUpdateApplySteps_FloatingIPDisableKeepsAddressAfterFailedChanges proves
// the release is withheld when any change failed: a release cannot be undone,
// and keeping the address lets the next update retry the whole transition.
func TestUpdateApplySteps_FloatingIPDisableKeepsAddressAfterFailedChanges(t *testing.T) {
	fixture := newFloatingIPDisableFixture(t)
	result := clusterupdate.NewEmptyUpdateResult()
	result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
		Field: talosprovisioner.MachineConfigField,
	})

	fixture.runStep(t, "release disabled floating IP", result)

	assert.Equal(t, int32(0), fixture.calls.del.Load())
}

// TestUpdateApplySteps_FloatingIPReleaseSkipsWhenEnabled proves the release
// step never touches an address the configuration still wants.
func TestUpdateApplySteps_FloatingIPReleaseSkipsWhenEnabled(t *testing.T) {
	t.Parallel()

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, true, calls)
	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
	}).WithInfraProvider(newFipUpdateProvider(server.URL))

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field:    floatingIPEnabledField,
		Category: clusterupdate.ChangeCategoryInPlace,
	})
	spec := &v1alpha1.ClusterSpec{ControlPlanes: 1}

	require.NoError(t, provisioner.RunUpdateApplyStepForTest(
		t.Context(), "release disabled floating IP", "fip-cluster",
		spec, spec, diff, clusterupdate.NewEmptyUpdateResult(),
	))

	assert.Equal(t, int32(0), calls.del.Load())
}
