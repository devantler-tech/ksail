package environment

import (
	"bytes"
	"errors"
	"fmt"

	yamlv3 "gopkg.in/yaml.v3"
)

// ErrInvalidConfig is returned when a cloned root config is not a YAML mapping, so its
// identity cannot be written into it.
var ErrInvalidConfig = errors.New("invalid environment config")

// identityIndent matches the two-space indentation KSail writes its configs with.
const identityIndent = 2

// Identity is the cluster identity a cloned root config must declare so the new
// environment targets its own cluster: the config's top-level metadata.name and its
// spec.cluster.connection.context. An empty field is not materialized.
type Identity struct {
	// Name is the cluster name written to metadata.name.
	Name string
	// Context is the kubeconfig context written to spec.cluster.connection.context.
	Context string
}

// MaterializeIdentity returns content with identity's fields written where the
// config leaves them unset. A field the config already declares is kept as it is,
// because the value-exact rewrites have already repointed it or it was set by hand.
//
// It exists for a base-synced source (see [Environment.IsBaseSynced]): the base
// ksail.yaml `project init --multi-cluster` scaffolds declares no name and no
// context, so the rewrites have nothing to repoint and a clone would resolve to the
// same default cluster as its source.
func MaterializeIdentity(content string, identity Identity) (string, error) {
	var doc yamlv3.Node

	err := yamlv3.Unmarshal([]byte(content), &doc)
	if err != nil {
		return "", fmt.Errorf("parsing config: %w", err)
	}

	if len(doc.Content) == 0 || doc.Content[0].Kind != yamlv3.MappingNode {
		return "", fmt.Errorf("%w: config is not a YAML mapping", ErrInvalidConfig)
	}

	root := doc.Content[0]
	changed := false

	if identity.Name != "" {
		changed = ensureScalar(root, []string{"metadata", "name"}, identity.Name) || changed
	}

	if identity.Context != "" {
		changed = ensureScalar(
			root, []string{"spec", "cluster", "connection", "context"}, identity.Context,
		) || changed
	}

	if !changed {
		return content, nil
	}

	var buf bytes.Buffer

	encoder := yamlv3.NewEncoder(&buf)
	encoder.SetIndent(identityIndent)

	err = encoder.Encode(&doc)
	if err != nil {
		return "", fmt.Errorf("encoding config: %w", err)
	}

	err = encoder.Close()
	if err != nil {
		return "", fmt.Errorf("encoding config: %w", err)
	}

	return buf.String(), nil
}

// ensureScalar sets the scalar at keyPath under mapping to value when it is absent
// or empty, creating intermediate mappings as needed. It reports whether it changed
// anything; a present non-empty value, or a path blocked by a non-mapping node, is
// left untouched.
func ensureScalar(mapping *yamlv3.Node, keyPath []string, value string) bool {
	node := mapping

	for i, key := range keyPath {
		child := mappingValue(node, key)
		last := i == len(keyPath)-1

		switch {
		case child == nil && last:
			insertPair(node, key, &yamlv3.Node{Kind: yamlv3.ScalarNode, Value: value})

			return true
		case child == nil:
			child = &yamlv3.Node{Kind: yamlv3.MappingNode}
			insertPair(node, key, child)
		case last:
			if child.Kind != yamlv3.ScalarNode || child.Value != "" {
				return false
			}

			child.Tag = ""
			child.Style = 0
			child.Value = value

			return true
		case child.Kind != yamlv3.MappingNode:
			return false
		}

		node = child
	}

	return false
}

// mappingValue returns the value node for key in mapping, or nil when absent.
func mappingValue(mapping *yamlv3.Node, key string) *yamlv3.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}

	return nil
}

// insertPair adds key: value to mapping. A new metadata block goes before spec, where
// a ksail config declares it; anything else is appended.
func insertPair(mapping *yamlv3.Node, key string, value *yamlv3.Node) {
	pair := []*yamlv3.Node{{Kind: yamlv3.ScalarNode, Value: key}, value}

	if key == "metadata" {
		for i := 0; i+1 < len(mapping.Content); i += 2 {
			if mapping.Content[i].Value == "spec" {
				rest := append(pair, mapping.Content[i:]...)
				mapping.Content = append(mapping.Content[:i:i], rest...)

				return
			}
		}
	}

	mapping.Content = append(mapping.Content, pair...)
}
