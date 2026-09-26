package ephemeral

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// MaxObservationWait bounds the explicitly requested reconciliation delay.
	MaxObservationWait  = 5 * time.Minute
	inventoryTimeout    = 2 * time.Minute
	inventoryPageSize   = 500
	maxInventoryObjects = 10000
)

// ErrObservation identifies incomplete or unverifiable operator-child coverage.
var ErrObservation = errors.New("operator-child observation incomplete")

// ObserveChildren waits, then inventories current descendants of the supplied
// workloads using server-assigned owner UIDs. It is a bounded snapshot, not a
// convergence guarantee. Roots must have been applied through this same client.
func (a *Applier) ObserveChildren(
	ctx context.Context,
	roots []*unstructured.Unstructured,
	delay time.Duration,
) ([]*unstructured.Unstructured, error) {
	if delay <= 0 || delay > MaxObservationWait {
		return nil, fmt.Errorf(
			"%w: observation wait must be positive and at most %s",
			ErrInput,
			MaxObservationWait,
		)
	}

	pinned, err := a.pinObservationRoots(roots)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, delay+inventoryTimeout)
	defer cancel()

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("observe children: %w", ctx.Err())
	case <-timer.C:
	}

	liveRoots, err := a.observationRoots(ctx, pinned)
	if err != nil {
		return nil, err
	}

	objects, err := a.inventory(ctx)
	if err != nil {
		return nil, err
	}

	_, err = a.observationRoots(ctx, pinned)
	if err != nil {
		return nil, err
	}

	children := descendants(liveRoots, objects)
	if len(children) == 0 {
		return nil, fmt.Errorf(
			"%w: no owned children observed; no child validation performed",
			ErrObservation,
		)
	}

	return children, nil
}

type observationRoot struct {
	object *unstructured.Unstructured
	uid    types.UID
}

func (a *Applier) pinObservationRoots(
	roots []*unstructured.Unstructured,
) ([]observationRoot, error) {
	if len(roots) == 0 {
		return nil, fmt.Errorf("%w: no directly submitted workloads to observe", ErrObservation)
	}

	a.appliedMu.Lock()
	defer a.appliedMu.Unlock()

	pinned := make([]observationRoot, 0, len(roots))
	for _, root := range roots {
		uid := a.applied[declaredIdentity(root)]
		if uid == "" {
			return nil, fmt.Errorf(
				"%w: no applied UID for %s",
				ErrObservation,
				resourceIdentity(root),
			)
		}

		pinned = append(pinned, observationRoot{object: root.DeepCopy(), uid: uid})
	}

	return pinned, nil
}

func (a *Applier) observationRoots(
	ctx context.Context,
	roots []observationRoot,
) ([]*unstructured.Unstructured, error) {
	live := make([]*unstructured.Unstructured, 0, len(roots))
	for _, pinned := range roots {
		root := pinned.object

		apiResource, err := a.resolve(ctx, root.GroupVersionKind())
		if err != nil {
			return nil, err
		}

		resource := a.client.Resource(
			root.GroupVersionKind().GroupVersion().WithResource(apiResource.Name),
		)

		namespace := ""
		if apiResource.Namespaced {
			namespace = root.GetNamespace()
			if namespace == "" {
				namespace = metav1.NamespaceDefault
			}
		}

		obj, err := resource.Namespace(namespace).Get(ctx, root.GetName(), metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("read observation root %s: %w", resourceIdentity(root), err)
		}

		if obj.GetUID() != pinned.uid {
			return nil, fmt.Errorf(
				"%w: observation root %s was replaced",
				ErrObservation,
				resourceIdentity(root),
			)
		}

		live = append(live, obj)
	}

	return live, nil
}

func (a *Applier) inventory(ctx context.Context) ([]*unstructured.Unstructured, error) {
	versions, err := a.inventoryVersions(ctx)
	if err != nil {
		return nil, err
	}

	var objects []*unstructured.Unstructured

	// A resource type served at several versions is listed once, at the first
	// version discovered, which is the group's preferred version when it has one.
	listedResources := map[schema.GroupResource]bool{}

	for _, version := range versions {
		path := "/apis/" + version.String()
		if version.Group == "" {
			path = "/api/" + version.Version
		}

		var resources metav1.APIResourceList

		err := a.discovery.Get().AbsPath(path).Do(ctx).Into(&resources)
		if err != nil {
			return nil, fmt.Errorf("discover child resources in %s: %w", version, err)
		}

		for _, resource := range resources.APIResources {
			if strings.Contains(resource.Name, "/") || !slices.Contains(resource.Verbs, "list") {
				continue
			}

			groupResource := version.WithResource(resource.Name).GroupResource()
			if listedResources[groupResource] {
				continue
			}

			listedResources[groupResource] = true

			listed, err := a.inventoryResource(
				ctx,
				version.WithResource(resource.Name),
				maxInventoryObjects-len(objects),
			)
			if err != nil {
				return nil, err
			}

			objects = append(objects, listed...)
		}
	}

	return objects, nil
}

func (a *Applier) inventoryVersions(ctx context.Context) ([]schema.GroupVersion, error) {
	var core metav1.APIVersions

	err := a.discovery.Get().AbsPath("/api").Do(ctx).Into(&core)
	if err != nil {
		return nil, fmt.Errorf("discover core child APIs: %w", err)
	}

	var groups metav1.APIGroupList

	err = a.discovery.Get().AbsPath("/apis").Do(ctx).Into(&groups)
	if err != nil {
		return nil, fmt.Errorf("discover child API groups: %w", err)
	}

	if !slices.Contains(core.Versions, "v1") {
		return nil, fmt.Errorf("%w: core v1 discovery is missing", ErrObservation)
	}

	versions := []schema.GroupVersion{{Version: "v1"}}

	for _, group := range groups.Groups {
		served, err := groupVersions(group)
		if err != nil {
			return nil, err
		}

		versions = append(versions, served...)
	}

	return versions, nil
}

// groupVersions returns every version a group serves, preferred first. A kind can be
// served only at a non-preferred version, so every served version is inventoried.
func groupVersions(group metav1.APIGroup) ([]schema.GroupVersion, error) {
	preferred, ok := parseServedVersion(group.Name, group.PreferredVersion.GroupVersion)
	if !ok {
		return nil, fmt.Errorf(
			"%w: invalid preferred API version for %s",
			ErrObservation,
			group.Name,
		)
	}

	versions := []schema.GroupVersion{preferred}

	for _, served := range group.Versions {
		other, ok := parseServedVersion(group.Name, served.GroupVersion)
		if !ok {
			return nil, fmt.Errorf(
				"%w: invalid served API version for %s",
				ErrObservation,
				group.Name,
			)
		}

		if other != preferred {
			versions = append(versions, other)
		}
	}

	return versions, nil
}

// parseServedVersion parses a discovered group version, rejecting one that names another
// group or no version.
func parseServedVersion(group, raw string) (schema.GroupVersion, bool) {
	version, err := schema.ParseGroupVersion(raw)
	if err != nil || version.Group != group || version.Version == "" {
		return schema.GroupVersion{}, false
	}

	return version, true
}

func (a *Applier) inventoryResource(
	ctx context.Context,
	gvr schema.GroupVersionResource,
	remaining int,
) ([]*unstructured.Unstructured, error) {
	var objects []*unstructured.Unstructured

	seenTokens := map[string]bool{}

	var token string
	for {
		list, err := a.client.Resource(gvr).
			List(ctx, metav1.ListOptions{Limit: inventoryPageSize, Continue: token})
		if err != nil {
			return nil, fmt.Errorf("list child inventory %s: %w", gvr, err)
		}

		if len(objects)+len(list.Items) > remaining {
			return nil, fmt.Errorf(
				"%w: inventory exceeds %d resources",
				ErrObservation,
				maxInventoryObjects,
			)
		}

		for _, obj := range list.Items {
			// Virtual unowned objects (for example ComponentStatus) have no
			// persistent identity and cannot participate in a rooted owner chain.
			// An owned object still requires a UID for safe transitive traversal.
			if obj.GetUID() == "" && len(obj.GetOwnerReferences()) > 0 {
				return nil, fmt.Errorf("%w: missing UID in %s", ErrObservation, gvr)
			}

			objects = append(objects, obj.DeepCopy())
		}

		token = list.GetContinue()
		if token == "" {
			break
		}

		if seenTokens[token] {
			return nil, fmt.Errorf("%w: repeated page token in %s", ErrObservation, gvr)
		}

		seenTokens[token] = true
	}

	return objects, nil
}

func descendants(roots, objects []*unstructured.Unstructured) []*unstructured.Unstructured {
	type edge struct {
		child *unstructured.Unstructured
		ref   metav1.OwnerReference
	}

	byOwner := make(map[types.UID][]edge)

	for _, obj := range objects {
		for _, ref := range obj.GetOwnerReferences() {
			byOwner[ref.UID] = append(byOwner[ref.UID], edge{child: obj, ref: ref})
		}
	}

	seen := make(map[types.UID]bool, len(roots))
	for _, root := range roots {
		seen[root.GetUID()] = true
	}

	var children []*unstructured.Unstructured

	queue := slices.Clone(roots)
	for index := 0; index < len(queue); index++ {
		owner := queue[index]
		for _, link := range byOwner[owner.GetUID()] {
			if seen[link.child.GetUID()] || !validOwner(link.child, owner, link.ref) {
				continue
			}

			seen[link.child.GetUID()] = true
			children = append(children, link.child)
			queue = append(queue, link.child)
		}
	}

	slices.SortFunc(children, func(a, b *unstructured.Unstructured) int {
		return strings.Compare(declaredIdentity(a), declaredIdentity(b))
	})

	return children
}

func validOwner(child, owner *unstructured.Unstructured, ref metav1.OwnerReference) bool {
	version, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil || version.Group != owner.GroupVersionKind().Group ||
		ref.Kind != owner.GetKind() ||
		ref.Name != owner.GetName() {
		return false
	}

	return owner.GetNamespace() == "" ||
		(child.GetNamespace() != "" && child.GetNamespace() == owner.GetNamespace())
}
