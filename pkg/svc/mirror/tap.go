package mirror

import (
	"errors"
	"fmt"
	"slices"
)

// ErrTargetNil is returned when SelectTapPoint is called with a nil Target.
var ErrTargetNil = errors.New("target is nil")

// ErrContainerNotFound is returned when the requested container name is not one
// of the Target's containers.
var ErrContainerNotFound = errors.New("container not found in target")

// ErrAmbiguousContainer is returned when a Target declares more than one
// container and the caller did not name which one the tap should attach to.
var ErrAmbiguousContainer = errors.New("target has multiple containers; specify one")

// TapPoint is the single resolved (pod, container) attachment point a mirror tap
// is injected into. It is produced by SelectTapPoint from a resolved Target and
// consumed by the later Phase 1 steps (ephemeral-container injection and the
// reverse tunnel back to the local process).
type TapPoint struct {
	// Namespace is the namespace the pod lives in.
	Namespace string
	// Pod is the name of the pod the tap attaches to.
	Pod string
	// Container is the name of the container within Pod whose traffic is mirrored.
	Container string
}

// SelectTapPoint resolves a Target to the concrete pod and container a tap
// attaches to. It is pure logic over an already-resolved Target, so — like
// ResolveTarget — it is fully unit-testable without a cluster and shared by every
// later Phase 1 step.
//
// The pod is the first Running pod of the Target (Target.Pods is in the order the
// API returned them); selecting a specific pod is a later slice. The container is
// the one named by container when it is non-empty (which must exist, else
// ErrContainerNotFound); otherwise the Target's sole container; otherwise
// ErrAmbiguousContainer, signalling the caller to pass --container.
//
// It returns ErrTargetNil for a nil target, and — defensively, for a hand-built
// Target that bypassed ResolveTarget's guarantees — ErrNoRunningPods or
// ErrDeploymentNoContainers when the respective list is empty.
func SelectTapPoint(target *Target, container string) (*TapPoint, error) {
	if target == nil {
		return nil, ErrTargetNil
	}

	namespace, deployment := target.Namespace, target.Deployment

	// ResolveTarget guarantees non-empty Pods and Containers, but a Target can be
	// constructed by hand, so guard rather than index into an empty slice.
	if len(target.Pods) == 0 {
		return nil, fmt.Errorf("%w: %q in %s", ErrNoRunningPods, deployment, namespace)
	}

	if len(target.Containers) == 0 {
		return nil, fmt.Errorf("%w: %q in %s", ErrDeploymentNoContainers, deployment, namespace)
	}

	selected, err := selectContainer(target.Containers, container)
	if err != nil {
		return nil, err
	}

	return &TapPoint{
		Namespace: target.Namespace,
		Pod:       target.Pods[0],
		Container: selected,
	}, nil
}

// selectContainer resolves which container name a tap attaches to: the requested
// one when non-empty (which must be present), the sole container when none is
// requested, or an ambiguity error when several exist and none was requested.
func selectContainer(containers []string, requested string) (string, error) {
	if requested != "" {
		if slices.Contains(containers, requested) {
			return requested, nil
		}

		return "", fmt.Errorf("%w: %q", ErrContainerNotFound, requested)
	}

	if len(containers) == 1 {
		return containers[0], nil
	}

	return "", fmt.Errorf("%w (have %v)", ErrAmbiguousContainer, containers)
}
