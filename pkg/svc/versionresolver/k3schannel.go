package versionresolver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	// K3sImageRef is the repository k3d pulls its node image from.
	K3sImageRef = "rancher/k3s"

	// K3sChannelsURL is k3s' own release-channel index — the authority the k3s
	// installer itself consults. A release appears here only once k3s promotes
	// it, which is what makes it usable as a promotion signal; the registry tag
	// appears earlier.
	K3sChannelsURL = "https://update.k3s.io/v1-release/channels"

	// k3sChannelTimeout bounds the channel lookup so a hung endpoint cannot
	// stall a cluster update indefinitely.
	k3sChannelTimeout = 15 * time.Second
)

// K3sChannelSource reports the k3s releases that have been promoted to a
// release channel.
//
// The endpoint is fixed: there is no legitimate reason to ask a different host
// which k3s releases exist, so the URL is a constant rather than a field. Tests
// redirect it by supplying a client whose transport points elsewhere.
type K3sChannelSource struct {
	client *http.Client
}

// NewK3sChannelSource returns a source backed by k3s' public channel index.
func NewK3sChannelSource() *K3sChannelSource {
	return &K3sChannelSource{client: &http.Client{Timeout: k3sChannelTimeout}}
}

// NewK3sChannelSourceWithClient injects the HTTP client, so a caller (or a
// test) can supply its own transport, timeout, or proxy.
func NewK3sChannelSourceWithClient(client *http.Client) *K3sChannelSource {
	if client == nil {
		client = &http.Client{Timeout: k3sChannelTimeout}
	}

	return &K3sChannelSource{client: client}
}

// k3sChannelsResponse is the subset of the channel index this package reads.
type k3sChannelsResponse struct {
	Data []struct {
		Name   string `json:"name"`
		Latest string `json:"latest"`
	} `json:"data"`
}

// PromotedTags returns the registry tags k3s has promoted, taken from the
// "latest" pointer of every channel it publishes that names a final release.
// Each channel names the newest promoted release for its stream, so the union
// across channels is the set of releases k3s considers out.
//
// Pre-releases are excluded. k3s publishes "testing" channels alongside the
// stable ones, and their latest is a release candidate rather than a release, so
// an unfiltered union would offer a cluster an upgrade onto an rc build — the
// same class of unreleased-version upgrade this source exists to prevent. The
// test is the version's own semver pre-release segment rather than the channel
// name, so a differently-named pre-release channel is caught too.
func (s *K3sChannelSource) PromotedTags(ctx context.Context) (map[string]struct{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, K3sChannelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building k3s channel request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying k3s release channels: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"%w: k3s release channels returned %s", ErrRegistryAccess, resp.Status,
		)
	}

	var parsed k3sChannelsResponse

	err = json.NewDecoder(resp.Body).Decode(&parsed)
	if err != nil {
		return nil, fmt.Errorf("decoding k3s release channels: %w", err)
	}

	tags := make(map[string]struct{}, len(parsed.Data))

	for _, channel := range parsed.Data {
		if channel.Latest == "" || k3sVersionIsPreRelease(channel.Latest) {
			continue
		}

		tags[K3sChannelVersionToTag(channel.Latest)] = struct{}{}
	}

	if len(tags) == 0 {
		return nil, fmt.Errorf(
			"%w: k3s release channels listed no promoted versions", ErrNoVersionsFound,
		)
	}

	return tags, nil
}

// k3sVersionIsPreRelease reports whether a channel version names a pre-release
// ("v1.18.2-rc3+k3s1") rather than a final release ("v1.36.2+k3s1").
//
// In semver the pre-release segment sits between the version core and the "+"
// build metadata, and k3s puts its "k3s1" suffix in that build metadata. The
// version core is digits and dots only, so a "-" anywhere before the "+" is a
// pre-release marker and nothing else.
func k3sVersionIsPreRelease(version string) bool {
	core, _, _ := strings.Cut(version, "+")

	return strings.Contains(core, "-")
}

// K3sChannelVersionToTag converts a k3s channel version to its registry tag.
// The channel index uses semver build metadata ("v1.36.2+k3s1") while the
// registry tag replaces "+" with "-" ("v1.36.2-k3s1"), because "+" is not a
// legal character in an OCI tag.
func K3sChannelVersionToTag(version string) string {
	return strings.ReplaceAll(version, "+", "-")
}
