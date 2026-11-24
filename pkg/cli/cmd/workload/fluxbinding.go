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

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/kyvernopolicy"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	fluxkustomize "github.com/fluxcd/pkg/apis/kustomize"
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

// fluxBinding is how Flux admits one folder: the identity it applies as and
// the composition it applies first.
type fluxBinding struct {
	identity        kyvernopolicy.Identity
	targetNamespace string
	patches         []fluxkustomize.Patch
	owners          []string
}

// composed reports whether Flux changes the folder's output before applying it.
func (b *fluxBinding) composed() bool {
	return b.targetNamespace != "" || len(b.patches) > 0
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
	if b.identity.Username != other.identity.Username ||
		b.targetNamespace != other.targetNamespace {
		return false
	}

	return slices.EqualFunc(b.patches, other.patches, func(mine, theirs fluxkustomize.Patch) bool {
		return mine.Patch == theirs.Patch && reflect.DeepEqual(mine.Target, theirs.Target)
	})
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
	skipped  map[string]struct{}
	warnings []string
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

// planFluxBindings reads the Flux Kustomizations the entry folders render
// (folders no other folder includes, so every rendered document is seen once)
// and binds each folder a Kustomization from the tree's own source applies.
// Kustomizations that read another source apply another tree and are left out.
// It returns the plan and the named binding diagnostics joined into one error.
func planFluxBindings(
	tree string,
	roots fluxSourceRoots,
	kustomizations []string,
	outputs map[string][]byte,
) (fluxBindingPlan, error) {
	plan := fluxBindingPlan{
		bindings: map[string]*fluxBinding{},
		entries:  entryFolders(tree, kustomizations),
		skipped:  map[string]struct{}{},
	}

	var errs []error

	for _, dir := range slices.Sorted(maps.Keys(plan.entries)) {
		for _, doc := range decodeDocuments(outputs[dir]) {
			kustomization, isFlux, err := decodeFluxKustomization(doc)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", dir, err))

				continue
			}

			if !isFlux || !fromOwnSource(kustomization) {
				continue
			}

			plan.active = true

			errs = append(errs, plan.bind(tree, roots, kustomizations, kustomization))
		}
	}

	return plan, errors.Join(errs...)
}

// bind records the folder kustomization applies, or the named diagnostic that
// stops it from being bound.
func (p *fluxBindingPlan) bind(
	tree string,
	roots fluxSourceRoots,
	kustomizations []string,
	kustomization *kustomizev1.Kustomization,
) error {
	owner := kustomization.Namespace + "/" + kustomization.Name

	dir, rel, err := resolveBindingDir(tree, roots, kustomization, owner)
	if err != nil {
		return err
	}

	if !slices.Contains(kustomizations, dir) {
		p.warnings = append(p.warnings, fmt.Sprintf(
			"Flux Kustomization %s applies %s, which has no kustomization file; "+
				"Flux generates one, so its documents are not evaluated for Kyverno admission",
			owner, rel,
		))

		return nil
	}

	binding := &fluxBinding{
		identity:        fluxIdentity(kustomization),
		targetNamespace: kustomization.Spec.TargetNamespace,
		patches:         kustomization.Spec.Patches,
		owners:          []string{owner},
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
		}

		return nil
	}

	delete(p.bindings, dir)
	p.skipped[dir] = struct{}{}

	return fmt.Errorf(
		"%w: %s is applied by Flux Kustomization %s as %s and by %s as %s, "+
			"with a different identity, targetNamespace or patches",
		ErrAmbiguousFluxBinding, rel,
		strings.Join(existing.owners, ", "), existing.identity.Username,
		owner, binding.identity.Username,
	)
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
	root := roots.artifact
	if kustomization.Spec.SourceRef.Kind == fluxGitRepositoryKind {
		root = roots.git
	}

	dir := filepath.Join(root, filepath.FromSlash(kustomization.Spec.Path))

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

// kustomizePatches converts Flux patches to the kustomize patches Flux applies.
func kustomizePatches(patches []fluxkustomize.Patch) []kustomizeTypes.Patch {
	converted := make([]kustomizeTypes.Patch, 0, len(patches))

	for _, patch := range patches {
		entry := kustomizeTypes.Patch{Patch: patch.Patch}

		if target := patch.Target; target != nil {
			selector := &kustomizeTypes.Selector{
				AnnotationSelector: target.AnnotationSelector,
				LabelSelector:      target.LabelSelector,
			}
			selector.Group = target.Group
			selector.Version = target.Version
			selector.Kind = target.Kind
			selector.Name = target.Name
			selector.Namespace = target.Namespace
			entry.Target = selector
		}

		converted = append(converted, entry)
	}

	return converted
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

// composedManifests renders dir as binding composes it: the kustomize build,
// then Flux's targetNamespace and patches, then Flux substitution and Helm
// rendering, in the order kustomize-controller and helm-controller apply them.
func (v *kustomizationValidator) composedManifests(
	ctx context.Context,
	dir string,
	binding *fluxBinding,
) ([]byte, map[string]string, error) {
	build := v.kustomize.Build
	if v.renderer != nil {
		build = v.renderer.build
	}

	output, err := build(ctx, dir)
	if err != nil {
		return nil, nil, err
	}

	composed, err := v.kustomize.Compose(
		ctx, output.Bytes(), binding.targetNamespace, kustomizePatches(binding.patches),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("apply Flux Kustomization %s: %w",
			strings.Join(binding.owners, ", "), err)
	}

	if v.renderer == nil {
		expanded, err := expandValidationSubstitutions(ctx, composed.Bytes(), v.opts)

		return expanded, nil, err
	}

	result, err := v.renderer.expandBuilt(ctx, composed.Bytes())
	if err != nil {
		return nil, nil, err
	}

	data, err := finishRenderedValidationSubstitutions(ctx, result, v.opts)

	return data, attributionFromDocuments(result.Documents), err
}
