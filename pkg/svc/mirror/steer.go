package mirror

import (
	"context"
	"errors"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// SteerContainerName is the fixed name of the ephemeral steering container
// InjectSteer appends to the tap point's pod. One steering agent per pod: a
// second InjectSteer on the same pod returns ErrSteerAlreadyInjected.
//
// Steering is deliberately a SECOND container, never an upgrade of the tap:
// the tap's NET_RAW-only context is the security posture of mirror-only mode,
// so mirror and intercept stay independently runnable (#5839).
const SteerContainerName = "ksail-steer"

// DefaultSteerImage is the image the steering container runs when the caller
// does not override it with WithSteerImage. The same pinned netshoot release
// as the tap ([DefaultTapImage]) — it carries the iptables userspace tooling
// the steering agent drives.
const DefaultSteerImage = DefaultTapImage

// ErrSteerAlreadyInjected is returned when the tap point's pod already carries
// a steering container. Ephemeral containers cannot be removed or restarted,
// so the existing agent is the one to use (or the pod must be replaced).
var ErrSteerAlreadyInjected = errors.New("steering container already injected")

// ErrSteerTerminated is returned by WaitForSteer when the steering container
// terminated instead of reaching Running.
var ErrSteerTerminated = errors.New("steering container terminated")

// SteerOption customises the ephemeral steering container InjectSteer builds.
type SteerOption func(*ephemeralConfig)

// WithSteerImage overrides the steering container's image (default
// DefaultSteerImage).
func WithSteerImage(image string) SteerOption {
	return func(cfg *ephemeralConfig) { cfg.image = image }
}

// WithSteerCommand overrides the steering container's command. The default is
// an inert holder (`sleep infinity`) until the conn↔stream pump increment
// supplies the real steering agent process (#5839 increment 2).
func WithSteerCommand(command ...string) SteerOption {
	return func(cfg *ephemeralConfig) { cfg.command = command }
}

// steerKind describes the steering container for the shared ephemeral
// injection mechanics ([injectEphemeralContainer]/[waitForEphemeralContainer]).
func steerKind() ephemeralKind {
	return ephemeralKind{
		containerName:   SteerContainerName,
		label:           "steering",
		securityContext: steerSecurityContext(),
		alreadyInjected: ErrSteerAlreadyInjected,
		terminated:      ErrSteerTerminated,
	}
}

// InjectSteer appends the steering ephemeral container to the tap point's pod
// and returns the container's name. See [injectEphemeralContainer] for the
// injection semantics (idempotency, conflict convergence, process-namespace
// targeting). A pod already carrying a tap can still receive a steering agent
// and vice versa — the two flavours are independent by design.
//
// The container runs an inert holder command by default; the iptables rule
// application and the conn↔stream pump are the next intercept increments
// (#5839).
func InjectSteer(
	ctx context.Context,
	client kubernetes.Interface,
	point *TapPoint,
	opts ...SteerOption,
) (string, error) {
	cfg := ephemeralConfig{image: DefaultSteerImage, command: []string{"sleep", "infinity"}}
	for _, opt := range opts {
		opt(&cfg)
	}

	return injectEphemeralContainer(ctx, client, point, steerKind(), cfg)
}

// steerSecurityContext returns the hardened security context every steering
// container runs with: everything dropped except NET_ADMIN — the one
// capability writing iptables NAT rules in the pod's network namespace needs.
// The context is intentionally not caller-configurable: intercept's blast
// radius rests on the agent holding exactly the rule-writing privilege and
// nothing else (notably NOT the tap's NET_RAW).
func steerSecurityContext() *corev1.SecurityContext {
	return hardenedSecurityContext("NET_ADMIN")
}

// WaitForSteer blocks until the steering container on the tap point's pod
// reports Running, the container terminates (ErrSteerTerminated, with its exit
// code), or timeout elapses.
func WaitForSteer(
	ctx context.Context,
	client kubernetes.Interface,
	point *TapPoint,
	timeout time.Duration,
) error {
	return waitForEphemeralContainer(ctx, client, point, steerKind(), timeout)
}
