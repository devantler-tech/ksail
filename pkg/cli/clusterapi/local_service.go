// Package clusterapi implements the operator REST API's ClusterService over the local KSail
// provider/provisioner lifecycle. It lets `ksail open web` serve the same web UI the operator
// serves, but backed by clusters on the local machine (Docker) instead of Cluster custom resources.
//
// Local create and delete are long-running, so they run asynchronously: a request returns
// immediately and an in-memory job store tracks the cluster's phase (Provisioning/Deleting/Failed)
// until the provisioner workflow completes. The web UI already polls the cluster list and renders
// status.phase, so this surfaces progress without any UI change.
package clusterapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// localNamespace is the synthetic namespace reported for local clusters. KSail has no namespace
// concept locally, but the web UI keys rows on namespace/name and builds delete paths from it, so a
// stable value is reported and the namespace path segment is otherwise ignored.
const localNamespace = "default"

// FactoryFunc builds a provisioner factory for a distribution. The name is used by distributions
// whose provisioner reads the cluster name from its config (VCluster, KWOK). It is injectable so
// tests can substitute mock provisioners.
type FactoryFunc func(distribution v1alpha1.Distribution, name string) (clusterprovisioner.Factory, error)

// job tracks an in-flight or recently finished create/delete operation for a single cluster.
type job struct {
	distribution v1alpha1.Distribution
	provider     v1alpha1.Provider
	phase        v1alpha1.ClusterPhase
	err          error
	// startedAt is when the operation began; it drives the status condition's transition time so the
	// UI can show how long a create/delete has been running or has been failed.
	startedAt time.Time
}

// message returns the human-readable detail for the job's current phase: the failure reason when the
// job failed, otherwise empty (the phase itself conveys an in-flight operation).
func (j *job) message() string {
	if j.err != nil {
		return j.err.Error()
	}

	return ""
}

// Ensure Service satisfies the operator REST API's backend contract. It deliberately does NOT
// implement api.ClusterUpdater: a local cluster's configuration is managed through its config files
// and `ksail cluster update`, not the API, so the SPA hides the edit affordance (capabilities.
// clusterUpdate=false) rather than offering a button that returns 501.
//
// It DOES implement api.ClusterLifecycleController (start/stop a Docker cluster without recreating it)
// and api.ComponentInstaller (reporting componentsInstall=false until the local create flow reuses the
// shared component pipeline), so the SPA gates those affordances on the advertised capabilities.
var (
	_ api.ClusterService             = (*Service)(nil)
	_ api.ClusterLifecycleController = (*Service)(nil)
	_ api.ComponentInstaller         = (*Service)(nil)
	_ api.PluginService              = (*Service)(nil)
	_ api.ChatService                = (*Service)(nil)
)

// Service implements api.ClusterService over the local provider/provisioner lifecycle.
type Service struct {
	// ResourceAdapter provides the ResourceService + ResourceWriter methods (list/get/scale/restart/
	// reconcile/delete) from the Service's ResourceClient method, so the workload-browser surface is
	// shared with the operator backend rather than reimplemented. Wired to point at the Service in
	// NewService.
	api.ResourceAdapter

	newFactory FactoryFunc

	// discoverer enumerates existing clusters across providers for List/Get; discoverProviders is
	// the set it queries. NewService queries every provider the machine can reach so cloud clusters
	// (Hetzner/Omni/EKS) are visible, not just local Docker ones.
	discoverer        *clusterdiscovery.Discoverer
	discoverProviders []v1alpha1.Provider

	// restConfigForCluster is the single kubeconfig seam: it resolves a *rest.Config for a named
	// cluster (honouring kubeconfigPath), and the four default client builders below derive from it. A
	// test that points it at one fake kubeconfig redirects every derived client. Injectable directly so
	// a test can also substitute a fully synthetic rest.Config.
	restConfigForCluster restConfigForClusterFunc

	// newDynamicClient builds a dynamic client for a named cluster (the read-only resource browser).
	// Injectable so tests can substitute a fake client; the default derives from restConfigForCluster.
	newDynamicClient dynamicClientFunc

	// newApplyClient builds a dynamic client + REST mapper for a named cluster (manifest apply, which
	// needs GVK→resource resolution). Injectable for tests; the default derives from restConfigForCluster.
	newApplyClient applyClientFunc

	// newLogClient builds a clientset for a named cluster (pod log streaming). Injectable for tests;
	// the default derives from restConfigForCluster.
	newLogClient logClientFunc
	// newExecClient builds a clientset + rest.Config for a named cluster (pod exec). Injectable; the
	// default derives from restConfigForCluster.
	newExecClient execClientFunc

	// kubeconfigPath resolves the kubeconfig file every cluster client (and the resource browser /
	// kubeconfig export) reads from, via restConfigForCluster. Injectable so tests can point at a temp
	// kubeconfig instead of the user's real one.
	kubeconfigPath func() string

	// plugins serves web-UI plugins from a local directory (~/.ksail/plugins), satisfying
	// api.PluginService so `ksail ui` can load Headlamp-compatible extensions. Its dir seam is
	// injectable for tests.
	plugins pluginStore

	// pluginCatalog browses installable Headlamp plugins from Artifact Hub, satisfying api.PluginCatalog
	// so the SPA can search and install plugins. Its baseURL/httpClient seams are injectable for tests.
	pluginCatalog pluginCatalog

	// chat powers the web UI's AI assistant (api.ChatService). It is wired in by the `ksail ui` command
	// (UseChat) so the Copilot dependency stays out of the core service; nil means unavailable.
	chat chatRunner

	// cosign verifies plugin downloads against cosign/sigstore material (the strongest install
	// authenticity tier). It is wired in by the `ksail open web` command (UseCosignVerifier) so the heavy
	// sigstore-go dependency stays out of the core service and the desktop module; nil means cosign
	// verification is unavailable, so an install carrying cosign material is rejected.
	cosign cosignVerifier

	mu   sync.Mutex
	jobs map[string]*job
}

// NewService returns a local ClusterService backed by the real provisioner factory.
func NewService() *Service {
	service := &Service{
		newFactory:        defaultFactory,
		discoverProviders: clusterdiscovery.AllProviders(),
		kubeconfigPath:    k8s.DefaultKubeconfigPath,
		plugins:           pluginStore{dir: defaultPluginsDir},
		pluginCatalog:     defaultPluginCatalog(),
		jobs:              map[string]*job{},
	}
	service.discoverer = &clusterdiscovery.Discoverer{
		DockerFactory: service.dockerFactory,
		// The web UI renders per-cluster run-state, so opt into the Docker run-state probe.
		ProbeRunState: true,
	}
	service.ResourceAdapter = api.ResourceAdapter{Provider: service}
	service.useDefaultClients()

	return service
}

// CreatableDistributions returns the distributions the local UI can provision. It feeds the
// create-form options the SPA renders (config.distributions).
func CreatableDistributions() []string {
	distributions := creatableDistributions()
	out := make([]string, 0, len(distributions))

	for _, dist := range distributions {
		out = append(out, string(dist))
	}

	return out
}

// creatableDistributions is the set the UI can provision. It spans Docker-based and cloud
// distributions; the SPA further gates each one to providers that are actually available (Docker
// running, HCLOUD_TOKEN set, eksctl installed, …) via the config endpoint's provider status, so a
// cloud distribution only appears once its provider's credentials are configured.
func creatableDistributions() []v1alpha1.Distribution {
	return []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos,
		v1alpha1.DistributionVCluster,
		v1alpha1.DistributionKWOK,
		v1alpha1.DistributionEKS,
	}
}

// listEntry is the merged view of a cluster's distribution, provider, and phase while assembling
// the list response. For job-tracked clusters it also carries the operation's detail (failure reason)
// and start time so List can attach a status condition the UI surfaces.
type listEntry struct {
	distribution v1alpha1.Distribution
	provider     v1alpha1.Provider
	phase        v1alpha1.ClusterPhase
	message      string
	since        time.Time
	// runState is the discovered cluster's coarse running/stopped state (Docker only). A stopped
	// cluster is reported with no Ready phase so the web UI does not render it green; instead List
	// attaches a Ready=False/reason=Stopped condition. RunStateUnknown leaves the cluster Ready.
	runState clusterdiscovery.RunState
}

// List returns the union of clusters discovered across providers and clusters tracked in the job
// store (in-flight creates/deletes). Job state takes precedence so a cluster mid-create shows
// Provisioning and one mid-delete shows Deleting even before the provider reflects it.
func (s *Service) List(ctx context.Context) (*v1alpha1.ClusterList, error) {
	live := s.enumerate(ctx)

	merged := make(map[string]listEntry, len(live))
	for name, cluster := range live {
		merged[name] = listEntry{
			distribution: cluster.Distribution,
			provider:     cluster.Provider,
			phase:        discoveredPhase(cluster.RunState),
			runState:     cluster.RunState,
		}
	}

	s.mu.Lock()
	for name, current := range s.jobs {
		// A ready cluster now visible to the provider no longer needs a job entry.
		if current.phase == v1alpha1.ClusterPhaseReady {
			if _, ok := live[name]; ok {
				delete(s.jobs, name)

				continue
			}
		}

		merged[name] = listEntry{
			distribution: current.distribution,
			provider:     current.provider,
			phase:        current.phase,
			message:      current.message(),
			since:        current.startedAt,
		}
	}
	s.mu.Unlock()

	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}

	sort.Strings(names)

	// Load the kubeconfig once and share it: both the endpoint map and the unmanaged-cluster synthesis
	// read the same contexts, and List is polled frequently by the UI — a single read avoids parsing
	// the file twice per request and any TOCTOU skew between the two views.
	kubeconfig := s.loadKubeconfig()

	// Endpoints come from the kubeconfig's contexts (no cluster round-trips), so the UI can show a
	// real API server URL for clusters that have one instead of an empty status field.
	endpoints := clusterEndpoints(kubeconfig)

	items := managedClusterItems(names, merged, endpoints)

	// Surface kubeconfig contexts ksail did not provision as unmanaged clusters (marked, never hidden
	// and never shown as a normal managed cluster), so read/operate surfaces can see them within limits
	// while ksail-only operations are refused.
	items = append(items, unmanagedClusters(kubeconfig, merged)...)

	// Sort the merged managed+unmanaged list globally by name so the surface order stays alphabetically
	// stable (the managed block and the unmanaged block are each sorted, but appending one after the
	// other would otherwise leave the combined list unsorted). Names are unique across the two groups —
	// contextIsManaged excludes any context that matches a managed cluster — so the sort never collides.
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })

	return &v1alpha1.ClusterList{Items: items}, nil
}

// managedClusterItems builds the Cluster list for the ksail-managed clusters, in the given name order,
// attaching each cluster's kubeconfig endpoint and its status condition.
func managedClusterItems(
	names []string,
	merged map[string]listEntry,
	endpoints map[string]string,
) []v1alpha1.Cluster {
	items := make([]v1alpha1.Cluster, 0, len(names))

	for _, name := range names {
		entry := merged[name]
		cluster := newCluster(name, entry.distribution, entry.provider, entry.phase)
		cluster.Status.Endpoint = endpoints[name]

		condition, ok := listEntryCondition(entry)
		if ok {
			cluster.Status.Conditions = []metav1.Condition{condition}
		}

		items = append(items, cluster)
	}

	return items
}

// Get returns a single local cluster. The namespace is ignored (clusters are keyed by name).
func (s *Service) Get(
	ctx context.Context,
	_, name string,
) (*v1alpha1.Cluster, error) {
	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	for i := range list.Items {
		if list.Items[i].Name == name {
			return &list.Items[i], nil
		}
	}

	return nil, fmt.Errorf("%w: %q", api.ErrNotFound, name)
}

// isSafeClusterName reports whether name is valid for the local backend: non-empty and DNS-1123
// compliant per KSail's shared cluster-name rule (v1alpha1.ValidateClusterName — the same check
// `ksail project init` and the JSON schema enforce, so the web UI and CLI agree on valid names).
// DNS-1123 names are single path components, so this also blocks path traversal where the name
// becomes a filesystem path downstream (~/.kwok/clusters/<name>, Docker container names). The local
// service always needs an explicit name, so empty is rejected here even though the config loader
// treats an empty name as "use the default".
func isSafeClusterName(name string) bool {
	return name != "" && v1alpha1.ValidateClusterName(name) == nil
}

// Create starts provisioning a cluster and returns immediately with the cluster in the Provisioning
// phase. The actual provisioning runs in a background goroutine; List/Get reflect its progress.
func (s *Service) Create(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
) (*v1alpha1.Cluster, error) {
	name := cluster.Name
	if !isSafeClusterName(name) {
		return nil, fmt.Errorf("%w: invalid name %q", api.ErrInvalid, name)
	}

	distribution := cluster.Spec.Cluster.Distribution
	if distribution == "" {
		return nil, fmt.Errorf("%w: distribution is required", api.ErrInvalid)
	}

	if !isCreatable(distribution) {
		return nil, fmt.Errorf(
			"%w: distribution %q cannot be provisioned locally",
			api.ErrNotSupported,
			distribution,
		)
	}

	// Default an omitted provider to the distribution's natural one (Docker for local distributions,
	// AWS for EKS) and reject invalid (distribution, provider) combinations such as EKS+Docker before
	// any work is enqueued — otherwise the provisioner would silently provision the wrong backend
	// (e.g. AWS) while the job and returned cluster are labelled with the requested provider.
	provider := cluster.Spec.Cluster.Provider
	if provider == "" {
		provider = v1alpha1.DefaultProviderForDistribution(distribution)
	}

	err := provider.ValidateForDistribution(distribution)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", api.ErrInvalid, err)
	}

	// Persist the defaulted/validated provider so the background goroutine, the job record, and the
	// returned cluster all agree on the backend that will actually be provisioned.
	cluster.Spec.Cluster.Provider = provider

	live := s.enumerate(ctx)

	s.mu.Lock()

	_, inLive := live[name]
	_, inJobs := s.jobs[name]

	if inLive || inJobs {
		s.mu.Unlock()

		return nil, fmt.Errorf("%w: %q", api.ErrAlreadyExists, name)
	}

	s.jobs[name] = &job{
		distribution: distribution,
		provider:     provider,
		phase:        v1alpha1.ClusterPhaseProvisioning,
		startedAt:    time.Now(),
	}
	s.mu.Unlock()

	go s.runCreate(context.WithoutCancel(ctx), name, cluster.Spec)

	created := newCluster(name, distribution, provider, v1alpha1.ClusterPhaseProvisioning)

	return &created, nil
}

// Delete starts deleting a cluster and returns immediately, marking it Deleting. The deletion runs
// in a background goroutine.
func (s *Service) Delete(ctx context.Context, _, name string) error {
	spec, err := s.startJob(ctx, name, v1alpha1.ClusterPhaseDeleting)
	if err != nil {
		return err
	}

	go s.runDelete(context.WithoutCancel(ctx), name, spec)

	return nil
}

// Start brings a stopped cluster's nodes back up. It implements api.ClusterLifecycleController so the
// web UI can power a Docker-stopped cluster back on without recreating it. Like Create/Delete it runs
// asynchronously: the cluster is marked Updating and the provisioner's Start runs in the background.
func (s *Service) Start(ctx context.Context, _, name string) error {
	return s.runLifecycle(
		ctx,
		name,
		func(actionCtx context.Context, p clusterprovisioner.Provisioner) error {
			return p.Start(actionCtx, name)
		},
	)
}

// Stop powers a running cluster's nodes down without deleting it (api.ClusterLifecycleController). It
// runs asynchronously like Start: the cluster is marked Updating while the provisioner's Stop runs.
func (s *Service) Stop(ctx context.Context, _, name string) error {
	return s.runLifecycle(
		ctx,
		name,
		func(actionCtx context.Context, p clusterprovisioner.Provisioner) error {
			return p.Stop(actionCtx, name)
		},
	)
}

// InstallsComponents reports whether this backend installs the declared cluster components. The local
// backend only provisions the cluster today (it does not run the component pipeline that CLI create
// and the operator reconciler do), so it returns false — the web UI then hides the create form's
// component selectors rather than offering options this backend silently drops. It flips to true once
// the local create flow reuses the shared installer pipeline.
func (s *Service) InstallsComponents() bool {
	return false
}

// Availability reports per-provider availability across the providers this service discovers. The
// web UI uses it to gate create-form options to providers the machine can actually reach.
func (s *Service) Availability(ctx context.Context) []clusterdiscovery.Availability {
	return s.discoverer.Availability(ctx, s.discoverProviders)
}

// UseCredentials points discovery and availability at a credential resolver (e.g. one backed by the
// OS secure store and the Settings page), so listing and gating reflect Settings overrides, not just
// raw environment variables. With no resolver set, discovery resolves credentials from the
// environment under their default variable names.
func (s *Service) UseCredentials(resolver credentials.Resolver) {
	s.discoverer.Resolver = resolver
}

// useDefaultClients points the single restConfigForCluster seam at the real kubeconfig resolver and
// derives the four client builders from it, so every cluster client resolves through one path that
// honours kubeconfigPath.
func (s *Service) useDefaultClients() {
	s.restConfigForCluster = s.defaultRESTConfigForCluster
	s.newDynamicClient = s.defaultDynamicClient
	s.newApplyClient = s.defaultApplyClient
	s.newLogClient = s.defaultLogClient
	s.newExecClient = s.defaultExecClient
}

// resolveCluster finds the distribution and provider of an existing cluster, checking live
// providers first and then the job store (for clusters still being provisioned).
func (s *Service) resolveCluster(
	ctx context.Context,
	name string,
) (v1alpha1.Distribution, v1alpha1.Provider, bool) {
	live := s.enumerate(ctx)
	if cluster, ok := live[name]; ok {
		return cluster.Distribution, cluster.Provider, true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if current, ok := s.jobs[name]; ok {
		return current.distribution, current.provider, true
	}

	return "", "", false
}

// dockerFactory adapts the Service's provisioner factory to the discovery DockerFactory shape,
// reusing the same (injectable) factory the create/delete paths use. The cluster name is irrelevant
// for listing, so it is empty.
func (s *Service) dockerFactory(
	distribution v1alpha1.Distribution,
) (clusterprovisioner.Factory, error) {
	return s.newFactory(distribution, "")
}

// startJob resolves a cluster, records an in-flight job for it at the given phase, and returns the
// minimal Spec a background provisioner action needs (distribution + provider — provider options like
// server types are irrelevant for delete/start/stop). Returns api.ErrNotFound when the cluster is
// unknown. Shared by Delete and the Start/Stop lifecycle path so the resolve+register handshake lives
// in one place.
func (s *Service) startJob(
	ctx context.Context,
	name string,
	phase v1alpha1.ClusterPhase,
) (v1alpha1.Spec, error) {
	distribution, provider, ok := s.resolveCluster(ctx, name)
	if !ok {
		return v1alpha1.Spec{}, fmt.Errorf("%w: %q", api.ErrNotFound, name)
	}

	s.mu.Lock()
	if current, found := s.jobs[name]; found && jobIsInProgress(current.phase) {
		s.mu.Unlock()

		return v1alpha1.Spec{}, fmt.Errorf(
			"%w: operation already in progress for %q",
			api.ErrAlreadyExists,
			name,
		)
	}

	s.jobs[name] = &job{
		distribution: distribution,
		provider:     provider,
		phase:        phase,
		startedAt:    time.Now(),
	}
	s.mu.Unlock()

	return v1alpha1.Spec{
		Cluster: v1alpha1.ClusterSpec{Distribution: distribution, Provider: provider},
	}, nil
}

func jobIsInProgress(phase v1alpha1.ClusterPhase) bool {
	return phase == v1alpha1.ClusterPhaseProvisioning ||
		phase == v1alpha1.ClusterPhaseDeleting ||
		phase == v1alpha1.ClusterPhaseUpdating
}

// runLifecycle marks a resolved cluster Updating, then runs the supplied provisioner action (Start or
// Stop) in the background, clearing the job on success and recording the failure otherwise — the same
// async pattern Create/Delete use so the web UI's list reflects progress without a UI change.
func (s *Service) runLifecycle(
	ctx context.Context,
	name string,
	action func(context.Context, clusterprovisioner.Provisioner) error,
) error {
	spec, err := s.startJob(ctx, name, v1alpha1.ClusterPhaseUpdating)
	if err != nil {
		return err
	}

	go s.runLifecycleAction(context.WithoutCancel(ctx), name, spec, action)

	return nil
}

// runLifecycleAction runs a Start/Stop action against a freshly built provisioner and records the
// outcome: the job is cleared on success (the next discovery poll reflects the new run-state) and
// pinned Failed with the error otherwise, so the UI can surface why a start/stop did not take.
func (s *Service) runLifecycleAction(
	ctx context.Context,
	name string,
	spec v1alpha1.Spec,
	action func(context.Context, clusterprovisioner.Provisioner) error,
) {
	err := s.runProvisioner(ctx, name, spec, action)

	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.jobs[name]
	if current == nil {
		return
	}

	if err != nil {
		slog.Error("cluster lifecycle action failed",
			"cluster", name, "distribution", spec.Cluster.Distribution, "error", err)

		current.phase = v1alpha1.ClusterPhaseFailed
		current.err = err

		return
	}

	delete(s.jobs, name)
}

func (s *Service) runCreate(ctx context.Context, name string, spec v1alpha1.Spec) {
	err := s.runProvisioner(
		ctx,
		name,
		spec,
		func(actionCtx context.Context, p clusterprovisioner.Provisioner) error {
			return p.Create(actionCtx, name)
		},
	)
	if err == nil && spec.Cluster.Distribution == v1alpha1.DistributionEKS {
		err = state.SaveClusterSpec(name, &spec.Cluster)
		if err != nil {
			err = fmt.Errorf("persist local EKS cluster ownership state: %w", err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.jobs[name]
	if current == nil {
		return
	}

	if err != nil {
		slog.Error("cluster creation failed",
			"cluster", name, "distribution", spec.Cluster.Distribution, "error", err)

		current.phase = v1alpha1.ClusterPhaseFailed
		current.err = err

		return
	}

	current.phase = v1alpha1.ClusterPhaseReady
}

func (s *Service) runDelete(ctx context.Context, name string, spec v1alpha1.Spec) {
	err := s.runProvisioner(
		ctx,
		name,
		spec,
		func(actionCtx context.Context, p clusterprovisioner.Provisioner) error {
			return p.Delete(actionCtx, name)
		},
	)
	if spec.Cluster.Distribution == v1alpha1.DistributionEKS &&
		(err == nil || errors.Is(err, clustererr.ErrClusterNotFound)) {
		cleanupErr := state.DeleteClusterState(name)
		if cleanupErr != nil {
			err = fmt.Errorf("clean up local EKS cluster state after deletion: %w", cleanupErr)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Deleting must be idempotent. ErrClusterNotFound means there is nothing to delete — a failed
	// create that produced no cluster, a cluster removed out-of-band, or a race where it vanished
	// between enumeration and Delete. Treat it as success and clear the tracked entry; otherwise the
	// job stays pinned Failed and the UI row can never be dismissed (forcing an app restart or a
	// fallback to `ksail cluster delete --name`). Only genuine failures are surfaced as Failed.
	if err != nil && !errors.Is(err, clustererr.ErrClusterNotFound) {
		slog.Error("cluster deletion failed",
			"cluster", name, "distribution", spec.Cluster.Distribution, "error", err)

		current := s.jobs[name]
		if current != nil {
			current.phase = v1alpha1.ClusterPhaseFailed
			current.err = err
		}

		return
	}

	delete(s.jobs, name)
}

// runProvisioner builds a provisioner for the requested spec and runs the supplied action against
// it. The spec carries the provider (so Talos routes to Hetzner/Omni/Docker) and node counts.
func (s *Service) runProvisioner(
	ctx context.Context,
	name string,
	spec v1alpha1.Spec,
	action func(context.Context, clusterprovisioner.Provisioner) error,
) error {
	provisioner, err := s.buildProvisioner(ctx, spec, name)
	if err != nil {
		return err
	}

	// Pass the background (WithoutCancel) ctx so the action outlives the HTTP request
	// that triggered it — the action MUST use this ctx, not a captured request-scoped one.
	return action(ctx, provisioner)
}

// enumerate discovers existing clusters across every configured provider, keyed by name (the first
// provider to report a name wins). Per-provider failures are logged and skipped (best-effort) so a
// single unreachable provider never blanks the list; this matches `ksail cluster list`.
func (s *Service) enumerate(ctx context.Context) map[string]clusterdiscovery.Cluster {
	clusters, failures := s.discoverer.Discover(ctx, s.discoverProviders)

	for _, failure := range failures {
		slog.Warn("cluster discovery failed for provider",
			"provider", failure.Provider, "error", failure.Err)
	}

	found := make(map[string]clusterdiscovery.Cluster, len(clusters))
	for _, cluster := range clusters {
		if _, ok := found[cluster.Name]; !ok {
			found[cluster.Name] = cluster
		}
	}

	return found
}

func (s *Service) buildProvisioner(
	ctx context.Context,
	spec v1alpha1.Spec,
	name string,
) (clusterprovisioner.Provisioner, error) {
	distribution := spec.Cluster.Distribution

	factory, err := s.newFactory(distribution, name)
	if err != nil {
		return nil, err
	}

	// Pass the full requested spec so the factory routes to the right provider (e.g. Talos →
	// Hetzner/Omni/Docker) and honors node counts and provider options, not just the distribution.
	cluster := &v1alpha1.Cluster{Spec: spec}
	cluster.Name = name

	provisioner, _, err := factory.Create(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("create %s provisioner: %w", distribution, err)
	}

	return provisioner, nil
}

// newCluster builds the Kubernetes-shaped Cluster the web UI consumes from a discovered (or
// in-flight) cluster's name, distribution, provider, and phase. An empty provider defaults to
// Docker (the local default), so older job entries and Docker clusters render correctly.
func newCluster(
	name string,
	distribution v1alpha1.Distribution,
	provider v1alpha1.Provider,
	phase v1alpha1.ClusterPhase,
) v1alpha1.Cluster {
	if provider == "" {
		provider = v1alpha1.ProviderDocker
	}

	cluster := v1alpha1.Cluster{}
	cluster.Name = name
	cluster.Namespace = localNamespace
	cluster.Spec.Cluster.Distribution = distribution
	cluster.Spec.Cluster.Provider = provider
	cluster.Status.Phase = phase

	return cluster
}

// discoveredPhase maps a discovered cluster's run-state to the phase the list reports. A running (or
// run-state-unknown, e.g. cloud) cluster is Ready; a stopped Docker cluster reports the
// ClusterPhaseStopped phase so the web UI renders it distinctly rather than green. List also attaches
// a Ready=False/reason=Stopped condition for consumers predating the Stopped phase value.
func discoveredPhase(runState clusterdiscovery.RunState) v1alpha1.ClusterPhase {
	if runState == clusterdiscovery.RunStateStopped {
		return v1alpha1.ClusterPhaseStopped
	}

	return v1alpha1.ClusterPhaseReady
}

// listEntryCondition returns the single status condition a list entry carries, if any: the Stopped
// condition for a discovered-but-stopped cluster, otherwise the in-flight/failed job condition.
func listEntryCondition(entry listEntry) (metav1.Condition, bool) {
	if entry.runState == clusterdiscovery.RunStateStopped {
		return stoppedCondition(), true
	}

	return jobConditionFor(entry.phase, entry.message, entry.since)
}

// stoppedCondition is the Ready=False/reason=Stopped condition a discovered Docker cluster carries
// when its containers exist but are not running. The conventional Ready type lets the existing UI
// render it, and the reason lets the SPA present a "Stopped" state without a new ClusterPhase value.
func stoppedCondition() metav1.Condition {
	return metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionFalse,
		Reason:  "Stopped",
		Message: "Cluster is stopped",
	}
}

// jobConditionFor builds the status condition describing an in-flight or failed local operation, so
// the UI's detail view surfaces why a cluster is Provisioning/Deleting or — most usefully — the
// reason it Failed (which the list otherwise drops). It returns ok=false for any other phase: a Ready
// cluster discovered from a provider carries no synthetic condition. The condition uses the
// conventional Ready type so the existing UI renders it, and `since` becomes its transition time.
func jobConditionFor(
	phase v1alpha1.ClusterPhase,
	message string,
	since time.Time,
) (metav1.Condition, bool) {
	var reason, detail string

	switch phase {
	case v1alpha1.ClusterPhaseProvisioning:
		reason, detail = "Provisioning", "Creating cluster"
	case v1alpha1.ClusterPhaseDeleting:
		reason, detail = "Deleting", "Deleting cluster"
	case v1alpha1.ClusterPhaseFailed:
		reason, detail = "Error", message
	case v1alpha1.ClusterPhaseReady,
		v1alpha1.ClusterPhasePending,
		v1alpha1.ClusterPhaseStopped,
		v1alpha1.ClusterPhaseUpdating:
		// Ready/Pending/Updating clusters carry no synthetic job condition. A Stopped cluster's
		// Ready=False/reason=Stopped condition is attached by listEntryCondition's run-state branch
		// (discovered-stopped clusters never enter the job store), so it carries none here either.
		return metav1.Condition{}, false
	default:
		return metav1.Condition{}, false
	}

	return metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            detail,
		LastTransitionTime: metav1.NewTime(since),
	}, true
}

func isCreatable(distribution v1alpha1.Distribution) bool {
	return slices.Contains(creatableDistributions(), distribution)
}

// defaultFactory builds a real provisioner factory with a default distribution config for the given
// name. The configs carry the TypeMeta/defaults the provisioners require to create clusters, and are
// equally valid for listing (where the cluster name is ignored).
func defaultFactory(
	distribution v1alpha1.Distribution,
	name string,
) (clusterprovisioner.Factory, error) {
	config, err := distributionConfig(distribution, name)
	if err != nil {
		return nil, err
	}

	return clusterprovisioner.DefaultFactory{DistributionConfig: config}, nil
}
