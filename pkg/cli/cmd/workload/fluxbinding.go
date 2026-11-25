package workload

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/kyvernopolicy"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kustomizeTypes "sigs.k8s.io/kustomize/api/types"
)

// Named Flux binding diagnostics. Each one fails validation on its own, so a
// binding that cannot be trusted is never evaluated as if it were known.
var (
	// ErrUnboundFluxKustomization marks a Kyverno failure in a folder no Flux
	// Kustomization from this source applies: it has no admission identity.
	ErrUnboundFluxKustomization = errors.New("unbound: not applied by any Flux Kustomization")
	// ErrAmbiguousFluxBinding is returned when two Flux Kustomizations apply the
	// same folder with a different identity, targetNamespace or patches.
	ErrAmbiguousFluxBinding = errors.New("ambiguous Flux Kustomization binding")
	// ErrFluxBindingOutsideTree is returned when a Flux Kustomization's
	// spec.path resolves outside the validated tree.
	ErrFluxBindingOutsideTree = errors.New("flux Kustomization path is outside the validated tree")
	// ErrFluxBindingPathNotFound is returned when a Flux Kustomization's
	// spec.path names a folder that does not exist.
	ErrFluxBindingPathNotFound = errors.New("flux Kustomization path does not exist")
	// ErrFluxBindingsDoNotSettle is returned when composing parent Flux
	// Kustomizations keeps changing the bindings of their children.
	ErrFluxBindingsDoNotSettle = errors.New("flux Kustomization bindings do not settle")
	// ErrFluxFieldNotModelled is returned when a Flux Kustomization sets a spec
	// field that validation does not reproduce, so its admission is not guessed.
	ErrFluxFieldNotModelled = errors.New("flux Kustomization field is not modelled")
)

const (
	// fluxSystemNamespace and fluxSystemSource name the source that delivers the
	// validated tree itself: both a FluxInstance sync and flux bootstrap create
	// it as flux-system in the flux-system namespace.
	fluxSystemNamespace = "flux-system"
	fluxSystemSource    = "flux-system"
	// fluxControllerAccount is the kustomize-controller's own service account,
	// the identity Flux applies with when a Kustomization names none.
	fluxControllerAccount = "kustomize-controller"
	fluxKustomizeGroup    = "kustomize.toolkit.fluxcd.io"
	fluxGitRepositoryKind = "GitRepository"
	defaultNamespace      = "default"
)

// fluxSourceRoots are the folders a Flux Kustomization's spec.path resolves
// against: the Git top level for a GitRepository source, and the pushed
// artifact root (the configured source directory) for any other source.
type fluxSourceRoots struct {
	git      string
	artifact string
}

// of returns the source root kustomization reads: the Git top level for a
// GitRepository source, the artifact root for any other source.
func (r fluxSourceRoots) of(kustomization *kustomizev1.Kustomization) string {
	if kustomization.Spec.SourceRef.Kind == fluxGitRepositoryKind {
		return r.git
	}

	return r.artifact
}

// resolveFluxSourceRoots finds the source roots for the validated tree. A
// missing Git top level or source directory falls back to the tree itself.
func resolveFluxSourceRoots(tree, sourceDir string) fluxSourceRoots {
	roots := fluxSourceRoots{git: tree, artifact: tree}

	if sourceDir != "" {
		canonical, err := fsutil.EvalCanonicalPath(sourceDir)
		if err == nil {
			roots.artifact = canonical
		}
	}

	for dir := tree; ; dir = filepath.Dir(dir) {
		_, err := os.Stat(filepath.Join(dir, ".git"))
		if err == nil {
			roots.git = dir

			break
		}

		if filepath.Dir(dir) == dir {
			break
		}
	}

	return roots
}

// fluxCompositionFields are the Flux Kustomization spec fields Flux's own
// generator merges into a folder's kustomization before it is built. Two
// Kustomizations that agree on all of them, and on the identity, apply a folder
// the same way.
var fluxCompositionFields = []string{ //nolint:gochecknoglobals // fixed list of Flux API fields
	"targetNamespace", "namePrefix", "nameSuffix",
	"patches", "patchesStrategicMerge", "patchesJson6902",
	"images", "components", "ignoreMissingComponents", "buildMetadata",
	// commonMetadata is not read by the generator: kustomize-controller sets it
	// on every applied object after the build (see withCommonMetadata).
	"commonMetadata",
}

// fluxModelledFields are every Flux Kustomization spec field validation either
// reproduces or knows cannot change what is admitted. Any other field fails the
// binding by name (ErrFluxFieldNotModelled) instead of being approximated:
//   - fluxCompositionFields change the built output and are reproduced;
//   - sourceRef, path and serviceAccountName choose what is built and as whom;
//   - postBuild substitution is resolved with the same typed placeholders as
//     every other folder, since substituteFrom values live in the cluster;
//   - decryption only changes Secret values, which stay encrypted offline;
//   - the rest schedule, order, wait for or prune the apply and leave the
//     applied objects unchanged.
//
// kubeConfig is deliberately absent: it applies to another cluster.
var fluxModelledFields = map[string]struct{}{ //nolint:gochecknoglobals // fixed list of Flux API fields
	"sourceRef":          {},
	"path":               {},
	"serviceAccountName": {},
	"postBuild":          {},
	"decryption":         {},
	"interval":           {},
	"retryInterval":      {},
	"timeout":            {},
	"suspend":            {},
	"dependsOn":          {},
	"prune":              {},
	"deletionPolicy":     {},
	"force":              {},
	"wait":               {},
	"healthChecks":       {},
	"healthCheckExprs":   {},
}

// unmodelledFluxFields returns the spec fields of object validation does not
// reproduce, sorted.
func unmodelledFluxFields(object map[string]any) []string {
	spec, _ := object["spec"].(map[string]any)

	var fields []string

	for field := range spec {
		if _, modelled := fluxModelledFields[field]; modelled {
			continue
		}

		if slices.Contains(fluxCompositionFields, field) {
			continue
		}

		fields = append(fields, field)
	}

	slices.Sort(fields)

	return fields
}

// maxFluxBindingRounds bounds how many times bindings are re-discovered from
// composed output. Each round reaches one Kustomization level deeper, so only a
// chain that keeps changing, such as a parent patched by its own child, hits it.
const maxFluxBindingRounds = 32

// fluxBinding is how Flux admits one folder: the identity it applies as and
// the Kustomization whose composition it applies first.
type fluxBinding struct {
	// root is the source the Kustomization reads; the build is confined to it.
	root     string
	identity kyvernopolicy.Identity
	// object is the Flux Kustomization as rendered, which Flux's generator reads.
	object map[string]any
	// composition holds the object's fluxCompositionFields that are set.
	composition map[string]any
	// generated is true when the folder has no kustomization file, so Flux
	// generates one listing its manifests.
	generated bool
	owners    []string

	// The folder's composed output, rendered once and reused by discovery and
	// evaluation.
	rendered    bool
	data        []byte
	attribution map[string]string
	renderErr   error
}

// composed reports whether Flux changes the folder's output before applying it.
func (b *fluxBinding) composed() bool {
	return b.generated || len(b.composition) > 0
}

// source names the folder and the Flux Kustomizations that apply it.
func (b *fluxBinding) source(dir string) string {
	return fmt.Sprintf(
		"%s (applied by Flux Kustomization %s as %s)",
		dir, strings.Join(b.owners, ", "), b.identity.Username,
	)
}

// sameAs reports whether other applies a folder exactly as b does.
func (b *fluxBinding) sameAs(other *fluxBinding) bool {
	// The root bounds what the build may read, so the same folder built from a
	// Git top level and from an artifact root is not the same application.
	return b.root == other.root &&
		b.identity.Username == other.identity.Username &&
		reflect.DeepEqual(b.composition, other.composition)
}

// composeFunc renders dir as binding composes it.
type composeFunc func(dir string, binding *fluxBinding) ([]byte, map[string]string, error)

// render returns the folder's composed output, rendering it on first use.
func (b *fluxBinding) render(dir string, compose composeFunc) ([]byte, map[string]string, error) {
	if !b.rendered {
		b.data, b.attribution, b.renderErr = compose(dir, b)
		b.rendered = true
	}

	return b.data, b.attribution, b.renderErr
}

// fluxBindingPlan decides which folders Kyverno admission evaluates and how.
// When active is false the tree renders no Flux Kustomization from its own
// source, and every folder is evaluated as before.
type fluxBindingPlan struct {
	active   bool
	bindings map[string]*fluxBinding
	// entries are the folders no other folder includes; an entry without a
	// binding is evaluated with no identity and its failures are named unbound.
	entries map[string]struct{}
	// skipped are bound folders that are not evaluated because their binding
	// already failed with a named diagnostic.
	skipped map[string]struct{}
}

// target reports whether Kyverno admission evaluates dir under plan.
func (p *fluxBindingPlan) target(dir string) bool {
	if !p.active {
		return true
	}

	if _, skipped := p.skipped[dir]; skipped {
		return false
	}

	if _, bound := p.bindings[dir]; bound {
		return true
	}

	_, entry := p.entries[dir]

	return entry
}

// admits reports whether dir's own output reaches the cluster under plan: every
// folder without Flux bindings, and otherwise a bound folder, an entry, or a
// folder whose binding failed (it is still applied; its evaluation reports why).
func (p *fluxBindingPlan) admits(dir string) bool {
	if _, skipped := p.skipped[dir]; skipped {
		return true
	}

	return p.target(dir)
}

// generatedTargets are the bound folders without a kustomization file: they
// have no validated output of their own and are evaluated only as Flux
// generates them.
func (p *fluxBindingPlan) generatedTargets() []string {
	var dirs []string

	for dir, binding := range p.bindings {
		if binding.generated {
			dirs = append(dirs, dir)
		}
	}

	slices.Sort(dirs)

	return dirs
}

// planFluxBindings binds each folder a Flux Kustomization from the tree's own
// source applies. Kustomizations are read from the entry folders' output
// (folders no other folder includes, so every rendered document is seen once)
// and, for a bound folder, from its output as Flux composes it: a parent's
// targetNamespace or patches can change a child Kustomization's namespace,
// path or serviceAccountName. Discovery repeats until the bindings stop
// changing. Kustomizations that read another source apply another tree and are
// left out. It returns the plan and the named diagnostics joined into one error.
func planFluxBindings(
	tree string,
	roots fluxSourceRoots,
	kustomizations []string,
	outputs map[string][]byte,
	compose composeFunc,
) (fluxBindingPlan, error) {
	var previous map[string]*fluxBinding

	for range maxFluxBindingRounds {
		plan, err := discoverFluxBindings(tree, roots, kustomizations, outputs, previous, compose)
		if previous != nil && sameBindings(previous, plan.bindings) {
			// Keep the bindings whose composed output is already rendered.
			plan.bindings = previous

			return plan, err
		}

		previous = plan.bindings
	}

	return fluxBindingPlan{active: true}, fmt.Errorf(
		"%w: they still change after %d rounds of composing parents",
		ErrFluxBindingsDoNotSettle, maxFluxBindingRounds,
	)
}

// discoverFluxBindings is one discovery round. Entries are read, and so is
// every folder previous binds with a composition or a generated kustomization,
// as previous composes it: a parent's patches reach the Kustomizations the
// folder renders even when an entry also includes the folder. Any other folder
// is read as rendered. A folder that an entry includes and a Flux Kustomization
// also composes is applied twice, so a child Kustomization the two copies
// disagree on is reported as ambiguous rather than bound either way.
func discoverFluxBindings(
	tree string,
	roots fluxSourceRoots,
	kustomizations []string,
	outputs map[string][]byte,
	previous map[string]*fluxBinding,
	compose composeFunc,
) (fluxBindingPlan, error) {
	plan := fluxBindingPlan{
		bindings: map[string]*fluxBinding{},
		entries:  entryFolders(tree, kustomizations),
		skipped:  map[string]struct{}{},
	}

	scan := maps.Clone(plan.entries)

	for dir, binding := range previous {
		if binding.composed() {
			scan[dir] = struct{}{}
		}
	}

	var errs []error

	for _, dir := range slices.Sorted(maps.Keys(scan)) {
		data := outputs[dir]

		if binding, bound := previous[dir]; bound && binding.composed() {
			composed, _, err := binding.render(dir, compose)
			if err != nil {
				errs = append(errs, err)

				continue
			}

			data = composed
		}

		errs = append(errs, plan.bindDocuments(tree, roots, kustomizations, dir, data))
	}

	return plan, errors.Join(errs...)
}

// bindDocuments binds every Flux Kustomization from the tree's own source that
// data, the output of dir, renders.
func (p *fluxBindingPlan) bindDocuments(
	tree string,
	roots fluxSourceRoots,
	kustomizations []string,
	dir string,
	data []byte,
) error {
	var errs []error

	for _, doc := range decodeDocuments(data) {
		kustomization, isFlux, err := decodeFluxKustomization(doc)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", dir, err))

			continue
		}

		if !isFlux || !fromOwnSource(kustomization) {
			continue
		}

		p.active = true

		errs = append(errs, p.bind(tree, roots, kustomizations, kustomization, doc))
	}

	return errors.Join(errs...)
}

// sameBindings reports whether two rounds bound the same folders the same way.
func sameBindings(previous, current map[string]*fluxBinding) bool {
	if len(previous) != len(current) {
		return false
	}

	for dir, binding := range current {
		before, found := previous[dir]
		if !found || !before.sameAs(binding) || before.generated != binding.generated ||
			!slices.Equal(before.owners, binding.owners) {
			return false
		}
	}

	return true
}

// bind records the folder kustomization applies, or the named diagnostic that
// stops it from being bound.
func (p *fluxBindingPlan) bind(
	tree string,
	roots fluxSourceRoots,
	kustomizations []string,
	kustomization *kustomizev1.Kustomization,
	object map[string]any,
) error {
	owner := kustomization.Namespace + "/" + kustomization.Name

	dir, rel, err := resolveBindingDir(tree, roots, kustomization, owner)
	if err != nil {
		return err
	}

	if fields := unmodelledFluxFields(object); len(fields) > 0 {
		delete(p.bindings, dir)
		p.skipped[dir] = struct{}{}

		return fmt.Errorf(
			"%w: Flux Kustomization %s sets spec.%s for %s; validation does not reproduce it",
			ErrFluxFieldNotModelled, owner, strings.Join(fields, ", spec."), rel,
		)
	}

	binding := &fluxBinding{
		root:        roots.of(kustomization),
		identity:    fluxIdentity(kustomization),
		object:      object,
		composition: fluxComposition(object),
		generated:   !slices.Contains(kustomizations, dir),
		owners:      []string{owner},
	}

	existing, found := p.bindings[dir]
	if !found {
		if _, skipped := p.skipped[dir]; !skipped {
			p.bindings[dir] = binding
		}

		return nil
	}

	if existing.sameAs(binding) {
		if !slices.Contains(existing.owners, owner) {
			existing.owners = append(existing.owners, owner)
			slices.Sort(existing.owners)
		}

		return nil
	}

	delete(p.bindings, dir)
	p.skipped[dir] = struct{}{}

	return fmt.Errorf(
		"%w: %s is applied by Flux Kustomization %s as %s and by %s as %s, "+
			"with a different identity or composition (targetNamespace, patches, images, ...)",
		ErrAmbiguousFluxBinding, rel,
		strings.Join(existing.owners, ", "), existing.identity.Username,
		owner, binding.identity.Username,
	)
}

// fluxComposition returns the fluxCompositionFields set on object's spec.
func fluxComposition(object map[string]any) map[string]any {
	composition := map[string]any{}

	spec, _ := object["spec"].(map[string]any)
	for _, field := range fluxCompositionFields {
		if value, set := spec[field]; set && value != nil {
			if list, isList := value.([]any); isList && len(list) == 0 {
				// Flux appends nothing for an empty list, so it does not change the folder.
				continue
			}

			if flag, isBool := value.(bool); isBool && !flag {
				// An unset flag is false (ignoreMissingComponents), so false does not change the folder.
				continue
			}

			composition[field] = value
		}
	}

	return composition
}

// resolveBindingDir resolves kustomization's spec.path against its source root
// and returns the folder and its path relative to tree, or the named diagnostic
// for a path outside tree or a folder that does not exist.
func resolveBindingDir(
	tree string,
	roots fluxSourceRoots,
	kustomization *kustomizev1.Kustomization,
	owner string,
) (string, string, error) {
	dir := filepath.Join(roots.of(kustomization), filepath.FromSlash(kustomization.Spec.Path))

	canonical, err := fsutil.EvalCanonicalPath(dir)
	if err == nil {
		dir = canonical
	}

	rel, err := filepath.Rel(tree, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf(
			"%w: Flux Kustomization %s path %q resolves to %s, outside %s",
			ErrFluxBindingOutsideTree, owner, kustomization.Spec.Path, dir, tree,
		)
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", "", fmt.Errorf(
			"%w: Flux Kustomization %s path %q (%s)",
			ErrFluxBindingPathNotFound, owner, kustomization.Spec.Path, rel,
		)
	}

	return dir, rel, nil
}

// decodeFluxKustomization decodes doc when it is a Flux Kustomization. ok is
// false for any other document.
func decodeFluxKustomization(doc map[string]any) (*kustomizev1.Kustomization, bool, error) {
	object := unstructured.Unstructured{Object: doc}
	if object.GetKind() != kustomizev1.KustomizationKind ||
		object.GroupVersionKind().Group != fluxKustomizeGroup {
		return nil, false, nil
	}

	var kustomization kustomizev1.Kustomization

	err := runtime.DefaultUnstructuredConverter.FromUnstructured(doc, &kustomization)
	if err != nil {
		return nil, false, fmt.Errorf(
			"decode Flux Kustomization %s/%s: %w", object.GetNamespace(), object.GetName(), err,
		)
	}

	if kustomization.Namespace == "" {
		kustomization.Namespace = defaultNamespace
	}

	return &kustomization, true, nil
}

// fromOwnSource reports whether kustomization reads the source that delivers
// the validated tree.
func fromOwnSource(kustomization *kustomizev1.Kustomization) bool {
	namespace := kustomization.Spec.SourceRef.Namespace
	if namespace == "" {
		namespace = kustomization.Namespace
	}

	return kustomization.Spec.SourceRef.Name == fluxSystemSource && namespace == fluxSystemNamespace
}

// fluxIdentity is who Flux applies kustomization's folder as: its
// serviceAccountName in its own namespace, which Flux impersonates, or the
// kustomize-controller's own service account when it names none.
func fluxIdentity(kustomization *kustomizev1.Kustomization) kyvernopolicy.Identity {
	if name := kustomization.Spec.ServiceAccountName; name != "" {
		return serviceAccountIdentity(kustomization.Namespace, name)
	}

	return serviceAccountIdentity(fluxSystemNamespace, fluxControllerAccount)
}

// serviceAccountIdentity is the requester Kubernetes authenticates a service
// account token as.
func serviceAccountIdentity(namespace, name string) kyvernopolicy.Identity {
	return kyvernopolicy.Identity{
		Username: "system:serviceaccount:" + namespace + ":" + name,
		Groups: []string{
			"system:serviceaccounts",
			"system:serviceaccounts:" + namespace,
			"system:authenticated",
		},
	}
}

// entryFolders returns the kustomization folders no other kustomization in the
// tree includes through resources, components or bases.
func entryFolders(tree string, kustomizations []string) map[string]struct{} {
	included := map[string]struct{}{}

	for _, dir := range kustomizations {
		kust, ok := readKustomization(tree, dir)
		if !ok {
			continue
		}

		//nolint:staticcheck // SA1019: Bases is deprecated; kept to support legacy kustomization files
		for _, ref := range slices.Concat(kust.Resources, kust.Components, kust.Bases) {
			referenced := filepath.Join(dir, ref)

			info, err := os.Stat(referenced)
			if err == nil && info.IsDir() {
				included[referenced] = struct{}{}
			}
		}
	}

	entries := map[string]struct{}{}

	for _, dir := range kustomizations {
		if _, found := included[dir]; !found {
			entries[dir] = struct{}{}
		}
	}

	return entries
}

// readKustomization reads and parses the kustomization file in kustDir, trying
// every recognized name. ok is false when none can be read or parsed.
func readKustomization(rootDir, kustDir string) (*kustomizeTypes.Kustomization, bool) {
	for _, name := range kustomizationFileNames {
		data, err := fsutil.ReadFileSafe(rootDir, filepath.Join(kustDir, name))
		if err != nil {
			continue
		}

		var kust kustomizeTypes.Kustomization

		err = kust.Unmarshal(data)
		if err != nil {
			return nil, false
		}

		return &kust, true
	}

	return nil, false
}

// composedManifests renders dir as binding composes it: the folder built the
// way Flux builds it (the Kustomization's composition merged into the folder's
// kustomization, or into one Flux generates), then Flux substitution and Helm
// rendering, in the order kustomize-controller and helm-controller apply them.
func (v *kustomizationValidator) composedManifests(
	ctx context.Context,
	dir string,
	binding *fluxBinding,
) ([]byte, map[string]string, error) {
	composed, err := v.kustomize.BuildFlux(ctx, binding.root, dir, binding.object)
	if err != nil {
		return nil, nil, fmt.Errorf("apply Flux Kustomization %s: %w",
			strings.Join(binding.owners, ", "), err)
	}

	built, err := withCommonMetadata(composed.Bytes(), binding.object)
	if err != nil {
		return nil, nil, fmt.Errorf("apply Flux Kustomization %s: %w",
			strings.Join(binding.owners, ", "), err)
	}

	if v.renderer == nil {
		expanded, err := expandValidationSubstitutions(ctx, built, v.opts)

		return expanded, nil, err
	}

	result, err := v.renderer.expandBuilt(ctx, built)

	return v.finishRendered(ctx, result, err)
}

// composer renders a bound folder as its Flux Kustomization composes it.
func (v *kustomizationValidator) composer(ctx context.Context) composeFunc {
	return func(dir string, binding *fluxBinding) ([]byte, map[string]string, error) {
		return v.composedManifests(ctx, dir, binding)
	}
}

// admittedOutputs returns what the cluster is asked to admit, the source of the
// Namespaces and cluster-wide policies every evaluation shares: each bound
// folder as Flux composes it (folders Flux generates a kustomization for
// included) and every other folder as rendered. Without Flux bindings it is the
// rendered output of every folder, as before. A folder whose composition fails
// keeps its rendered output here; its own evaluation reports the failure. A
// folder that is neither bound nor an entry reaches the cluster only through the
// folders that include it, whose outputs already carry it as composed, so its
// own raw output is left out: a base policy a composed overlay strengthens must
// not stand in for the composed one.
func (v *kustomizationValidator) admittedOutputs(
	ctx context.Context,
	plan fluxBindingPlan,
) [][]byte {
	dirs := slices.Sorted(maps.Keys(v.renderedOutputs))
	dirs = append(dirs, plan.generatedTargets()...)

	outputs := make([][]byte, 0, len(dirs))

	for _, dir := range dirs {
		if !plan.admits(dir) {
			continue
		}

		data := v.renderedOutputs[dir]

		if binding, bound := plan.bindings[dir]; bound && binding.composed() {
			composed, _, err := binding.render(dir, v.composer(ctx))
			if err == nil {
				data = composed
			}
		}

		outputs = append(outputs, data)
	}

	return outputs
}

// checkComposed runs the schema and --rules CEL checks on a bound folder's
// composed output, with the same kind exclusions and render attribution as the
// folder's own output.
func (v *kustomizationValidator) checkComposed(
	ctx context.Context,
	source string,
	data []byte,
	skipKinds []string,
	attribution map[string]string,
) error {
	opts := kubeconform.ValidationOptions{}
	if v.opts != nil {
		opts = *v.opts
	}

	opts.SkipKinds = skipKinds
	opts.Attribution = attribution

	err := v.kubeconform.ValidateBytes(ctx, source, data, &opts)
	if err != nil {
		return fmt.Errorf("validate composed manifests: %w", err)
	}

	return evaluateCELDocuments(v.engine, data, source, opts.SkipKinds, v.celSink, opts.Attribution)
}
