package clusterapi

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/yaml"
)

// Ensure the local backend can apply manifests.
var _ api.ApplyService = (*Service)(nil)

const (
	applyFieldManager = "ksail-ui"
	applyStatusOK     = "applied"
	applyStatusError  = "error"
)

// applyClientFunc builds a dynamic client + RESTMapper for a named cluster. Server-side apply needs a
// mapper to resolve each manifest's GroupVersionKind to a resource (so arbitrary kinds, incl. CRDs,
// work). Injectable so tests can substitute a fake client + static mapper.
type applyClientFunc func(
	ctx context.Context,
	clusterName string,
) (dynamic.Interface, meta.RESTMapper, error)

// defaultApplyClient resolves the cluster's kubeconfig context and builds a dynamic client plus a
// discovery-backed REST mapper against it.
func defaultApplyClient(
	_ context.Context,
	clusterName string,
) (dynamic.Interface, meta.RESTMapper, error) {
	restConfig, err := restConfigForCluster(clusterName)
	if err != nil {
		return nil, nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("build dynamic client for %q: %w", clusterName, err)
	}

	httpClient, err := rest.HTTPClientFor(restConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("build http client for %q: %w", clusterName, err)
	}

	mapper, err := apiutil.NewDynamicRESTMapper(restConfig, httpClient)
	if err != nil {
		return nil, nil, fmt.Errorf("build rest mapper for %q: %w", clusterName, err)
	}

	return dynamicClient, mapper, nil
}

// splitManifests splits multi-document YAML into individual, trimmed, non-empty documents. A
// malformed document separator (e.g. `--- junk`) is surfaced as an ErrInvalid-wrapped error rather
// than silently dropping the in-progress and following documents (which would apply a subset and
// report a false success).
func splitManifests(data []byte) ([][]byte, error) {
	reader := yamlutil.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))

	var docs [][]byte

	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("%w: %w", api.ErrInvalid, err)
		}

		doc = bytes.TrimSpace(doc)
		if len(doc) > 0 {
			docs = append(docs, doc)
		}
	}

	return docs, nil
}

// ApplyManifests server-side-applies each document in the supplied multi-document YAML to the named
// cluster, returning a per-document result. A per-document failure is recorded (not fatal) so one bad
// document does not abort the batch. dryRun applies server-side without persisting (validation).
func (s *Service) ApplyManifests(
	ctx context.Context,
	_, name string,
	manifests []byte,
	dryRun bool,
) ([]api.ApplyResult, error) {
	docs, err := splitManifests(manifests)
	if err != nil {
		return nil, err
	}

	if len(docs) == 0 {
		return nil, fmt.Errorf("%w: no manifests provided", api.ErrInvalid)
	}

	dynamicClient, mapper, err := s.newApplyClient(ctx, name)
	if err != nil {
		return nil, err
	}

	results := make([]api.ApplyResult, 0, len(docs))
	for _, doc := range docs {
		results = append(results, applyOne(ctx, dynamicClient, mapper, doc, dryRun))
	}

	return results, nil
}

// applyOne parses and server-side-applies a single manifest document, returning its result.
func applyOne(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	mapper meta.RESTMapper,
	doc []byte,
	dryRun bool,
) api.ApplyResult {
	obj := &unstructured.Unstructured{}

	err := yaml.Unmarshal(doc, &obj.Object)
	if err != nil {
		return api.ApplyResult{
			Status: applyStatusError,
			Error:  fmt.Sprintf("parse manifest: %v", err),
		}
	}

	gvk := obj.GroupVersionKind()
	result := api.ApplyResult{Kind: gvk.Kind, Name: obj.GetName(), Namespace: obj.GetNamespace()}

	if gvk.Kind == "" || obj.GetName() == "" {
		result.Status = applyStatusError
		result.Error = "manifest missing apiVersion/kind or metadata.name"

		return result
	}

	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		result.Status = applyStatusError
		result.Error = fmt.Sprintf("resolve %s: %v", gvk.String(), err)

		return result
	}

	resource := resourceInterfaceFor(dynamicClient, mapping, obj, &result)

	options := metav1.ApplyOptions{FieldManager: applyFieldManager, Force: true}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}

	_, err = resource.Apply(ctx, obj.GetName(), obj, options)
	if err != nil {
		result.Status = applyStatusError
		result.Error = err.Error()

		return result
	}

	result.Status = applyStatusOK

	return result
}

// resourceInterfaceFor returns the dynamic resource client for a mapping, scoping it to the object's
// namespace (defaulting to "default") for namespaced kinds and updating result.Namespace to match.
func resourceInterfaceFor(
	dynamicClient dynamic.Interface,
	mapping *meta.RESTMapping,
	obj *unstructured.Unstructured,
	result *api.ApplyResult,
) dynamic.ResourceInterface {
	if mapping.Scope.Name() != meta.RESTScopeNameNamespace {
		return dynamicClient.Resource(mapping.Resource)
	}

	namespace := obj.GetNamespace()
	if namespace == "" {
		namespace = "default"
	}

	result.Namespace = namespace

	return dynamicClient.Resource(mapping.Resource).Namespace(namespace)
}
