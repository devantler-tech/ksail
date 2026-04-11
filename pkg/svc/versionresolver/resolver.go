package versionresolver

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// Resolver discovers available versions from a container image registry.
type Resolver interface {
	// ListVersions returns all parseable version tags for the given image reference.
	// The imageRef should be a repository reference without a tag (e.g., "kindest/node").
	ListVersions(ctx context.Context, imageRef string) ([]Version, error)
}

// OCIResolver lists tags from OCI-compliant registries using the Distribution API.
type OCIResolver struct {
	// remoteOptions are passed to all remote calls (auth, transport, etc.)
	remoteOptions []remote.Option
}

// NewOCIResolver creates a new OCIResolver with optional remote options.
func NewOCIResolver(opts ...remote.Option) *OCIResolver {
	return &OCIResolver{remoteOptions: opts}
}

// ListVersions queries the OCI registry for all tags of the given repository
// and parses them into Version structs. Unparseable tags are silently skipped.
func (r *OCIResolver) ListVersions(ctx context.Context, imageRef string) ([]Version, error) {
	repo, err := name.NewRepository(imageRef)
	if err != nil {
		return nil, fmt.Errorf("%w: parsing repository %q: %w", ErrRegistryAccess, imageRef, err)
	}

	opts := append([]remote.Option{remote.WithContext(ctx)}, r.remoteOptions...)

	tags, err := remote.List(repo, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: listing tags for %q: %w", ErrRegistryAccess, imageRef, err)
	}

	return ParseTags(tags), nil
}

// UpgradeStep represents a single step in an upgrade path.
type UpgradeStep struct {
	Version  Version
	ImageRef string
}

// ComputeUpgradePath discovers available versions from the registry, filters to
// stable versions newer than currentTag, and returns an ordered upgrade path.
// The suffix parameter filters versions by distribution-specific suffix (e.g., "k3s1").
// Pass empty string to match only tags without a suffix.
func ComputeUpgradePath(
	ctx context.Context,
	resolver Resolver,
	imageRef string,
	currentTag string,
	suffix string,
) ([]UpgradeStep, error) {
	current, err := ParseVersion(currentTag)
	if err != nil {
		return nil, fmt.Errorf("parsing current version: %w", err)
	}

	allVersions, err := resolver.ListVersions(ctx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("listing versions for %s: %w", imageRef, err)
	}

	stable := FilterStable(allVersions)
	if len(stable) == 0 {
		return nil, fmt.Errorf("%w: repository %s", ErrNoVersionsFound, imageRef)
	}

	// If a suffix is specified (e.g., K3s tags like "v1.35.3-k3s1"),
	// filter to matching suffix before computing upgrade path.
	if suffix != "" {
		stable = MatchingSuffix(stable, suffix)
		if len(stable) == 0 {
			return nil, fmt.Errorf("%w: no versions with suffix %q in %s",
				ErrNoVersionsFound, suffix, imageRef)
		}
	} else {
		// When no suffix, only include tags without any suffix
		stable = MatchingSuffix(stable, "")
	}

	newer := NewerThan(stable, current)
	if len(newer) == 0 {
		return nil, fmt.Errorf(
			"%w: already at latest stable version %s", ErrNoUpgradesAvailable, currentTag,
		)
	}

	steps := make([]UpgradeStep, 0, len(newer))
	for _, v := range newer {
		steps = append(steps, UpgradeStep{
			Version:  v,
			ImageRef: imageRef + ":" + v.Original,
		})
	}

	return steps, nil
}
