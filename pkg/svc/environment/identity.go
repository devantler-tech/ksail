package environment

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	yamlv3 "gopkg.in/yaml.v3"
)

// ErrInvalidConfig is returned when a cloned root config is not a YAML mapping, so its
// identity cannot be written into it.
var ErrInvalidConfig = errors.New("invalid environment config")

// identityIndent matches the two-space indentation KSail writes its configs with.
const (
	identityIndent = 2
	metadataKey    = "metadata"
	nameKey        = "name"
	specKey        = "spec"
	providerKey    = "provider"
	clusterKey     = "cluster"
	contextKey     = "context"
	connectionKey  = "connection"
	yamlStringTag  = "!!str"
)

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
	doc, err := parseIdentityConfig(content)
	if err != nil {
		return "", err
	}

	if !applyIdentity(doc.Content[0], identity) {
		return content, nil
	}

	return encodeConfig(doc)
}

// applyIdentity writes identity's non-empty fields into root where they are unset and
// reports whether it changed anything.
func applyIdentity(root *yamlv3.Node, identity Identity) bool {
	changed := false

	if identity.Name != "" {
		changed = ensureScalar(root, []string{metadataKey, nameKey}, identity.Name) || changed
	}

	if identity.Context != "" {
		changed = ensureScalar(
			root, []string{specKey, clusterKey, connectionKey, contextKey}, identity.Context,
		) || changed
	}

	return changed
}

// A root config's cluster name is its source identity even when it differs from
// the environment label. Provider overrides also need an insertion when default
// omission left no scalar for the value-exact rewrites to match.
func materializeCloneTarget(content string, plan cloneTargetPlan) (string, error) {
	target := plan.target
	if target.Identity.Name == "" {
		return "", fmt.Errorf("%w: clone target requires a destination name", ErrInvalidConfig)
	}

	doc, err := parseIdentityConfig(content)
	if err != nil {
		return "", err
	}

	context, err := cloneContextDestination(doc.Content[0], plan)
	if err != nil {
		return "", err
	}

	fields := []cloneTargetField{
		{[]string{metadataKey, nameKey}, target.Identity.Name, true},
		{[]string{specKey, clusterKey, providerKey}, target.Provider, true},
		{[]string{specKey, clusterKey, connectionKey, contextKey}, context, false},
	}

	err = validateCloneFields(doc.Content[0], fields)
	if err != nil {
		return "", err
	}

	changed, err := applyCloneFields(doc.Content[0], fields)
	if err != nil {
		return "", err
	}

	if !changed {
		return content, nil
	}

	return encodeConfig(doc)
}

// parseIdentityConfig requires a YAML mapping before any identity traversal.
func parseIdentityConfig(content string) (*yamlv3.Node, error) {
	var doc yamlv3.Node

	err := yamlv3.Unmarshal([]byte(content), &doc)
	if err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if len(doc.Content) == 0 || doc.Content[0].Kind != yamlv3.MappingNode {
		return nil, fmt.Errorf("%w: config is not a YAML mapping", ErrInvalidConfig)
	}

	return &doc, nil
}

// replaceRootScalar replaces one validated, explicit root field as a string.
func replaceRootScalar(root *yamlv3.Node, keyPath []string, value string) (bool, error) {
	changed := ensureScalar(root, keyPath, value)

	node := root
	for _, key := range keyPath {
		if node == nil || node.Kind != yamlv3.MappingNode {
			return false, fmt.Errorf("%w: clone identity path is not a mapping", ErrInvalidConfig)
		}

		node = mappingValue(node, key)
	}

	if node == nil || node.Kind != yamlv3.ScalarNode ||
		(!changed && node.ShortTag() != yamlStringTag) {
		return false, fmt.Errorf("%w: clone identity field is not a string", ErrInvalidConfig)
	}

	if node.Value != value {
		node.Value = value
		changed = true
	}

	node.Tag = yamlStringTag

	return changed, nil
}

// encodeConfig renders doc with the indentation KSail writes its configs with.
func encodeConfig(doc *yamlv3.Node) (string, error) {
	var buf bytes.Buffer

	encoder := yamlv3.NewEncoder(&buf)
	encoder.SetIndent(identityIndent)

	err := encoder.Encode(doc)
	if err != nil {
		return "", fmt.Errorf("encoding config: %w", err)
	}

	err = encoder.Close()
	if err != nil {
		return "", fmt.Errorf("encoding config: %w", err)
	}

	return buf.String(), nil
}

// ensureScalar sets the scalar at keyPath under mapping to value when it is absent,
// empty or null, creating intermediate mappings as needed (in place of a null one
// too). It reports whether it changed anything; a present non-empty value, or a
// path blocked by a non-mapping node, is left untouched.
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
			if !isUnset(child) {
				return false
			}

			child.Tag = ""
			child.Style = 0
			child.Value = value

			return true
		case isNull(child):
			child.Kind = yamlv3.MappingNode
			child.Tag = ""
			child.Style = 0
			child.Value = ""
		case child.Kind != yamlv3.MappingNode:
			return false
		}

		node = child
	}

	return false
}

// isNull reports whether node is a YAML null scalar (`~`, `null`, or an empty value),
// which the config treats as unset.
func isNull(node *yamlv3.Node) bool {
	return node.Kind == yamlv3.ScalarNode && node.ShortTag() == "!!null"
}

// isUnset reports whether a leaf node holds no value: null, or an empty string.
func isUnset(node *yamlv3.Node) bool {
	return isNull(node) || (node.Kind == yamlv3.ScalarNode && node.Value == "")
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
				mapping.Content = slices.Insert(mapping.Content, i, pair...)

				return
			}
		}
	}

	mapping.Content = append(mapping.Content, pair...)
}
