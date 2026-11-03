package environment

import (
	"fmt"
	"slices"
)

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
	// Root identity fields must not broadcast into unrelated annotations or groups.
	rootRewrites := slices.DeleteFunc(slices.Clone(rewrites), func(rewrite Rewrite) bool {
		return rewrite.Kind == MetaFieldValue &&
			(rewrite.Field == nameKey || rewrite.Field == providerKey)
	})

	return prepareCloneEnvironmentConfig(repoRoot, srcConfigRel, dstConfigRel,
		rootRewrites, target.Identity, &target)
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
