// Package mirror is the foundation for a native local-process ↔ cluster traffic
// bridge (`ksail workload mirror`), letting a developer run one service locally
// while it receives — and, later, intercepts — traffic destined for its
// Deployment in a running cluster. It is the "developer inner-loop compression"
// capability covered by tools like mirrord and Telepresence; see ksail#4521.
//
// # Design decision: native, in-process — not an external tool
//
// KSail implements this natively in Go, reusing the same in-process Kubernetes
// primitives the rest of the CLI already embeds (client-go via [pkg/k8s], and
// the embedded `k8s.io/kubectl/pkg/cmd/*` commands behind `workload forward`,
// `workload debug`, and `workload exec`). It does NOT shell out to, or bundle a
// daemon from, an external tool:
//
//   - mirrord (metalbear) is a Rust binary; integrating it means shelling out to
//     a foreign binary — against KSail's native-Go, in-process house rule
//     (the same rule that governs provisioning: native SDKs, never `curl | sh`
//     or shelled binaries).
//   - Telepresence (CNCF, Apache-2.0) and ktunnel are Go, but ship as a
//     privileged user daemon plus a cluster-wide traffic-manager — a heavyweight
//     external lifecycle to manage, not an in-process library.
//
// Reusing KSail's existing embedding pattern keeps the feature consistent with
// `workload forward`/`debug`/`exec`, avoids a new runtime dependency, and keeps
// all logic unit-testable against a fake clientset.
//
// # Phased delivery
//
// The capability is large (the issue marks it High-complexity), so it ships in
// independently-valuable phases:
//
//   - Phase 0 — foundation (this package): resolve a Deployment to the running
//     pods and containers a tap would attach to ([ResolveTarget]). Pure
//     client-go, fully unit-testable, needed by every later phase regardless of
//     the eventual tap mechanism.
//
//   - Phase 1 — mirror-only: inject a read-only "tap" sidecar (reusing the
//     ephemeral-container mechanism behind `workload debug`) that captures the
//     target's inbound traffic and streams it to the local process. Read-only:
//     the local process receives mirrored traffic but does not answer back into
//     the cluster — the lowest-risk first mode the issue itself suggests.
//     Increments so far: [SelectTapPoint] (pick the concrete pod and container),
//     [InjectTap]/[WaitForTap] (the ephemeral tap container), the capture
//     spec ([CaptureCommand] + the hardened NET_RAW-only security context),
//     and the capture session ([RunCaptureSession] streaming the pcap bytes
//     over the exec channel + [SummarizeCapture] reading them locally with
//     the pure-Go pcapgo reader).
//
//     Capture design (#4521): passive pcap via CAP_NET_RAW — tcpdump execed in
//     the tap container, pcap on stdout over the already-embedded exec channel.
//     A userspace proxy (socat or Go) in the pod netns cannot passively observe
//     traffic addressed to the app container; being in-path needs
//     NET_ADMIN/iptables, which is Phase 2 intercept by definition, and eBPF
//     costs node-level privilege for no Phase 1 gain. Because the exec channel
//     itself carries the stream, mirror mode needs NO reverse tunnel — the
//     tunnel returns in Phase 2, where responses must flow back into the
//     cluster.
//
//   - Phase 2 — intercept: steer a subset (or all) of the Deployment's traffic to
//     the local process and return its responses.
//
//   - Phase 3 — environment & volume projection: run the local process with the
//     target pod's env vars and mounted volumes/secrets for cluster-equivalent
//     context.
//
// Phase 1 targets the Vanilla, K3s, and VCluster distributions first (per the
// issue's acceptance criteria); Talos/Omni follow.
package mirror
