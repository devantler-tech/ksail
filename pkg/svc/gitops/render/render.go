package render

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"maps"
	"sort"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"sigs.k8s.io/yaml"
)

// ErrNoResolver is returned by Expand when Options.Resolver is nil.
var ErrNoResolver = errors.New("render: no chart resolver configured")

// OriginKind classifies how a Document entered the rendered stream.
type OriginKind int

const (
	// OriginStream marks a document that came verbatim from the input stream
	// (kustomize build + Flux substitution output), e.g. a plain resource or a
	// HelmRelease that could not be rendered.
	OriginStream OriginKind = iota
	// OriginRendered marks a document produced by templating a HelmRelease.
	OriginRendered
)

// Provenance records where a Document came from so callers can attribute
// findings back to the originating HelmRelease.
type Provenance struct {
	Origin OriginKind
	// SourceHelmRelease is "namespace/name" of the HelmRelease that produced this
	// document when Origin is OriginRendered; empty otherwise.
	SourceHelmRelease string
	// ReleaseName is the Helm release name used to render, when OriginRendered.
	ReleaseName string
}

// Document is a single Kubernetes object in the rendered stream.
type Document struct {
	Bytes      []byte
	APIVersion string
	Kind       string
	Name       string
	Namespace  string
	Provenance Provenance
}

// Degradation records a HelmRelease that could not be rendered offline. The
// caller falls back to validating/scanning the HelmRelease CR itself. Silent
// degradations are expected in normal repos (e.g. a source object that lives in
// a different Flux Kustomization) and should not be surfaced as warnings.
type Degradation struct {
	HelmRelease string // "namespace/name"
	Reason      string
	Err         error
	Silent      bool
}

// Result is the outcome of expanding one manifest stream.
type Result struct {
	Documents    []Document
	Degradations []Degradation
}

// Options configures Expand.
type Options struct {
	// Resolver renders a HelmRelease into its child manifests. Required.
	Resolver ChartResolver
}

// objectMeta is the minimal identity probe decoded from each document.
type objectMeta struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
}

// Bytes joins the documents into a single multi-document YAML stream. Documents
// are already in a deterministic order (Expand sorts them), so the output is
// stable across runs — important for snapshot tests and reproducible scans.
func (r Result) Bytes() []byte {
	var buf bytes.Buffer

	for index, doc := range r.Documents {
		if index > 0 {
			buf.WriteString("---\n")
		}

		buf.Write(doc.Bytes)

		if len(doc.Bytes) == 0 || doc.Bytes[len(doc.Bytes)-1] != '\n' {
			buf.WriteByte('\n')
		}
	}

	return buf.Bytes()
}

// Expand parses the (kustomize-built, Flux-substituted) manifest stream and
// replaces each renderable HelmRelease with its rendered children. HelmReleases
// that cannot be rendered offline are left in place and recorded as
// Degradations. Expand returns an error only when Options is misconfigured; an
// unparseable document is passed through verbatim rather than failing the run.
func Expand(ctx context.Context, stream []byte, opts Options) (Result, error) {
	if opts.Resolver == nil {
		return Result{}, ErrNoResolver
	}

	docs, metas, index := parseSourceIndexedDocs(stream)

	var result Result

	for docIndex, doc := range docs {
		meta := metas[docIndex]
		if isHelmRelease(meta) {
			expandHelmRelease(ctx, opts.Resolver, index, doc, meta, &result)

			continue
		}

		result.Documents = append(
			result.Documents,
			newDocument(doc, meta, Provenance{Origin: OriginStream}),
		)
	}

	sortDocuments(result.Documents)

	return result, nil
}

// expandHelmRelease renders a single HelmRelease document, appending either its
// rendered children (on success) or the HelmRelease itself plus a Degradation
// (on failure) to result.
func expandHelmRelease(
	ctx context.Context,
	resolver ChartResolver,
	index SourceIndex,
	doc []byte,
	meta objectMeta,
	result *Result,
) {
	var helmRelease helmv2.HelmRelease

	err := yaml.Unmarshal(doc, &helmRelease)
	if err != nil {
		// Unparseable HelmRelease: pass through so CR-schema validation flags it.
		result.Documents = append(
			result.Documents,
			newDocument(doc, meta, Provenance{Origin: OriginStream}),
		)

		return
	}

	key := helmRelease.Namespace + "/" + helmRelease.Name

	manifest, err := resolver.Render(ctx, &helmRelease, index)
	if err != nil {
		result.Degradations = append(result.Degradations, Degradation{
			HelmRelease: key,
			Reason:      err.Error(),
			Err:         err,
			Silent:      isSilentDegradation(err),
		})
		result.Documents = append(
			result.Documents,
			newDocument(doc, meta, Provenance{Origin: OriginStream}),
		)

		return
	}

	provenance := Provenance{
		Origin:            OriginRendered,
		SourceHelmRelease: key,
		ReleaseName:       helmRelease.GetReleaseName(),
	}

	for _, child := range fsutil.SplitYAMLDocuments([]byte(manifest)) {
		result.Documents = append(
			result.Documents,
			newDocument(child, parseObjectMeta(child), provenance),
		)
	}
}

// isSilentDegradation reports whether a degradation is expected in normal repos
// and should not be surfaced as a user-facing warning.
func isSilentDegradation(err error) bool {
	return errors.Is(err, ErrSourceNotFound) || errors.Is(err, ErrUnsupportedSourceKind)
}

// newDocument builds a Document from raw bytes, its parsed identity, and provenance.
func newDocument(doc []byte, meta objectMeta, provenance Provenance) Document {
	return Document{
		Bytes:      doc,
		APIVersion: meta.APIVersion,
		Kind:       meta.Kind,
		Name:       meta.Metadata.Name,
		Namespace:  meta.Metadata.Namespace,
		Provenance: provenance,
	}
}

// parseObjectMeta best-effort decodes a document's GVK and name/namespace.
// Documents that are not single Kubernetes objects yield a zero objectMeta.
func parseObjectMeta(doc []byte) objectMeta {
	var meta objectMeta

	_ = yaml.Unmarshal(doc, &meta)

	return meta
}

// sortDocuments orders documents deterministically by (kind, namespace, name),
// falling back to the raw bytes so identical identities (or unnamed documents)
// keep a stable order. Downstream consumers (kubeconform, kubescape) are
// order-insensitive, so a stable total order only serves reproducibility.
func sortDocuments(docs []Document) {
	sort.SliceStable(docs, func(i, j int) bool {
		left, right := docs[i], docs[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}

		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}

		if left.Name != right.Name {
			return left.Name < right.Name
		}

		return bytes.Compare(left.Bytes, right.Bytes) < 0
	})
}

// parseSourceIndexedDocs splits a manifest stream into documents and builds
// each document's object meta plus the index of chart/values sources they
// declare — the shared prelude of Expand and EnumerateChartSpecs.
func parseSourceIndexedDocs(stream []byte) ([][]byte, []objectMeta, SourceIndex) {
	docs := fsutil.SplitYAMLDocuments(stream)

	index := newSourceIndex()
	metas := make([]objectMeta, len(docs))

	for docIndex, doc := range docs {
		metas[docIndex] = parseObjectMeta(doc)
		indexSource(&index, metas[docIndex], doc)
	}

	return docs, metas, index
}

// newSourceIndex returns an empty, ready-to-populate SourceIndex.
func newSourceIndex() SourceIndex {
	return SourceIndex{
		OCIRepos:   map[string]*sourcev1.OCIRepository{},
		HelmRepos:  map[string]*sourcev1.HelmRepository{},
		ConfigMaps: map[string]map[string]string{},
		Secrets:    map[string]map[string]string{},
	}
}

// indexSource records a source object (OCIRepository/HelmRepository) or a
// values source (ConfigMap/Secret) so HelmRelease rendering can resolve them.
func indexSource(index *SourceIndex, meta objectMeta, doc []byte) {
	key := meta.Metadata.Namespace + "/" + meta.Metadata.Name

	switch {
	case isSourceKind(meta, sourcev1.OCIRepositoryKind):
		indexOCIRepository(index, key, doc)
	case isSourceKind(meta, sourcev1.HelmRepositoryKind):
		indexHelmRepository(index, key, doc)
	case isCoreKind(meta, "ConfigMap"):
		indexValuesData(index.ConfigMaps, key, doc, false)
	case isCoreKind(meta, "Secret"):
		indexValuesData(index.Secrets, key, doc, true)
	}
}

// indexOCIRepository parses and records an OCIRepository source object.
func indexOCIRepository(index *SourceIndex, key string, doc []byte) {
	var repo sourcev1.OCIRepository
	if yaml.Unmarshal(doc, &repo) == nil {
		index.OCIRepos[key] = &repo
	}
}

// indexHelmRepository parses and records a HelmRepository source object.
func indexHelmRepository(index *SourceIndex, key string, doc []byte) {
	var repo sourcev1.HelmRepository
	if yaml.Unmarshal(doc, &repo) == nil {
		index.HelmRepos[key] = &repo
	}
}

// indexValuesData records ConfigMap/Secret string data for valuesFrom resolution.
func indexValuesData(dst map[string]map[string]string, key string, doc []byte, secret bool) {
	if data := extractStringData(doc, secret); data != nil {
		dst[key] = data
	}
}

// isHelmRelease reports whether meta identifies a Flux HelmRelease.
func isHelmRelease(meta objectMeta) bool {
	return groupOf(meta.APIVersion) == helmv2.GroupVersion.Group &&
		meta.Kind == helmv2.HelmReleaseKind
}

// isSourceKind reports whether meta identifies a source-controller object of the given kind.
func isSourceKind(meta objectMeta, kind string) bool {
	return groupOf(meta.APIVersion) == sourcev1.GroupVersion.Group && meta.Kind == kind
}

// isCoreKind reports whether meta identifies a core/v1 object of the given kind.
func isCoreKind(meta objectMeta, kind string) bool {
	return meta.APIVersion == "v1" && meta.Kind == kind
}

// groupOf returns the API group from an apiVersion ("group/version" → "group",
// "v1" → "").
func groupOf(apiVersion string) string {
	if group, _, found := strings.Cut(apiVersion, "/"); found {
		return group
	}

	return ""
}

// extractStringData decodes a ConfigMap/Secret's string data for valuesFrom
// resolution. For Secrets, base64 `data` is decoded and plaintext `stringData`
// overlaid; for ConfigMaps, `data` is used verbatim. Returns nil when empty.
func extractStringData(doc []byte, secret bool) map[string]string {
	var obj struct {
		Data       map[string]string `json:"data"`
		StringData map[string]string `json:"stringData"`
	}

	if yaml.Unmarshal(doc, &obj) != nil {
		return nil
	}

	out := make(map[string]string, len(obj.Data)+len(obj.StringData))

	for dataKey, value := range obj.Data {
		if secret {
			decoded, decodeErr := base64.StdEncoding.DecodeString(value)
			if decodeErr != nil {
				continue
			}

			out[dataKey] = string(decoded)

			continue
		}

		out[dataKey] = value
	}

	maps.Copy(out, obj.StringData)

	if len(out) == 0 {
		return nil
	}

	return out
}
