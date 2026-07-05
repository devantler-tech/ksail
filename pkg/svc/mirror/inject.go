package mirror

import (
	"context"
	"errors"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// TapContainerName is the fixed name of the ephemeral tap container InjectTap
// appends to the tap point's pod. One tap per pod: a second InjectTap on the
// same pod returns ErrTapAlreadyInjected.
const TapContainerName = "ksail-tap"

// DefaultTapImage is the image the tap container runs when the caller does not
// override it with WithTapImage. netshoot is the de-facto standard network
// debugging image and carries tcpdump, which the read-only capture increment
// execs to produce the mirror's pcap stream ([CaptureCommand]); it is pinned
// to a release tag so tap behaviour doesn't drift under a floating latest.
const DefaultTapImage = "docker.io/nicolaka/netshoot:v0.16"

// ErrTapAlreadyInjected is returned when the tap point's pod already carries a
// tap container. Ephemeral containers cannot be removed or restarted, so the
// existing tap is the one to use (or the pod must be replaced).
var ErrTapAlreadyInjected = errors.New("tap container already injected")

// ErrTapTerminated is returned by WaitForTap when the tap container terminated
// instead of reaching Running.
var ErrTapTerminated = errors.New("tap container terminated")

// TapOption customises the ephemeral tap container InjectTap builds.
type TapOption func(*ephemeralConfig)

// WithTapImage overrides the tap container's image (default DefaultTapImage).
func WithTapImage(image string) TapOption {
	return func(cfg *ephemeralConfig) { cfg.image = image }
}

// WithTapCommand overrides the tap container's command. The default is an inert
// holder (`sleep infinity`) until the later traffic increment supplies the real
// tap process.
func WithTapCommand(command ...string) TapOption {
	return func(cfg *ephemeralConfig) { cfg.command = command }
}

// tapKind describes the read-only tap container for the shared ephemeral
// injection mechanics ([injectEphemeralContainer]/[waitForEphemeralContainer]).
func tapKind() ephemeralKind {
	return ephemeralKind{
		containerName:   TapContainerName,
		label:           "tap",
		securityContext: tapSecurityContext(),
		alreadyInjected: ErrTapAlreadyInjected,
		terminated:      ErrTapTerminated,
	}
}

// InjectTap appends the read-only tap ephemeral container to the tap point's
// pod and returns the container's name. See [injectEphemeralContainer] for the
// injection semantics (idempotency, conflict convergence, process-namespace
// targeting).
//
// The container runs an inert holder command by default; wiring the actual
// traffic tap and the reverse tunnel are the next Phase 1 increments (#4521).
func InjectTap(
	ctx context.Context,
	client kubernetes.Interface,
	point *TapPoint,
	opts ...TapOption,
) (string, error) {
	cfg := ephemeralConfig{image: DefaultTapImage, command: []string{"sleep", "infinity"}}
	for _, opt := range opts {
		opt(&cfg)
	}

	return injectEphemeralContainer(ctx, client, point, tapKind(), cfg)
}

// tapSecurityContext returns the hardened security context every tap container
// runs with: everything dropped except NET_RAW — the one capability passive
// pcap capture ([CaptureCommand]) needs. The context is intentionally not
// caller-configurable: the read-only guarantee of mirror mode rests on the tap
// never holding more than capture privileges.
func tapSecurityContext() *corev1.SecurityContext {
	return hardenedSecurityContext("NET_RAW")
}

// WaitForTap blocks until the tap container on the tap point's pod reports
// Running, the container terminates (ErrTapTerminated, with its exit code), or
// timeout elapses.
func WaitForTap(
	ctx context.Context,
	client kubernetes.Interface,
	point *TapPoint,
	timeout time.Duration,
) error {
	return waitForEphemeralContainer(ctx, client, point, tapKind(), timeout)
}
