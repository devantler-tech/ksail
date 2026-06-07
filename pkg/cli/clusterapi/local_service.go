// Package clusterapi implements the operator REST API's ClusterService over the local KSail
// provider/provisioner lifecycle. It lets `ksail ui` serve the same web UI the operator
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
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	kindconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/scaffolder"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// localNamespace is the synthetic namespace reported for local clusters. KSail has no namespace
// concept locally, but the web UI keys rows on namespace/name and builds delete paths from it, so a
// stable value is reported and the namespace path segment is otherwise ignored.
const localNamespace = "default"

// File modes for the generated EKS config under ~/.ksail/clusters/<name>.
const (
	eksConfigDirMode  = 0o700
	eksConfigFileMode = 0o600
)

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

// Ensure Service satisfies the operator REST API's backend contract, including the optional
// capability reporter the SPA uses to gate edit affordances.
var (
	_ api.ClusterService     = (*Service)(nil)
	_ api.CapabilityReporter = (*Service)(nil)
)

// Service implements api.ClusterService over the local provider/provisioner lifecycle.
type Service struct {
	newFactory FactoryFunc

	// discoverer enumerates existing clusters across providers for List/Get; discoverProviders is
	// the set it queries. NewService queries every provider the machine can reach so cloud clusters
	// (Hetzner/Omni/EKS) are visible, not just local Docker ones.
	discoverer        *clusterdiscovery.Discoverer
	discoverProviders []v1alpha1.Provider

	// newDynamicClient builds a dynamic client for a named cluster (the read-only resource browser).
	// Injectable so tests can substitute a fake client instead of resolving a real kubeconfig context.
	newDynamicClient dynamicClientFunc

	// newApplyClient builds a dynamic client + REST mapper for a named cluster (manifest apply, which
	// needs GVK→resource resolution). Injectable for tests (fake client + static mapper).
	newApplyClient applyClientFunc

	// newExecClient builds a clientset + rest.Config for a named cluster (pod exec). Injectable.
	newExecClient execClientFunc

	// kubeconfigPath resolves the kubeconfig file the resource browser / kubeconfig export read from.
	// Injectable so tests can point at a temp kubeconfig instead of the user's real one.
	kubeconfigPath func() string

	mu   sync.Mutex
	jobs map[string]*job
}

// NewService returns a local ClusterService backed by the real provisioner factory.
func NewService() *Service {
	service := &Service{
		newFactory:        defaultFactory,
		discoverProviders: clusterdiscovery.AllProviders(),
		newDynamicClient:  defaultDynamicClient,
		newApplyClient:    defaultApplyClient,
		newExecClient:     defaultExecClient,
		kubeconfigPath:    k8s.DefaultKubeconfigPath,
		jobs:              map[string]*job{},
	}
	service.discoverer = &clusterdiscovery.Discoverer{DockerFactory: service.dockerFactory}

	return service
}

// Capabilities reports the operations the local backend supports. In-place cluster update is not
// supported locally — a local cluster's configuration is managed through its config files and
// `ksail cluster update`, not the API (see Update) — so the SPA hides the edit affordance rather
// than offering a button that returns 501.
func (s *Service) Capabilities() api.Capabilities {
	return api.Capabilities{ClusterUpdate: false}
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
			phase:        v1alpha1.ClusterPhaseReady,
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

	items := make([]v1alpha1.Cluster, 0, len(names))
	for _, name := range names {
		entry := merged[name]
		cluster := newCluster(name, entry.distribution, entry.provider, entry.phase)

		condition, ok := jobConditionFor(entry.phase, entry.message, entry.since)
		if ok {
			cluster.Status.Conditions = []metav1.Condition{condition}
		}

		items = append(items, cluster)
	}

	return &v1alpha1.ClusterList{Items: items}, nil
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

// Create starts provisioning a cluster and returns immediately with the cluster in the Provisioning
// phase. The actual provisioning runs in a background goroutine; List/Get reflect its progress.
func (s *Service) Create(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
) (*v1alpha1.Cluster, error) {
	name := cluster.Name
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", api.ErrInvalid)
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

// Update is not supported for local clusters; their configuration is managed via the CLI/files.
func (s *Service) Update(
	_ context.Context,
	_, _ string,
	_ *v1alpha1.Cluster,
) (*v1alpha1.Cluster, error) {
	return nil, fmt.Errorf("%w: updating clusters is not supported locally", api.ErrNotSupported)
}

// Delete starts deleting a cluster and returns immediately, marking it Deleting. The deletion runs
// in a background goroutine.
func (s *Service) Delete(ctx context.Context, _, name string) error {
	distribution, provider, ok := s.resolveCluster(ctx, name)
	if !ok {
		return fmt.Errorf("%w: %q", api.ErrNotFound, name)
	}

	s.mu.Lock()
	s.jobs[name] = &job{
		distribution: distribution,
		provider:     provider,
		phase:        v1alpha1.ClusterPhaseDeleting,
		startedAt:    time.Now(),
	}
	s.mu.Unlock()

	// Reconstruct the spec the provisioner needs to target the right provider. Provider options
	// (server types, etc.) are irrelevant for deletion, so distribution + provider suffice.
	spec := v1alpha1.Spec{
		Cluster: v1alpha1.ClusterSpec{Distribution: distribution, Provider: provider},
	}

	go s.runDelete(context.WithoutCancel(ctx), name, spec)

	return nil
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

func (s *Service) runCreate(ctx context.Context, name string, spec v1alpha1.Spec) {
	err := s.runProvisioner(ctx, name, spec, func(p clusterprovisioner.Provisioner) error {
		return p.Create(ctx, name)
	})

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
	err := s.runProvisioner(ctx, name, spec, func(p clusterprovisioner.Provisioner) error {
		return p.Delete(ctx, name)
	})

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
	action func(clusterprovisioner.Provisioner) error,
) error {
	provisioner, err := s.buildProvisioner(ctx, spec, name)
	if err != nil {
		return err
	}

	return action(provisioner)
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
		v1alpha1.ClusterPhaseUpdating:
		// Ready/Pending/Updating clusters carry no synthetic job condition.
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

func distributionConfig(
	distribution v1alpha1.Distribution,
	name string,
) (*clusterprovisioner.DistributionConfig, error) {
	//nolint:exhaustive // K3s/VCluster/KWOK go through SimpleDistributionConfig (default case).
	switch distribution {
	case v1alpha1.DistributionVanilla:
		// NewKindCluster sets the TypeMeta; SetDefaultsCluster adds the default control-plane node.
		// An empty v1alpha4.Cluster{} is rejected by Kind with "unknown apiVersion".
		kindCluster := kindconfigmanager.NewKindCluster(name, "", "")
		v1alpha4.SetDefaultsCluster(kindCluster)

		return &clusterprovisioner.DistributionConfig{Kind: kindCluster}, nil
	case v1alpha1.DistributionTalos:
		return talosDistributionConfig(name)
	case v1alpha1.DistributionEKS:
		return eksDistributionConfig(name)
	default:
		// K3s, VCluster, KWOK need only the name (shared with the operator backend).
		config := clusterprovisioner.SimpleDistributionConfig(distribution, name)
		if config != nil {
			return config, nil
		}

		return nil, errDistributionUnavailable(distribution)
	}
}

// talosDistributionConfig builds a fully-initialized Talos config bundle named after the cluster
// (PKI is baked in at name time and cannot change later), via the same shared helper the operator
// uses. The bundle works for Talos on Docker, Hetzner, or Omni; the factory selects the provider
// from the cluster spec.
func talosDistributionConfig(
	name string,
) (*clusterprovisioner.DistributionConfig, error) {
	configs, err := talosconfigmanager.NewDefaultConfigsWithName(name)
	if err != nil {
		return nil, fmt.Errorf("talos distribution config: %w", err)
	}

	return &clusterprovisioner.DistributionConfig{Talos: configs}, nil
}

// eksDistributionConfig renders an eksctl ClusterConfig (region from the AWS_REGION environment,
// which the credential overlay populates from Settings) and writes it under ~/.ksail/clusters/<name>
// so the EKS provisioner has the on-disk config it requires to create the cluster.
func eksDistributionConfig(
	name string,
) (*clusterprovisioner.DistributionConfig, error) {
	region := os.Getenv(credentials.DefaultEnvVar(credentials.AWSRegion))

	configPath, err := writeEKSConfig(name, region)
	if err != nil {
		return nil, err
	}

	return &clusterprovisioner.DistributionConfig{
		EKS: &clusterprovisioner.EKSConfig{Name: name, Region: region, ConfigPath: configPath},
	}, nil
}

// writeEKSConfig renders and writes the eks.yaml for a cluster, returning its path. The name must be
// a single path segment, and the resolved directory is verified to stay under ~/.ksail/clusters even
// after symlink resolution, so neither a crafted name nor a symlinked cluster directory can redirect
// the write outside the intended tree.
func writeEKSConfig(name, region string) (string, error) {
	// The name becomes exactly one directory under ~/.ksail/clusters, so it must be a single path
	// segment. filepath.IsLocal alone is insufficient — it still permits multi-segment names like
	// "foo/bar" and ".", which would redirect the write into an unintended nested directory — so also
	// require the name to equal its own base element and reject the "." / ".." specials.
	if !filepath.IsLocal(name) || name != filepath.Base(name) || name == "." || name == ".." {
		return "", fmt.Errorf(
			"%w: cluster name %q must be a single path segment",
			api.ErrInvalid,
			name,
		)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	clustersRoot := filepath.Join(home, ".ksail", "clusters")

	mkErr := os.MkdirAll(filepath.Join(clustersRoot, name), eksConfigDirMode)
	if mkErr != nil {
		return "", fmt.Errorf("create eks config directory: %w", mkErr)
	}

	dir, err := canonicalClusterDir(clustersRoot, name)
	if err != nil {
		return "", err
	}

	configPath := filepath.Join(dir, scaffolder.EKSConfigFile)
	content := scaffolder.RenderEKSConfig(scaffolder.DefaultEKSConfigParams(name, region))

	// dir is canonicalized and verified within ~/.ksail/clusters by canonicalClusterDir above.
	//nolint:gosec // configPath is contained within ~/.ksail/clusters (see canonicalClusterDir)
	writeErr := os.WriteFile(configPath, content, eksConfigFileMode)
	if writeErr != nil {
		return "", fmt.Errorf("write eks config: %w", writeErr)
	}

	return configPath, nil
}

// canonicalClusterDir canonicalizes ~/.ksail/clusters/<name> (resolving symlinks) and confirms it
// remains within the canonical clusters root, rejecting any path that escapes it.
func canonicalClusterDir(clustersRoot, name string) (string, error) {
	canonicalRoot, err := fsutil.EvalCanonicalPath(clustersRoot)
	if err != nil {
		return "", fmt.Errorf("canonicalize clusters directory: %w", err)
	}

	canonicalDir, err := fsutil.EvalCanonicalPath(filepath.Join(clustersRoot, name))
	if err != nil {
		return "", fmt.Errorf("canonicalize eks config directory: %w", err)
	}

	rel, err := filepath.Rel(canonicalRoot, canonicalDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: eks config path escapes %s", api.ErrInvalid, canonicalRoot)
	}

	return canonicalDir, nil
}

func errDistributionUnavailable(distribution v1alpha1.Distribution) error {
	return fmt.Errorf(
		"%w: distribution %q is not available locally",
		api.ErrNotSupported,
		distribution,
	)
}
