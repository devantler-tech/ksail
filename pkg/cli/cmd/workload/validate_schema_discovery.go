package workload

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/render"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"
)

// discoverySchemaInput selects built-ins and the Flux objects needed by Helm
// rendering. Other custom resources remain in the stream, with placeholders
// preserved until the same render has supplied any CRD-derived schemas.
func discoverySchemaInput(data []byte) ([]byte, map[string]struct{}) {
	var inputs [][]byte

	deferred := make(map[string]struct{})

	for _, document := range fsutil.SplitYAMLDocuments(data) {
		var meta metav1.TypeMeta

		err := yaml.Unmarshal(document, &meta)
		if err != nil || schemaNeededDuringRender(meta) {
			// Keep malformed input in the strict prefetch path rather than hide it.
			inputs = append(inputs, document)

			continue
		}

		deferred[meta.APIVersion+"/"+meta.Kind] = struct{}{}
	}

	return bytes.Join(inputs, []byte("\n---\n")), deferred
}

func schemaNeededDuringRender(meta metav1.TypeMeta) bool {
	switch meta.Kind {
	case "HelmRelease", "HelmRepository", "OCIRepository", "CustomResourceDefinition":
		return true
	default:
		return clientgoscheme.Scheme.Recognizes(
			schema.FromAPIVersionAndKind(meta.APIVersion, meta.Kind),
		)
	}
}

// finishRenderedValidationSubstitutions types deferred source documents after
// CRD discovery without rebuilding or re-rendering. Helm-generated documents
// never receive Flux substitution: Flux substitutes the HelmRelease, not its
// later children. Explicit refresh still requires schemas for every final object.
func finishRenderedValidationSubstitutions(
	ctx context.Context,
	result render.Result,
	opts *kubeconform.ValidationOptions,
) ([]byte, error) {
	result.Documents = slices.Clone(result.Documents)
	for index, document := range result.Documents {
		if document.Provenance.Origin != render.OriginStream ||
			!bytes.Contains(document.Bytes, []byte("${")) {
			continue
		}

		expanded, err := expandValidationSubstitutions(ctx, document.Bytes, opts)
		if err != nil {
			return nil, fmt.Errorf("prepare deferred source schemas: %w", err)
		}

		result.Documents[index].Bytes = expanded
	}

	data := result.Bytes()
	if opts != nil && opts.CacheDirectory != "" {
		err := kubeconform.NewClient().PrecacheSchemas(ctx, data, opts)
		if err != nil {
			return nil, fmt.Errorf("refresh rendered schemas: %w", err)
		}
	}

	return data, nil
}
