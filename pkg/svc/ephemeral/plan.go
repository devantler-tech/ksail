// Package ephemeral prepares manifests for admission checks in an isolated cluster.
package ephemeral

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/client/kustomize"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/render"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/json"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

// ErrInput identifies a workload that cannot be faithfully applied in an ephemeral run.
var ErrInput = errors.New("unsupported ephemeral input")

// Plan separates bootstrap prerequisites from workloads that need installed operators.
// Helm owns chart-generated objects; only directly declared resources are applied separately.
type Plan struct {
	Namespaces    []*unstructured.Unstructured
	CRDs          []*unstructured.Unstructured
	Configuration []*unstructured.Unstructured
	Resources     []*unstructured.Unstructured
	Charts        []*helm.ChartSpec
}

// Load builds one selected Kustomize root, YAML file, or plain manifest directory.
// It never substitutes synthetic values or separately builds a selected overlay's bases.
func Load(ctx context.Context, path string) (*Plan, error) {
	path, err := fsutil.EvalCanonicalPath(path)
	if err != nil {
		return nil, fmt.Errorf("resolve ephemeral input: %w", err)
	}

	stream, err := readInput(ctx, path)
	if err != nil {
		return nil, err
	}

	plan, err := parsePlan(stream)
	if err != nil {
		return nil, err
	}

	plan.Charts, err = enumerateCharts(stream)
	if err != nil {
		return nil, err
	}

	err = plan.validateConfigurationNamespaces()
	if err != nil {
		return nil, err
	}

	return plan, nil
}

func parsePlan(stream []byte) (*Plan, error) {
	plan := &Plan{}
	reader := k8syaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(stream)))
	seen := make(map[string]bool)

	for {
		doc, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}

		if readErr != nil {
			return nil, fmt.Errorf("read ephemeral document: %w", readErr)
		}

		obj, decodeErr := decodeDocument(doc)
		if decodeErr != nil {
			return nil, decodeErr
		}

		if len(obj.Object) == 0 {
			continue
		}

		key := declaredIdentity(obj)
		if seen[key] {
			return nil, fmt.Errorf("%w: duplicate resource %s", ErrInput, key)
		}

		seen[key] = true

		err := plan.classify(obj)
		if err != nil {
			return nil, err
		}
	}

	return plan, nil
}

func enumerateCharts(stream []byte) ([]*helm.ChartSpec, error) {
	charts, degradations := render.EnumerateChartSpecs(stream)
	if len(degradations) > 0 {
		return nil, fmt.Errorf("%w: cannot resolve HelmRelease %s: %s",
			ErrInput, degradations[0].HelmRelease, degradations[0].Reason)
	}

	seenCharts := make(map[string]bool)

	for _, chart := range charts {
		key := chart.Namespace + "/" + chart.ReleaseName
		if seenCharts[key] {
			return nil, fmt.Errorf("%w: duplicate Helm release %s", ErrInput, key)
		}

		seenCharts[key] = true
	}

	return charts, nil
}

func declaredIdentity(obj *unstructured.Unstructured) string {
	namespace := obj.GetNamespace()
	if namespace == "" {
		namespace = "default"
	}

	return obj.GroupVersionKind().GroupKind().String() + "/" + namespace + "/" + obj.GetName()
}

func (p *Plan) validateConfigurationNamespaces() error {
	known := map[string]bool{
		"":                true,
		"default":         true,
		"kube-system":     true,
		"kube-public":     true,
		"kube-node-lease": true,
	}
	for _, namespace := range p.Namespaces {
		known[namespace.GetName()] = true
	}

	for _, resource := range p.Configuration {
		if namespace := resource.GetNamespace(); !known[namespace] {
			return fmt.Errorf("%w: declare Namespace %s for %s before chart installation",
				ErrInput, namespace, resourceIdentity(resource))
		}
	}

	return nil
}

func readInput(ctx context.Context, path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat ephemeral input: %w", err)
	}

	if !info.IsDir() {
		data, readErr := fsutil.ReadFileSafe(filepath.Dir(path), path)
		if readErr != nil {
			return nil, fmt.Errorf("read ephemeral input: %w", readErr)
		}

		return data, nil
	}

	if isKustomizeRoot(path) {
		output, buildErr := kustomize.NewClient().Build(ctx, path)
		if buildErr != nil {
			return nil, fmt.Errorf("build ephemeral input: %w", buildErr)
		}

		return output.Bytes(), nil
	}

	return collectDirectory(ctx, path)
}

func collectDirectory(ctx context.Context, path string) ([]byte, error) {
	var stream bytes.Buffer

	err := filepath.WalkDir(path, func(file string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		err := ctx.Err()
		if err != nil {
			return fmt.Errorf("collection cancelled: %w", err)
		}

		if entry.IsDir() {
			if isKustomizeRoot(file) {
				return fmt.Errorf(
					"%w: select a Kustomize root explicitly instead of its parent directory",
					ErrInput,
				)
			}

			return nil
		}

		ext := strings.ToLower(filepath.Ext(file))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		data, readErr := fsutil.ReadFileSafe(path, file)
		if readErr != nil {
			return fmt.Errorf("read manifest %s: %w", file, readErr)
		}

		stream.Write(data)
		stream.WriteString("\n---\n")

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect ephemeral input: %w", err)
	}

	return stream.Bytes(), nil
}

func isKustomizeRoot(path string) bool {
	for _, name := range []string{"kustomization.yaml", "kustomization.yml", "Kustomization"} {
		info, err := os.Stat(filepath.Join(path, name))
		if err == nil && !info.IsDir() {
			return true
		}
	}

	return false
}

func decodeDocument(doc []byte) (*unstructured.Unstructured, error) {
	data, err := yaml.YAMLToJSONStrict(doc)
	if err != nil {
		return nil, fmt.Errorf("decode ephemeral document: %w", err)
	}

	var object map[string]any

	err = json.Unmarshal(data, &object)
	if err != nil {
		return nil, fmt.Errorf("decode ephemeral object: %w", err)
	}

	obj := &unstructured.Unstructured{Object: object}
	if len(object) == 0 {
		return obj, nil
	}

	err = validateDocument(obj, data)
	if err != nil {
		return nil, err
	}

	return obj, nil
}

func validateDocument(obj *unstructured.Unstructured, data []byte) error {
	if obj.GetKind() == "List" || (obj.GetName() == "" && obj.IsList()) {
		return fmt.Errorf("%w: expand List documents into individual resources", ErrInput)
	}

	if obj.GetAPIVersion() == "" || obj.GetKind() == "" || obj.GetName() == "" {
		return fmt.Errorf(
			"%w: each document requires apiVersion, kind, and metadata.name",
			ErrInput,
		)
	}

	if bytes.Contains(data, []byte("${")) {
		return fmt.Errorf(
			"%w: resolve Flux substitution expressions in %s/%s before admission checks",
			ErrInput,
			obj.GetKind(),
			obj.GetName(),
		)
	}

	return nil
}

func (p *Plan) classify(obj *unstructured.Unstructured) error {
	group := obj.GroupVersionKind().Group

	switch group + "/" + obj.GetKind() {
	case "kustomize.toolkit.fluxcd.io/Kustomization":
		return fmt.Errorf(
			"%w: select the workload referenced by Flux Kustomization %s",
			ErrInput,
			obj.GetName(),
		)
	case "helm.toolkit.fluxcd.io/HelmRelease":
		return validateHelmRelease(obj)
	case "source.toolkit.fluxcd.io/HelmRepository", "source.toolkit.fluxcd.io/OCIRepository":
		// Chart source descriptors are resolved by EnumerateChartSpecs.
	case "/Namespace":
		p.Namespaces = append(p.Namespaces, obj)
	case "apiextensions.k8s.io/CustomResourceDefinition":
		p.CRDs = append(p.CRDs, obj)
	case "/ConfigMap", "/Secret":
		p.Configuration = append(p.Configuration, obj)
	default:
		p.Resources = append(p.Resources, obj)
	}

	return nil
}

func validateHelmRelease(obj *unstructured.Unstructured) error {
	// EnumerateChartSpecs skips malformed typed objects; reject them explicitly.
	data, err := json.Marshal(obj.Object)
	if err != nil {
		return fmt.Errorf("encode HelmRelease %s: %w", obj.GetName(), err)
	}

	var release helmv2.HelmRelease

	err = json.Unmarshal(data, &release)
	if err != nil {
		return fmt.Errorf("decode HelmRelease %s: %w", obj.GetName(), err)
	}

	if len(release.Spec.PostRenderers) > 0 {
		return fmt.Errorf(
			"%w: HelmRelease %s postRenderers are not supported by ephemeral admission",
			ErrInput,
			obj.GetName(),
		)
	}

	return nil
}
