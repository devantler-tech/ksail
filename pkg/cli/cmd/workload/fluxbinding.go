package workload

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

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
	// same folder as different identities.
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

// fluxBinding is the identity Flux applies one folder as, and the Flux
// Kustomizations that apply it.
type fluxBinding struct {
	identity kyvernopolicy.Identity
	owners   []string
}

// source names the folder and the Flux Kustomizations that apply it.
func (b *fluxBinding) source(dir string) string {
	return fmt.Sprintf(
		"%s (applied by Flux Kustomization %s as %s)",
		dir, strings.Join(b.owners, ", "), b.identity.Username,
	)
}

// fluxBindingPlan decides which folders Kyverno admission evaluates and as
// whom. When active is false the tree renders no Flux Kustomization from its
// own source, and every folder is evaluated as before.
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

// planFluxBindings binds each kustomization folder a Flux Kustomization from
// the tree's own source applies to the identity Flux applies it as. The Flux
// Kustomizations are read from the entry folders' rendered output: every
// kustomization folder is an entry or is included by one, so every rendered
// document is seen once. Each bound folder keeps its own build. A Flux
// Kustomization whose folder has no kustomization file is not bound, because
// that folder has no build of its own to check. Kustomizations that read
// another source apply another tree and are left out. It returns the plan and
// the named diagnostics joined into one error.
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

	errs := make([]error, 0, len(plan.entries))

	for _, dir := range slices.Sorted(maps.Keys(plan.entries)) {
		errs = append(errs, plan.bindDocuments(tree, roots, kustomizations, dir, outputs[dir]))
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

		errs = append(errs, p.bind(tree, roots, kustomizations, kustomization))
	}

	return errors.Join(errs...)
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
		// Flux generates a kustomization for this folder at apply time; it has
		// no build of its own here, so it is not checked, as before.
		return nil
	}

	binding := &fluxBinding{identity: fluxIdentity(kustomization), owners: []string{owner}}

	existing, found := p.bindings[dir]
	if !found {
		if _, skipped := p.skipped[dir]; !skipped {
			p.bindings[dir] = binding
		}

		return nil
	}

	if existing.identity.Username == binding.identity.Username {
		if !slices.Contains(existing.owners, owner) {
			existing.owners = append(existing.owners, owner)
			slices.Sort(existing.owners)
		}

		return nil
	}

	delete(p.bindings, dir)
	p.skipped[dir] = struct{}{}

	return fmt.Errorf(
		"%w: %s is applied by Flux Kustomization %s as %s and by %s as %s",
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
