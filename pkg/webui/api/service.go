package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

// Sentinel errors a ClusterService implementation may return. clientErrorStatus maps them to HTTP
// status codes so backends that do not surface Kubernetes apierrors (e.g. the local CLI backend)
// can still drive the correct responses.
var (
	// ErrNotFound indicates the requested cluster does not exist (HTTP 404).
	ErrNotFound = errors.New("cluster not found")
	// ErrAlreadyExists indicates a cluster with the requested name already exists (HTTP 409).
	ErrAlreadyExists = errors.New("cluster already exists")
	// ErrInvalid indicates the client supplied an invalid cluster definition (HTTP 422).
	ErrInvalid = errors.New("invalid cluster")
	// ErrNotSupported indicates the backend does not support the requested operation (HTTP 501).
	ErrNotSupported = errors.New("operation not supported")
	// ErrHostClusterProtected indicates the request targets the operator's self-registered host
	// cluster — the cluster the operator runs on — whose registration cannot be created, modified,
	// or deleted through the API (HTTP 403). Disable it via the operator's host-cluster option
	// instead.
	ErrHostClusterProtected = errors.New(
		"the host cluster registration cannot be modified through the API",
	)
)

// errClusterUpdateNotSupported is the 501 a backend without ClusterUpdater (the local `ksail open web`
// backend) returns from PUT /api/v1/clusters/{namespace}/{name}. It preserves the message detail the
// former local Update stub carried, wrapping ErrNotSupported so clientErrorStatus maps it to 501.
var errClusterUpdateNotSupported = fmt.Errorf(
	"%w: updating clusters is not supported locally",
	ErrNotSupported,
)

// ClusterService is the backend the REST handlers delegate to. It is expressed in the
// Kubernetes-shaped wire types the web UI already consumes (v1alpha1.Cluster / v1alpha1.ClusterList)
// so a single HTTP layer can be served by two implementations: the operator's controller-runtime
// backend (pkg/operator's CR-backed service, via NewCRClusterService) which CRUDs Cluster custom
// resources, and the CLI's local backend (pkg/cli/clusterapi) which drives the provider/provisioner
// lifecycle for `ksail open web`.
//
// Implementations may return the sentinel errors below; clientErrorStatus maps them (and Kubernetes
// apierrors) to HTTP status codes. Returning any other error yields HTTP 500.
type ClusterService interface {
	// List returns all clusters. Items must be non-nil (empty slice, not nil) so the JSON encodes
	// as [] rather than null, matching Kubernetes list semantics.
	List(ctx context.Context) (*v1alpha1.ClusterList, error)
	// Get returns a single cluster, or a not-found error.
	Get(ctx context.Context, namespace, name string) (*v1alpha1.Cluster, error)
	// Create provisions a new cluster from client-supplied input and returns the created object.
	Create(ctx context.Context, cluster *v1alpha1.Cluster) (*v1alpha1.Cluster, error)
	// Delete removes a cluster.
	Delete(ctx context.Context, namespace, name string) error
}

// ClusterUpdater is an optional interface a ClusterService may implement to apply spec changes to an
// existing cluster (PUT /api/v1/clusters/{namespace}/{name}). It mirrors the other optional
// capability interfaces (ResourceService, ApplyService, …): a backend that implements it advertises
// capabilities.clusterUpdate=true and the PUT route succeeds; a backend that does not (the local
// `ksail open web` backend, which manages cluster configuration via files and `ksail cluster update`)
// leaves the capability false and the PUT returns 501, so the SPA hides the edit affordance rather
// than offering an action that fails.
type ClusterUpdater interface {
	// Update applies the client-supplied spec to an existing cluster and returns the updated object.
	Update(
		ctx context.Context,
		namespace, name string,
		cluster *v1alpha1.Cluster,
	) (*v1alpha1.Cluster, error)
}

// ClusterLifecycleController is an optional interface a ClusterService may implement to start and stop
// an existing cluster's infrastructure without deleting it (POST .../start and .../stop). A backend
// that implements it advertises capabilities.clusterStartStop=true and the routes are registered; a
// backend that does not (the operator, whose clusters are reconciled, not power-cycled) leaves the
// capability false and the routes unregistered, so the SPA hides the start/stop affordances. The local
// `ksail open web`/desktop backend implements it over the provisioner's Start/Stop for Docker clusters.
type ClusterLifecycleController interface {
	// Start brings a stopped cluster's nodes back up.
	Start(ctx context.Context, namespace, name string) error
	// Stop powers a running cluster's nodes down without deleting it.
	Stop(ctx context.Context, namespace, name string) error
}

// ComponentInstaller is an optional marker interface a ClusterService may implement to advertise that
// it installs the cluster components (CNI, CSI, metrics-server, …) declared in the create form
// (capabilities.componentsInstall=true). The SPA gates the create form's component selectors on it so
// it does not offer options a backend silently drops. The operator backend installs components via its
// reconciler and implements it; the local `ksail open web` backend only provisions the cluster (it does not
// yet run the component pipeline), so it leaves the capability false until that reuse lands.
type ComponentInstaller interface {
	// InstallsComponents reports whether this backend installs declared cluster components. It exists
	// solely so the capability is interface-derived (and so cannot drift from the routes), mirroring the
	// marker style of the other optional interfaces.
	InstallsComponents() bool
}

// Capabilities reports which optional operations a backend supports, so the SPA can hide affordances
// a backend cannot fulfill instead of offering an action that fails. It is served on
// /api/v1/config under "capabilities". New capability flags are added here as the UI surface grows
// (e.g. workload reads, log streaming, exec), and each backend reports the subset it implements.
type Capabilities struct {
	// ClusterUpdate reports whether the backend can apply spec changes to an existing cluster
	// (PUT /api/v1/clusters/{namespace}/{name}) — true exactly when the serving ClusterService
	// implements ClusterUpdater. The operator patches the Cluster custom resource and supports it; the
	// local CLI backend manages cluster configuration via files and does not, so the SPA hides the edit
	// affordance there rather than offering an action that returns 501. Derived from the interface in
	// handleConfig like the other flags, so it cannot drift from whether the PUT actually succeeds.
	ClusterUpdate bool `json:"clusterUpdate"`
	// WorkloadRead reports whether the backend can read live Kubernetes resources from a target
	// cluster (the read-only workload browser). It is true exactly when the serving ClusterService
	// implements ResourceService; the SPA shows the Resources view only then. Derived from the
	// interface in handleConfig rather than reported via CapabilityReporter, so it cannot drift from
	// whether the endpoints are actually registered.
	WorkloadRead bool `json:"workloadRead"`
	// WorkloadWrite reports whether the backend exposes the safe write actions (scale, rollout
	// restart, delete) on browsable resources — true exactly when the serving ClusterService
	// implements ResourceWriter. The SPA still combines it with !readOnly before showing the actions.
	WorkloadWrite bool `json:"workloadWrite"`
	// KubeconfigDownload reports whether the backend can export a portable kubeconfig for a cluster —
	// true exactly when the serving ClusterService implements KubeconfigProvider. Credential-bearing
	// kubeconfig exports must only be implemented by backends with an authentication boundary.
	KubeconfigDownload bool `json:"kubeconfigDownload"`
	// ApplyManifests reports whether the backend can server-side-apply raw manifests to a cluster —
	// true exactly when the serving ClusterService implements ApplyService. Combined with !readOnly
	// before the SPA shows the apply affordance.
	ApplyManifests bool `json:"applyManifests"`
	// SecretsCipher reports whether the backend can encrypt/decrypt secrets with SOPS using the local
	// age keys — true exactly when the serving ClusterService implements CipherService. The SPA shows
	// the Secrets view only then (local backend only; the operator has no local keys).
	SecretsCipher bool `json:"secretsCipher"`
	// WorkloadLogs reports whether the backend can stream a pod container's logs (the in-browser log
	// viewer) — true exactly when the serving ClusterService implements LogService. Logs are
	// read-only, so the SPA shows the action regardless of readOnly (unlike the write actions).
	WorkloadLogs bool `json:"workloadLogs"`
	// WorkloadExec reports whether the backend can exec into a pod container (the in-browser
	// terminal) — true exactly when the serving ClusterService implements ExecService. The SPA
	// combines it with !readOnly before showing the terminal (exec can run arbitrary commands).
	WorkloadExec bool `json:"workloadExec"`
	// ClusterStartStop reports whether the backend can start/stop an existing cluster's infrastructure
	// (POST .../start and .../stop) — true exactly when the serving ClusterService implements
	// ClusterLifecycleController. The SPA shows the start/stop actions only then; the operator omits it
	// (its clusters are reconciled, not power-cycled). Combined with !readOnly before showing the
	// actions (they mutate infrastructure).
	ClusterStartStop bool `json:"clusterStartStop"`
	// ComponentsInstall reports whether the backend installs the cluster components (CNI, CSI, …) the
	// create form offers — true exactly when the serving ClusterService implements ComponentInstaller
	// and reports it. The SPA gates the create form's component selectors on it so it does not offer
	// options a backend silently drops. The operator installs components via its reconciler; the local
	// backend does not yet, so it leaves this false (the form then hides the component selectors).
	ComponentsInstall bool `json:"componentsInstall"`
	// Plugins reports whether the backend serves web-UI plugins (Headlamp-compatible JS bundles) for
	// the SPA to load — true exactly when the serving ClusterService implements PluginService. The SPA
	// loads installed plugins and shows the Plugins surface only then. The local `ksail ui`/desktop
	// backend serves plugins from a local directory; the operator leaves it false (in-cluster plugin
	// serving is deferred), so the routes are not registered and the Plugins surface stays hidden.
	Plugins bool `json:"plugins"`
	// AIChat reports whether the web UI's AI assistant can run — true when the serving ClusterService
	// implements ChatService AND reports it available (ChatAvailable), so the panel appears only when a
	// turn can actually run (e.g. GitHub Copilot is configured). The local `ksail ui`/desktop backend
	// powers it over Copilot; the operator leaves it false.
	AIChat bool `json:"aiChat"`
	// KubeProxy reports whether the backend proxies read-only requests to a cluster's kube-apiserver
	// (KubeProxy) — true exactly when the serving ClusterService implements it. It powers reads of
	// resource kinds beyond the curated allowlist (and the Headlamp-compatible plugins' ApiProxy data
	// layer). The local `ksail ui`/desktop backend implements it; the operator leaves it false.
	KubeProxy bool `json:"kubeProxy"`
	// PluginInstall reports whether the backend can install/uninstall web-UI plugins (PluginInstaller)
	// and is not read-only. The SPA shows the plugin install/uninstall surface only then.
	PluginInstall bool `json:"pluginInstall"`
	// AIChatWrite reports whether the AI assistant may perform write operations — true when AIChat is
	// available AND the deployment is not read-only. The assistant's write tools are gated behind a
	// per-action confirmation in the SPA; a read-only deployment rejects them server-side (the chat POST
	// is mutating), so the SPA gates the confirmation affordance on this flag. Always derived alongside
	// AIChat in handleConfig, so it cannot diverge from whether a write turn can actually run.
	AIChatWrite bool `json:"aiChatWrite"`
	// PluginCatalog reports whether the backend can browse a remote catalog of installable plugins
	// (PluginCatalog) — the local backend searches Artifact Hub for Headlamp plugins. The SPA shows the
	// catalog search box only then; each result installs via the existing install flow. Derived from the
	// interface in handleConfig like the other flags, so it cannot drift from whether the route exists.
	PluginCatalog bool `json:"pluginCatalog"`
	// KubeWatch reports whether the backend streams read-only kube-apiserver watches (KubeWatch) — true
	// exactly when the serving ClusterService implements it. It powers live incremental updates
	// (ADDED/MODIFIED/DELETED) for the plugin K8s data layer; when false the SPA falls back to polling.
	// The local `ksail open web`/desktop backend implements it; the operator leaves it false.
	KubeWatch bool `json:"kubeWatch"`
	// WSMultiplexer reports whether the backend serves the Headlamp WebSocket multiplexer endpoint
	// (/wsMultiplexer) — true exactly when the serving ClusterService implements KubeWatch (the same
	// apiserver-watch backing). It lets a Headlamp plugin's WebSocketManager multiplex many resource
	// watches over one socket; the plugin K8s data layer prefers it over the per-list SSE watch when
	// advertised, falling back to SSE then polling. The local `ksail open web`/desktop backend serves it;
	// the operator leaves it false.
	WSMultiplexer bool `json:"wsMultiplexer"`
}

// KubeconfigProvider is an optional interface a ClusterService may implement to export a portable,
// single-context kubeconfig for a cluster (so the SPA can offer a "Download kubeconfig" action). The
// returned bytes are a complete kubeconfig YAML scoped to just the named cluster's context.
type KubeconfigProvider interface {
	Kubeconfig(ctx context.Context, namespace, name string) ([]byte, error)
}
