package render

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	meta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	helmv4strvals "helm.sh/helm/v4/pkg/strvals"
	"sigs.k8s.io/yaml"
)

// helmRenderMu serializes the in-process Helm template step across concurrent
// renders. Helm's chart download + render path mutates process-global on-disk
// caches (HELM_REPOSITORY_CACHE / HELM_CONTENT_CACHE, read from shared defaults
// by every helm.NewTemplateOnlyClient) and other shared SDK state that is not
// safe for concurrent use. validate (and scan) render kustomizations in
// parallel, and unserialized renders corrupted that shared cache state.
// Constructing a fresh client per render is not enough because the shared state
// lives on disk, below the client. Serializing only the template step keeps the
// expensive parallel work (kustomize build, kubeconform validation) unaffected,
// and keeps the shared chart cache warm so each chart is downloaded at most once.
//
// This lock does not, on its own, resolve the #5362 YAML corruption. That
// corruption's race-detector-confirmed root cause is a separate data race inside
// kubeconform's resource.FromStream, which aliases the bufio.Scanner buffer
// across documents it emits and validates concurrently; the large multi-resource
// streams this in-process render produces merely widen that race (the in-process
// render itself was a red herring). The kubeconform client avoids that path by
// splitting rendered streams into independent single-document validations before
// handing them to kubeconform. The lock still earns its place by preventing the
// distinct Helm shared-cache corruption above.
//
//nolint:gochecknoglobals // package-level lock guarding Helm's process-global render state
var helmRenderMu sync.Mutex

var (
	// ErrSourceNotFound is returned when a HelmRelease references a source object
	// that is not present in the stream (normal in multi-Kustomization repos).
	ErrSourceNotFound = errors.New("referenced source not present in stream")
	// ErrUnsupportedSourceKind is returned for source kinds that cannot be
	// rendered offline yet (GitRepository, HelmChart, Bucket).
	ErrUnsupportedSourceKind = errors.New("unsupported chart source kind")
	// ErrNoChartSource is returned when a HelmRelease has neither chartRef nor
	// chart.sourceRef.
	ErrNoChartSource = errors.New("HelmRelease has neither chartRef nor chart.sourceRef")
)

// ChartResolver renders a HelmRelease (already overlay-patched and substituted)
// into the multi-document YAML of its rendered children. Implementations resolve
// the chart from the source objects discovered in the same stream. It is an
// interface so tests can inject a fake that renders a local chart offline.
type ChartResolver interface {
	Render(
		ctx context.Context,
		helmRelease *helmv2.HelmRelease,
		sources SourceIndex,
	) (string, error)
}

// SourceIndex holds the objects parsed out of the manifest stream that a
// HelmRelease may reference, keyed by "namespace/name".
type SourceIndex struct {
	OCIRepos   map[string]*sourcev1.OCIRepository
	HelmRepos  map[string]*sourcev1.HelmRepository
	ConfigMaps map[string]map[string]string // valuesFrom data (plaintext)
	Secrets    map[string]map[string]string // valuesFrom data (decoded plaintext)
}

// HelmChartResolver is the production ChartResolver, backed by the in-process,
// kubeconfig-free Helm template client (helm.NewTemplateOnlyClient()).
type HelmChartResolver struct {
	helm  helm.Interface
	cache *ChartCache
}

var _ ChartResolver = (*HelmChartResolver)(nil)

// ResolverOption configures a HelmChartResolver.
type ResolverOption func(*HelmChartResolver)

// WithChartCache makes the resolver serve identical chart renders from the
// shared cache instead of re-templating. Pass one cache per validate/scan run
// (see ChartCache) so identical HelmReleases across kustomizations are
// templated once.
func WithChartCache(cache *ChartCache) ResolverOption {
	return func(r *HelmChartResolver) { r.cache = cache }
}

// NewHelmChartResolver returns a resolver that renders charts with the given
// Helm client. Pass a client from helm.NewTemplateOnlyClient() for offline use.
func NewHelmChartResolver(client helm.Interface, opts ...ResolverOption) *HelmChartResolver {
	resolver := &HelmChartResolver{helm: client}
	for _, opt := range opts {
		opt(resolver)
	}

	return resolver
}

// Render resolves the chart source for helmRelease and templates it in-process.
//
// buildChartSpec is pure (in-memory) and runs concurrently; the Helm template
// call is then serialized by helmRenderMu (see its doc) because it touches
// Helm's process-global on-disk caches, which concurrent renders corrupted
// (issue #5362). The deferred Unlock holds the lock only across that call plus a
// trivial error-wrap, and stays correct even if TemplateChart panics.
//
// When a ChartCache is configured (WithChartCache), the lookup and store happen
// inside the helmRenderMu critical section: because that lock already serializes
// every render, a second render of the same spec finds the first result and
// skips TemplateChart entirely (no thundering herd), and cache hits release the
// lock immediately. Only successful renders are cached — a template error is
// returned as before, never memoized.
func (r *HelmChartResolver) Render(
	ctx context.Context,
	helmRelease *helmv2.HelmRelease,
	sources SourceIndex,
) (string, error) {
	spec, err := buildChartSpec(helmRelease, sources)
	if err != nil {
		return "", err
	}

	helmRenderMu.Lock()
	defer helmRenderMu.Unlock()

	if r.cache != nil {
		if manifest, ok := r.cache.get(spec); ok {
			return manifest, nil
		}
	}

	manifest, err := r.helm.TemplateChart(ctx, spec)
	if err != nil {
		return "", fmt.Errorf(
			"template chart for HelmRelease %s/%s: %w",
			helmRelease.Namespace, helmRelease.Name, err,
		)
	}

	if r.cache != nil {
		r.cache.put(spec, manifest)
	}

	return manifest, nil
}

// buildChartSpec maps a HelmRelease and the in-stream source objects to a
// helm.ChartSpec. It mirrors, in reverse, the source-kind handling in
// pkg/cli/cmd/workload/gen/helmrelease.go.
func buildChartSpec(helmRelease *helmv2.HelmRelease, sources SourceIndex) (*helm.ChartSpec, error) {
	spec := &helm.ChartSpec{
		ReleaseName: helmRelease.GetReleaseName(),
		Namespace:   helmRelease.GetReleaseNamespace(),
	}

	var err error

	switch {
	case helmRelease.Spec.ChartRef != nil:
		err = applyChartRef(spec, helmRelease, sources)
	case helmRelease.Spec.Chart != nil:
		err = applySourceRef(spec, helmRelease, sources)
	default:
		err = ErrNoChartSource
	}

	if err != nil {
		return nil, err
	}

	values, err := buildValues(helmRelease, sources)
	if err != nil {
		return nil, err
	}

	spec.ValuesYaml = values

	return spec, nil
}

// applyChartRef resolves the newer spec.chartRef form (OCIRepository).
func applyChartRef(
	spec *helm.ChartSpec,
	helmRelease *helmv2.HelmRelease,
	sources SourceIndex,
) error {
	ref := helmRelease.Spec.ChartRef
	if ref.Kind != sourcev1.OCIRepositoryKind {
		return fmt.Errorf("%w: chartRef kind %q", ErrUnsupportedSourceKind, ref.Kind)
	}

	key := sourceKey(ref.Namespace, ref.Name, helmRelease.Namespace)

	repo, ok := sources.OCIRepos[key]
	if !ok {
		return fmt.Errorf("%w: OCIRepository %s", ErrSourceNotFound, key)
	}

	applyOCIRepository(spec, repo)

	return nil
}

// applySourceRef resolves the classic spec.chart.spec.sourceRef form (HelmRepository).
func applySourceRef(
	spec *helm.ChartSpec,
	helmRelease *helmv2.HelmRelease,
	sources SourceIndex,
) error {
	chart := helmRelease.Spec.Chart.Spec
	ref := chart.SourceRef

	switch ref.Kind {
	case sourcev1.HelmRepositoryKind:
		key := sourceKey(ref.Namespace, ref.Name, helmRelease.Namespace)

		repo, ok := sources.HelmRepos[key]
		if !ok {
			return fmt.Errorf("%w: HelmRepository %s", ErrSourceNotFound, key)
		}

		applyHelmRepository(spec, repo, chart.Chart, chart.Version)

		return nil
	default:
		return fmt.Errorf("%w: sourceRef kind %q", ErrUnsupportedSourceKind, ref.Kind)
	}
}

// applyOCIRepository maps an OCIRepository (which is itself the chart) to the
// chart spec. Version precedence follows Flux: digest, then semver, then tag.
func applyOCIRepository(spec *helm.ChartSpec, repo *sourcev1.OCIRepository) {
	spec.ChartName = repo.Spec.URL

	ref := repo.Spec.Reference
	if ref == nil {
		return
	}

	switch {
	case ref.Digest != "":
		spec.ChartName = strings.TrimSuffix(repo.Spec.URL, "/") + "@" + ref.Digest
	case ref.SemVer != "":
		spec.Version = ref.SemVer
	case ref.Tag != "":
		spec.Version = ref.Tag
	}
}

// applyHelmRepository maps a HelmRepository plus the chart name/version to the
// chart spec. An OCI-type HelmRepository is addressed as oci://repo/chart; a
// default (HTTP) repository uses RepoURL + chart name.
func applyHelmRepository(
	spec *helm.ChartSpec,
	repo *sourcev1.HelmRepository,
	chartName, version string,
) {
	spec.Version = version

	if repo.Spec.Type == sourcev1.HelmRepositoryTypeOCI {
		spec.ChartName = strings.TrimSuffix(repo.Spec.URL, "/") + "/" + chartName

		return
	}

	spec.RepoURL = repo.Spec.URL
	spec.ChartName = chartName
}

// buildValues merges spec.values with any in-repo valuesFrom sources and returns
// the result as YAML for ChartSpec.ValuesYaml. The stream is already
// Flux-substituted before Expand is called, so no substitution happens here.
func buildValues(helmRelease *helmv2.HelmRelease, sources SourceIndex) (string, error) {
	values := helmRelease.GetValues()
	if values == nil {
		values = map[string]any{}
	}

	for index := range helmRelease.Spec.ValuesFrom {
		applyValuesFrom(values, helmRelease.Spec.ValuesFrom[index], helmRelease.Namespace, sources)
	}

	if len(values) == 0 {
		return "", nil
	}

	raw, err := yaml.Marshal(values)
	if err != nil {
		return "", fmt.Errorf(
			"marshal values for HelmRelease %s/%s: %w",
			helmRelease.Namespace, helmRelease.Name, err,
		)
	}

	return string(raw), nil
}

// lookupValuesRef resolves a valuesFrom reference's raw value from the in-stream
// ConfigMap/Secret index, returning it and whether it was found. It is the single
// source of truth for "can this reference be resolved offline?", shared by
// applyValuesFrom (which merges the value) and unresolvedValueRefs (which reports
// when it cannot be resolved).
func lookupValuesRef(
	ref meta.ValuesReference,
	namespace string,
	sources SourceIndex,
) (string, bool) {
	var data map[string]string

	switch ref.Kind {
	case "ConfigMap":
		data = sources.ConfigMaps[namespace+"/"+ref.Name]
	case "Secret":
		data = sources.Secrets[namespace+"/"+ref.Name]
	default:
		return "", false
	}

	raw, ok := data[ref.GetValuesKey()]

	return raw, ok
}

// applyValuesFrom merges one valuesFrom reference into values, resolving it from
// the in-repo ConfigMap/Secret index. References to objects not present in the
// stream (typically cluster-managed) are skipped. A reference with a targetPath
// injects its single flat value at that path (see applyTargetPathValue);
// otherwise the referenced value is YAML-merged at the root.
func applyValuesFrom(
	values map[string]any,
	ref meta.ValuesReference,
	namespace string,
	sources SourceIndex,
) {
	raw, ok := lookupValuesRef(ref, namespace, sources)
	if !ok {
		return
	}

	if ref.TargetPath != "" {
		applyTargetPathValue(values, ref, raw)

		return
	}

	var parsed map[string]any

	if yaml.Unmarshal([]byte(raw), &parsed) != nil {
		return
	}

	mergeValues(values, parsed)
}

// applyTargetPathValue merges a single flat value at ref.TargetPath, mirroring
// helm-controller's valuesFrom targetPath handling so the offline render matches
// what Flux applies. A non-literal reference uses Helm's --set-string semantics
// (the target path is interpreted but the value stays a string); a literal
// reference uses --set-literal semantics (the value is injected verbatim, with
// no comma/bracket/dot/equals interpretation — the safe path for config files,
// JSON blobs and multi-line strings). A malformed assignment is skipped rather
// than failing the whole offline render, matching the root-merge branch.
func applyTargetPathValue(values map[string]any, ref meta.ValuesReference, raw string) {
	assignment := fmt.Sprintf("%s=%s", ref.TargetPath, raw)

	parse := helmv4strvals.ParseIntoString
	if ref.Literal {
		parse = helmv4strvals.ParseLiteralInto
	}

	_ = parse(assignment, values)
}

// sourceKey builds the "namespace/name" lookup key, defaulting an empty
// reference namespace to the HelmRelease's namespace (Flux cross-namespace
// default).
func sourceKey(refNamespace, refName, defaultNamespace string) string {
	namespace := refNamespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	return namespace + "/" + refName
}

// mergeValues deep-merges src into dst (src wins on conflicts).
func mergeValues(dst, src map[string]any) {
	for key, srcValue := range src {
		if srcMap, ok := srcValue.(map[string]any); ok {
			if dstMap, ok := dst[key].(map[string]any); ok {
				mergeValues(dstMap, srcMap)

				continue
			}
		}

		dst[key] = srcValue
	}
}
