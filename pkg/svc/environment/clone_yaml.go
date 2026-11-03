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

func validateCloneFields(root *yamlv3.Node, fields []cloneTargetField) error {
	for _, field := range fields {
		if field.value == "" {
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
			if (!allowMergedLeaf || index < len(keyPath)-1) && mappingValue(node, "<<") != nil {
				return fmt.Errorf(
					"%w: clone identity would shadow a merged parent mapping",
					ErrInvalidConfig,
				)
			}

			return nil
		}

		node = child
	}

	return validateCloneNode(root, node)
}

func validateCloneNode(root, node *yamlv3.Node) error {
	if node.Kind == yamlv3.AliasNode || (node.Anchor != "" && referencesAlias(root, node)) {
		return fmt.Errorf(
			"%w: clone identity uses a shared YAML alias; make the target path explicit",
			ErrInvalidConfig,
		)
	}

	return nil
}

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
