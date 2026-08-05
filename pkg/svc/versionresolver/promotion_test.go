package versionresolver_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/versionresolver"
)

// errPromotion is returned by the failing stub below.
var errPromotion = errors.New("channel unavailable")

// stubPromotionSource implements PromotionSource without network access.
type stubPromotionSource struct {
	tags map[string]struct{}
	err  error
}

func (s *stubPromotionSource) PromotedTags(_ context.Context) (map[string]struct{}, error) {
	if s.err != nil {
		return nil, s.err
	}

	return s.tags, nil
}

// rewriteTransport redirects every request to target, so a test can exercise
// the fixed channel URL against a local server.
type rewriteTransport struct {
	target *url.URL
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.target.Scheme
	clone.URL.Host = t.target.Host

	//nolint:wrapcheck // test transport: the caller asserts on the original error
	return http.DefaultTransport.RoundTrip(clone)
}

// channelClientFor returns a client whose requests land on server.
func channelClientFor(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parsing test server URL: %v", err)
	}

	return &http.Client{Transport: &rewriteTransport{target: parsed}}
}

func promotedSet(tags ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		set[t] = struct{}{}
	}

	return set
}

// TestComputeUpgradePath_SkipsUnpromotedK3sRelease reproduces the live defect:
// k3s pushed rancher/k3s:v1.36.3-k3s1 to the registry on 2026-08-04 but had not
// promoted it to any release channel, so a cluster already at v1.36.2-k3s1 must
// see no upgrade. Before promotion awareness this returned a one-step path and
// recreated the cluster.
func TestComputeUpgradePath_SkipsUnpromotedK3sRelease(t *testing.T) {
	t.Parallel()

	base := &mockResolver{
		versions: parseTags([]string{"v1.36.1-k3s1", "v1.36.2-k3s1", "v1.36.3-k3s1"}),
	}
	resolver := versionresolver.NewPromotionAwareResolverWithSources(
		base,
		map[string]versionresolver.PromotionSource{
			versionresolver.K3sImageRef: &stubPromotionSource{
				tags: promotedSet("v1.36.1-k3s1", "v1.36.2-k3s1"),
			},
		},
	)

	_, err := versionresolver.ComputeUpgradePath(
		context.Background(), resolver, versionresolver.K3sImageRef, "v1.36.2-k3s1", "k3s1",
	)
	if !errors.Is(err, versionresolver.ErrNoUpgradesAvailable) {
		t.Fatalf("expected ErrNoUpgradesAvailable for an unpromoted release, got %v", err)
	}
}

// TestComputeUpgradePath_UsesPromotedK3sRelease is the positive control: once
// k3s promotes the release, the same inputs must yield the upgrade.
func TestComputeUpgradePath_UsesPromotedK3sRelease(t *testing.T) {
	t.Parallel()

	base := &mockResolver{
		versions: parseTags([]string{"v1.36.1-k3s1", "v1.36.2-k3s1", "v1.36.3-k3s1"}),
	}
	resolver := versionresolver.NewPromotionAwareResolverWithSources(
		base,
		map[string]versionresolver.PromotionSource{
			versionresolver.K3sImageRef: &stubPromotionSource{
				tags: promotedSet("v1.36.1-k3s1", "v1.36.2-k3s1", "v1.36.3-k3s1"),
			},
		},
	)

	steps, err := versionresolver.ComputeUpgradePath(
		context.Background(), resolver, versionresolver.K3sImageRef, "v1.36.2-k3s1", "k3s1",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(steps) != 1 || steps[0].Version.Original != "v1.36.3-k3s1" {
		t.Fatalf("expected a single step to v1.36.3-k3s1, got %+v", steps)
	}
}

// TestPromotionAwareResolver_PassesThroughUnknownImage pins that distributions
// without a registered channel keep their previous behaviour.
func TestPromotionAwareResolver_PassesThroughUnknownImage(t *testing.T) {
	t.Parallel()

	base := &mockResolver{versions: parseTags([]string{"v1.35.0", "v1.36.0"})}
	resolver := versionresolver.NewPromotionAwareResolverWithSources(
		base, map[string]versionresolver.PromotionSource{},
	)

	versions, err := resolver.ListVersions(context.Background(), "kindest/node")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(versions) != 2 {
		t.Fatalf("expected the base versions to pass through, got %d", len(versions))
	}
}

// TestPromotionAwareResolver_FailsClosed pins that a broken channel source
// surfaces an error rather than silently falling back to raw registry tags —
// falling back would reinstate the defect exactly when the check is needed.
func TestPromotionAwareResolver_FailsClosed(t *testing.T) {
	t.Parallel()

	base := &mockResolver{versions: parseTags([]string{"v1.36.2-k3s1", "v1.36.3-k3s1"})}
	resolver := versionresolver.NewPromotionAwareResolverWithSources(
		base,
		map[string]versionresolver.PromotionSource{
			versionresolver.K3sImageRef: &stubPromotionSource{err: errPromotion},
		},
	)

	_, err := resolver.ListVersions(context.Background(), versionresolver.K3sImageRef)
	if !errors.Is(err, errPromotion) {
		t.Fatalf("expected the source error to surface, got %v", err)
	}
}

// TestK3sChannelSource_PromotedTags pins the channel-index parse, including the
// "+" → "-" conversion between the channel version and the registry tag.
func TestK3sChannelSource_PromotedTags(t *testing.T) {
	t.Parallel()

	body := `{"data":[
		{"name":"stable","latest":"v1.36.2+k3s1"},
		{"name":"latest","latest":"v1.36.2+k3s1"},
		{"name":"v1.35","latest":"v1.35.6+k3s1"},
		{"name":"v1.16-testing","latest":""}
	]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	source := versionresolver.NewK3sChannelSourceWithClient(channelClientFor(t, server))

	tags, err := source.PromotedTags(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tags) != 2 {
		t.Fatalf("expected 2 distinct promoted tags, got %d (%v)", len(tags), tags)
	}

	for _, want := range []string{"v1.36.2-k3s1", "v1.35.6-k3s1"} {
		if _, ok := tags[want]; !ok {
			t.Errorf("expected %q among promoted tags, got %v", want, tags)
		}
	}
}

// TestK3sChannelSource_EmptyIsAnError pins that an index listing nothing is
// reported rather than read as "no upgrades available".
func TestK3sChannelSource_EmptyIsAnError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	source := versionresolver.NewK3sChannelSourceWithClient(channelClientFor(t, server))

	_, err := source.PromotedTags(context.Background())
	if err == nil {
		t.Fatal("expected an error for an empty channel index")
	}
}
