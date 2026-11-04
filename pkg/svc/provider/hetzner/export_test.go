//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package hetzner

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// FirewallRulesMatchForTest exports firewallRulesMatch for testing.
var FirewallRulesMatchForTest = firewallRulesMatch

// WaitForServersStatusForTest exposes the real server-status poll for regression tests.
func (p *Provider) WaitForServersStatusForTest(
	ctx context.Context,
	clusterName string,
	status hcloud.ServerStatus,
) error {
	return p.waitForServersStatus(ctx, clusterName, status)
}

// SourceIPsEqualForTest exports sourceIPsEqual for testing.
var SourceIPsEqualForTest = sourceIPsEqual

// BuildFirewallRulesForTest exports buildFirewallRules for testing.
var BuildFirewallRulesForTest = buildFirewallRules

// BuildServerCreateOptsForTest exports buildServerCreateOpts for testing.
func BuildServerCreateOptsForTest(opts CreateServerOpts) (hcloud.ServerCreateOpts, error) {
	p := &Provider{}

	return p.buildServerCreateOpts(opts)
}

// ShouldRetryErrorForTest exports shouldRetryError for testing.
var ShouldRetryErrorForTest = shouldRetryError

// ShouldDisablePlacementForTest exports shouldDisablePlacement for testing.
var ShouldDisablePlacementForTest = shouldDisablePlacement

// CalculateRetryDelayForTest exposes calculateRetryDelay for testing via a nil-client provider.
// Only the attempt number matters; the client field is unused by this method.
func CalculateRetryDelayForTest(attempt int) time.Duration {
	p := &Provider{}

	return p.calculateRetryDelay(attempt)
}

// NormalizeNodeRoleForTest exports normalizeNodeRole for testing.
var NormalizeNodeRoleForTest = normalizeNodeRole

// LBInNetworkForTest exports lbInNetwork for testing.
var LBInNetworkForTest = lbInNetwork

// ServerInNetworkForTest exports serverInNetwork for testing.
var ServerInNetworkForTest = serverInNetwork

// AvailableLocationsForTest exports availableLocations for testing.
var AvailableLocationsForTest = availableLocations

// DeduplicateServerTypesForTest exports deduplicateServerTypes for testing.
var DeduplicateServerTypesForTest = deduplicateServerTypes

// BuildLocationListForTest exports buildLocationList for testing.
var BuildLocationListForTest = buildLocationList

// IsRetryableAvailabilityErrorForTest exports isRetryableAvailabilityError for testing.
var IsRetryableAvailabilityErrorForTest = isRetryableAvailabilityError

// CheckServerAvailabilityWithRetryForTest exposes the internal availability retry
// loop with an injectable delay function so tests avoid real backoff sleeps.
func (p *Provider) CheckServerAvailabilityWithRetryForTest(
	ctx context.Context,
	serverTypes []string,
	primaryLocation string,
	fallbackLocations []string,
	maxAttempts int,
	logWriter io.Writer,
	delayFunc func(int) time.Duration,
) error {
	return p.checkServerAvailabilityWithRetry(
		ctx,
		serverTypes,
		primaryLocation,
		fallbackLocations,
		maxAttempts,
		logWriter,
		delayFunc,
	)
}

// DeleteLoadBalancerWithRetryForTest exports deleteLoadBalancerWithRetry for testing.
func (p *Provider) DeleteLoadBalancerWithRetryForTest(
	ctx context.Context,
	loadBalancer *hcloud.LoadBalancer,
) error {
	return p.deleteLoadBalancerWithRetry(ctx, loadBalancer)
}

// DeleteFloatingIPForTest exports deleteFloatingIP for testing.
func (p *Provider) DeleteFloatingIPForTest(ctx context.Context, clusterName string) error {
	return p.deleteFloatingIP(ctx, clusterName)
}

// testSnapshotBuildSettle is the settle bound test managers use. It is long enough for an
// uploader that returns shortly after cancellation, and short enough that a test whose
// uploader never returns does not wait out the production bound.
const testSnapshotBuildSettle = 2 * time.Second

// NewSnapshotManagerWithUploaderForTest creates a SnapshotManager with a custom uploader,
// allowing tests to inject a mock without hitting real Hetzner upload infrastructure.
// A nil logWriter is replaced with io.Discard, matching NewSnapshotManager behavior.
func NewSnapshotManagerWithUploaderForTest(
	hcloudClient *hcloud.Client,
	uploader snapshotUploader,
	logWriter io.Writer,
) *SnapshotManager {
	if logWriter == nil {
		logWriter = io.Discard
	}

	return &SnapshotManager{
		hcloudClient: hcloudClient,
		uploader:     uploader,
		logWriter:    logWriter,
		settle:       testSnapshotBuildSettle,
		retryDelay:   testSnapshotDownloadRetryDelay,
		probeImage:   imageServed,
	}
}

// testSnapshotDownloadRetryDelay keeps the backoff between snapshot build attempts short in tests.
const testSnapshotDownloadRetryDelay = time.Millisecond

// imageServed is the image probe test managers use by default: it answers as the Image Factory
// does for an image it serves, without a network request.
func imageServed(context.Context, *url.URL) (int, error) {
	return http.StatusOK, nil
}

// SetImageProbeForTest replaces the request that checks whether the Image Factory serves an
// image after a failed download.
func (sm *SnapshotManager) SetImageProbeForTest(
	probe func(ctx context.Context, imageURL *url.URL) (int, error),
) {
	sm.probeImage = probe
}

// BuildSSHFirewallRulesForTest exports buildSSHFirewallRules for testing.
var BuildSSHFirewallRulesForTest = buildSSHFirewallRules

// HeadImageURLForTest exposes the real Image Factory probe for tests against a local server.
var HeadImageURLForTest = headImageURL

// ImageMissingForTest exposes imageMissing for testing.
var ImageMissingForTest = imageMissing
