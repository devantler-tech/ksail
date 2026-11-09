package environment

import (
	"fmt"
)

// cloneTargetPlan separates root-only operations from overlay-wide rewrites.
// Context rewrites remain value-exact; unlike a name or provider override, their
// destination cannot be inferred without examining the selected root config.
type cloneTargetPlan struct {
	target          CloneTarget
	contextRewrites []Rewrite
}

// ConfigClone holds a validated root-config clone without writing it. Its source
// content is captured during preparation; Write rechecks destination containment
// and the normal no-overwrite policy when the caller is ready to commit the file.
type ConfigClone struct {
	repoRoot, relativePath, content string
	prepared                        bool
}

// PrepareEnvironmentConfigCloneForTarget validates and captures a root-config
// clone before any associated overlay files are changed. It applies exactly the
// same rewrites, identity, encrypted-copy and path guards as the immediate API.
func PrepareEnvironmentConfigCloneForTarget(
	repoRoot, srcConfigRel, dstConfigRel string,
	rewrites []Rewrite,
	target CloneTarget,
) (ConfigClone, error) {
	rootRewrites, plan, err := resolveCloneTarget(rewrites, target)
	if err != nil {
		return ConfigClone{}, err
	}

	return prepareCloneEnvironmentConfig(repoRoot, srcConfigRel, dstConfigRel,
		rootRewrites, target.Identity, &plan)
}

func resolveCloneTarget(
	rewrites []Rewrite,
	target CloneTarget,
) ([]Rewrite, cloneTargetPlan, error) {
	plan := cloneTargetPlan{target: target}

	var remaining []Rewrite

	// Validate before partitioning so even an overridden root rewrite must be valid.
	for _, rewrite := range rewrites {
		err := rewrite.validate()
		if err != nil {
			return nil, plan, err
		}
	}

	for _, rewrite := range rewrites {
		if rewrite.Kind != MetaFieldValue {
			remaining = append(remaining, rewrite)

			continue
		}

		switch rewrite.Field {
		case nameKey, providerKey:
			// Resolved together below to preserve ordered chains and explicit overrides.
		case contextKey:
			plan.contextRewrites = append(plan.contextRewrites, rewrite)
		default:
			remaining = append(remaining, rewrite)
		}
	}

	var err error

	plan.target.Identity.Name, err = cloneRewriteDestination(
		rewrites,
		nameKey,
		target.Identity.Name,
	)
	if err != nil {
		return nil, plan, err
	}

	plan.target.Provider, err = cloneRewriteDestination(rewrites, providerKey, target.Provider)

	return remaining, plan, err
}

func cloneRewriteDestination(rewrites []Rewrite, field, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	var destination string

	for _, rewrite := range rewrites {
		if rewrite.Kind != MetaFieldValue || rewrite.Field != field {
			continue
		}

		if destination != "" && rewrite.Old != destination && rewrite.New != destination {
			return "", fmt.Errorf("%w: conflicting clone %s destinations", ErrInvalidRewrite, field)
		}

		destination = rewrite.New
	}

	return destination, nil
}

// Write commits the captured config through the existing descriptor-rooted
// writer. It returns the relative path and whether it wrote rather than preserved
// an existing file. A zero ConfigClone cannot write anything.
func (clone ConfigClone) Write(force bool) (string, bool, error) {
	if !clone.prepared {
		return "", false, fmt.Errorf("%w: config clone was not prepared", ErrInvalidConfig)
	}

	wrote, err := writeClone(clone.repoRoot, clone.relativePath, clone.content, force)
	if err != nil {
		return "", false, err
	}

	return clone.relativePath, wrote, nil
}
