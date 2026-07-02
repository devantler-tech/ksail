//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package hetzner

import (
	"context"
	"io"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// FirewallRulesMatchForTest exports firewallRulesMatch for testing.
var FirewallRulesMatchForTest = firewallRulesMatch

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

// DeleteFloatingIPForTest exports deleteFloatingIP for testing.
func (p *Provider) DeleteFloatingIPForTest(ctx context.Context, clusterName string) error {
	return p.deleteFloatingIP(ctx, clusterName)
}

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
	}
}
