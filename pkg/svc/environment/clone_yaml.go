package environment

import (
	"fmt"

	yamlv3 "gopkg.in/yaml.v3"
)

type cloneTargetField struct {
	path            []string
	value           string
	allowMergedLeaf bool
}

// validateCloneFields checks only paths whose requested values would change.
func validateCloneFields(root *yamlv3.Node, fields []cloneTargetField) error {
	for _, field := range fields {
		if field.value == "" || cloneFieldMatches(root, field.path, field.value) {
			continue
		}

		err := validateClonePath(root, field.path, field.allowMergedLeaf)
		if err != nil {
			return err
		}
	}

	return nil
}

// validateClonePath refuses changes that could mutate an alias user or hide an
// inherited parent mapping. It runs before any fill-only identity insertion.
func validateClonePath(root *yamlv3.Node, keyPath []string, allowMergedLeaf bool) error {
	node := root
	for index, key := range keyPath {
		err := validateCloneNode(root, node)
		if err != nil {
			return err
		}

		if isNull(node) {
			return nil
		}

		if node.Kind != yamlv3.MappingNode {
			return fmt.Errorf("%w: clone identity parent is not a mapping", ErrInvalidConfig)
		}

		child := mappingValue(node, key)
		if child == nil {
			return validateMissingCloneKey(node, key, allowMergedLeaf && index == len(keyPath)-1)
		}

		node = child
	}

	return validateCloneNode(root, node)
}

// validateMissingCloneKey distinguishes an absent field from an inherited value
// that an insertion would hide. Explicit replacement is allowed only at a leaf.
func validateMissingCloneKey(node *yamlv3.Node, key string, allowMergedLeaf bool) error {
	if allowMergedLeaf || mappingValue(node, "<<") == nil {
		return nil
	}

	var merged map[string]any

	err := node.Decode(&merged)
	if err != nil {
		return fmt.Errorf("%w: reading merged clone mapping: %w", ErrInvalidConfig, err)
	}

	if _, inherited := merged[key]; inherited {
		return fmt.Errorf(
			"%w: clone identity would shadow a merged parent mapping",
			ErrInvalidConfig,
		)
	}

	return nil
}

// cloneFieldMatches permits a shared value only when no mutation is needed.
// Aliases are read without altering their anchor or other users.
func cloneFieldMatches(root *yamlv3.Node, keyPath []string, value string) bool {
	node := root
	for _, key := range keyPath {
		if node.Kind == yamlv3.AliasNode {
			node = node.Alias
		}

		if node == nil || node.Kind != yamlv3.MappingNode {
			return false
		}

		node = mappingValue(node, key)
		if node == nil {
			return false
		}
	}

	if node.Kind == yamlv3.AliasNode {
		node = node.Alias
	}

	return node != nil && node.Kind == yamlv3.ScalarNode && node.ShortTag() == yamlStringTag &&
		node.Value == value
}

// validateCloneNode rejects writes through aliases or into a shared anchor.
func validateCloneNode(root, node *yamlv3.Node) error {
	if node.Kind == yamlv3.AliasNode || (node.Anchor != "" && referencesAlias(root, node)) {
		return fmt.Errorf(
			"%w: clone identity uses a shared YAML alias; make the target path explicit",
			ErrInvalidConfig,
		)
	}

	return nil
}

// referencesAlias checks alias edges without traversing them recursively.
func referencesAlias(root, target *yamlv3.Node) bool {
	if root.Kind == yamlv3.AliasNode && root.Alias == target {
		return true
	}

	for _, child := range root.Content {
		if referencesAlias(child, target) {
			return true
		}
	}

	return false
}
