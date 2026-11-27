package workload

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"sigs.k8s.io/kustomize/kyaml/kio"
	kyaml "sigs.k8s.io/kustomize/kyaml/yaml"
)

// ErrFluxCommonMetadataInvalid is returned when a Flux Kustomization's
// spec.commonMetadata is not a map of string labels and annotations.
var ErrFluxCommonMetadataInvalid = errors.New("invalid Flux Kustomization spec.commonMetadata")

// withCommonMetadata sets object's spec.commonMetadata labels and annotations
// on every document in data, as kustomize-controller does to every object it
// applies after the build: a key the metadata names replaces the object's own
// value, and every other key is kept. Objects a HelmRelease renders later are
// not applied by the Kustomization, so callers apply this before Helm
// rendering. data is returned unchanged when the field is unset.
func withCommonMetadata(data []byte, object map[string]any) ([]byte, error) {
	labels, annotations, err := commonMetadataOf(object)
	if err != nil {
		return nil, err
	}

	if len(labels) == 0 && len(annotations) == 0 {
		return data, nil
	}

	nodes, err := kio.FromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("read built resources: %w", err)
	}

	for _, node := range nodes {
		err = mergeMetadata(node, labels, annotations)
		if err != nil {
			return nil, err
		}
	}

	out, err := kio.StringAll(nodes)
	if err != nil {
		return nil, fmt.Errorf("write built resources: %w", err)
	}

	return []byte(out), nil
}

// commonMetadataOf returns object's spec.commonMetadata labels and annotations,
// both empty when the field is unset.
func commonMetadataOf(object map[string]any) (map[string]string, map[string]string, error) {
	spec, _ := object["spec"].(map[string]any)

	metadata, set := spec["commonMetadata"]
	if !set || metadata == nil {
		return nil, nil, nil
	}

	fields, isMap := metadata.(map[string]any)
	if !isMap {
		return nil, nil, fmt.Errorf("%w: not a map", ErrFluxCommonMetadataInvalid)
	}

	labels, err := stringMap(fields, "labels")
	if err != nil {
		return nil, nil, err
	}

	annotations, err := stringMap(fields, "annotations")
	if err != nil {
		return nil, nil, err
	}

	return labels, annotations, nil
}

// mergeMetadata sets labels and annotations on node, replacing same-named keys.
func mergeMetadata(node *kyaml.RNode, labels, annotations map[string]string) error {
	if len(labels) > 0 {
		merged := node.GetLabels()
		maps.Copy(merged, labels)

		err := node.SetLabels(merged)
		if err != nil {
			return fmt.Errorf("set commonMetadata labels on %s: %w", describeNode(node), err)
		}
	}

	if len(annotations) > 0 {
		merged := node.GetAnnotations()
		maps.Copy(merged, annotations)

		err := node.SetAnnotations(merged)
		if err != nil {
			return fmt.Errorf("set commonMetadata annotations on %s: %w", describeNode(node), err)
		}
	}

	return nil
}

// stringMap returns fields[key] as a map of strings. A missing key is empty.
func stringMap(fields map[string]any, key string) (map[string]string, error) {
	value, set := fields[key]
	if !set || value == nil {
		return map[string]string{}, nil
	}

	entries, isMap := value.(map[string]any)
	if !isMap {
		return nil, fmt.Errorf("%w: %s is not a map", ErrFluxCommonMetadataInvalid, key)
	}

	out := make(map[string]string, len(entries))

	for _, name := range slices.Sorted(maps.Keys(entries)) {
		text, isString := entries[name].(string)
		if !isString {
			return nil, fmt.Errorf(
				"%w: %s.%s is not a string", ErrFluxCommonMetadataInvalid, key, name,
			)
		}

		out[name] = text
	}

	return out, nil
}

// describeNode names a resource for an error message.
func describeNode(node *kyaml.RNode) string {
	parts := []string{node.GetKind(), node.GetNamespace(), node.GetName()}

	return strings.Join(slices.DeleteFunc(parts, func(part string) bool { return part == "" }), "/")
}
