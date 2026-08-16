package versionresolver

import (
	"context"
	"fmt"
)

// PromotionSource reports the registry tags a distribution has promoted to a
// release channel.
//
// An OCI registry serves an image the moment its tag is pushed. For some
// distributions that happens well before — or entirely without — the release
// being promoted for general use, so the tag list alone cannot answer "is this
// release out yet". k3s is the standing example: it pushes rancher/k3s:<tag> at
// tag time and moves its stable channel only after the release is promoted.
type PromotionSource interface {
	// PromotedTags returns the registry tags (e.g. "v1.36.2-k3s1") the
	// distribution has promoted. An empty result is an error, not an empty set:
	// silently returning nothing would read as "no upgrades available" and hide
	// a broken source.
	PromotedTags(ctx context.Context) (map[string]struct{}, error)
}

// promotionAwareResolver constrains a base Resolver to the versions the
// distribution publishing the image has actually promoted.
type promotionAwareResolver struct {
	base    Resolver
	sources map[string]PromotionSource
}

// NewPromotionAwareResolver wraps base so that images from a distribution with
// a known release channel are filtered to promoted versions only. Images with
// no registered source pass through unchanged, so distributions whose registry
// tags already mean "released" keep their existing behaviour.
func NewPromotionAwareResolver(base Resolver) Resolver {
	return &promotionAwareResolver{base: base, sources: defaultPromotionSources()}
}

// NewPromotionAwareResolverWithSources injects the promotion sources instead of
// using the defaults, so a caller (or a test) can resolve without reaching the
// network.
func NewPromotionAwareResolverWithSources(
	base Resolver, sources map[string]PromotionSource,
) Resolver {
	return &promotionAwareResolver{base: base, sources: sources}
}

// defaultPromotionSources maps an image reference to the authority on whether a
// given tag of it has been promoted.
func defaultPromotionSources() map[string]PromotionSource {
	return map[string]PromotionSource{
		K3sImageRef: NewK3sChannelSource(),
	}
}

// ListVersions returns the base resolver's versions, minus any the
// distribution has not promoted.
func (r *promotionAwareResolver) ListVersions(
	ctx context.Context, imageRef string,
) ([]Version, error) {
	versions, err := r.base.ListVersions(ctx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("listing versions for %s: %w", imageRef, err)
	}

	source, ok := r.sources[imageRef]
	if !ok {
		return versions, nil
	}

	promoted, err := source.PromotedTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving promoted versions for %s: %w", imageRef, err)
	}

	return FilterPromoted(versions, promoted), nil
}

// FilterPromoted returns only the versions whose original tag appears in
// promoted.
func FilterPromoted(versions []Version, promoted map[string]struct{}) []Version {
	result := make([]Version, 0, len(versions))

	for _, v := range versions {
		if _, ok := promoted[v.Original]; ok {
			result = append(result, v)
		}
	}

	return result
}
