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
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	kindconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
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
	phase        v1alpha1.ClusterPhase
	err          error
}

// Ensure Service satisfies the operator REST API's backend contract.
var _ api.ClusterService = (*Service)(nil)

// Service implements api.ClusterService over the local provider/provisioner lifecycle.
type Service struct {
	newFactory FactoryFunc

	mu   sync.Mutex
	jobs map[string]*job
}

// NewService returns a local ClusterService backed by the real provisioner factory.
func NewService() *Service {
	return &Service{
		newFactory: defaultFactory,
		jobs:       map[string]*job{},
	}
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

// creatableDistributions is the MVP set of locally provisionable distributions.
func creatableDistributions() []v1alpha1.Distribution {
	return []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionVCluster,
	}
}

// listDistributions is the set enumerated when listing existing clusters. It is broader than the
// creatable set so clusters created out-of-band (e.g. via the CLI) still appear.
func listDistributions() []v1alpha1.Distribution {
	return []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos,
		v1alpha1.DistributionVCluster,
		v1alpha1.DistributionKWOK,
	}
}

// List returns the union of clusters discovered from the providers and clusters tracked in the job
// store (in-flight creates/deletes). Job state takes precedence so a cluster mid-create shows
// Provisioning and one mid-delete shows Deleting even before the provider reflects it.
func (s *Service) List(ctx context.Context) (*v1alpha1.ClusterList, error) {
	live := s.enumerate(ctx)

	merged := make(map[string]v1alpha1.ClusterPhase, len(live))
	dists := make(map[string]v1alpha1.Distribution, len(live))

	for name, dist := range live {
		merged[name] = v1alpha1.ClusterPhaseReady
		dists[name] = dist
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

		merged[name] = current.phase
		dists[name] = current.distribution
	}
	s.mu.Unlock()

	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}

	sort.Strings(names)

	items := make([]v1alpha1.Cluster, 0, len(names))
	for _, name := range names {
		items = append(items, newCluster(name, dists[name], merged[name]))
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

	live := s.enumerate(ctx)

	s.mu.Lock()

	_, inLive := live[name]
	_, inJobs := s.jobs[name]

	if inLive || inJobs {
		s.mu.Unlock()

		return nil, fmt.Errorf("%w: %q", api.ErrAlreadyExists, name)
	}

	s.jobs[name] = &job{distribution: distribution, phase: v1alpha1.ClusterPhaseProvisioning}
	s.mu.Unlock()

	go s.runCreate(context.WithoutCancel(ctx), name, distribution)

	created := newCluster(name, distribution, v1alpha1.ClusterPhaseProvisioning)

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
	distribution, ok := s.resolveDistribution(ctx, name)
	if !ok {
		return fmt.Errorf("%w: %q", api.ErrNotFound, name)
	}

	s.mu.Lock()
	s.jobs[name] = &job{distribution: distribution, phase: v1alpha1.ClusterPhaseDeleting}
	s.mu.Unlock()

	go s.runDelete(context.WithoutCancel(ctx), name, distribution)

	return nil
}

// resolveDistribution finds the distribution of an existing cluster, checking live providers first
// and then the job store (for clusters still being provisioned).
func (s *Service) resolveDistribution(
	ctx context.Context,
	name string,
) (v1alpha1.Distribution, bool) {
	live := s.enumerate(ctx)
	if dist, ok := live[name]; ok {
		return dist, true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if current, ok := s.jobs[name]; ok {
		return current.distribution, true
	}

	return "", false
}

func (s *Service) runCreate(ctx context.Context, name string, distribution v1alpha1.Distribution) {
	err := s.runProvisioner(ctx, name, distribution, func(p clusterprovisioner.Provisioner) error {
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
			"cluster", name, "distribution", distribution, "error", err)

		current.phase = v1alpha1.ClusterPhaseFailed
		current.err = err

		return
	}

	current.phase = v1alpha1.ClusterPhaseReady
}

func (s *Service) runDelete(ctx context.Context, name string, distribution v1alpha1.Distribution) {
	err := s.runProvisioner(
		ctx,
		name,
		distribution,
		func(provisioner clusterprovisioner.Provisioner) error {
			// Deleting must be idempotent: a failed create (or a cluster removed out-of-band) leaves a
			// tracked entry with no underlying cluster to delete. Probe Exists first and treat "already
			// gone" as success, so the entry is cleared below. Otherwise Delete returns
			// ErrClusterNotFound, the job is pinned Failed, and the UI row can never be dismissed — the
			// user is forced to restart the app or fall back to `ksail cluster delete --name`.
			exists, existsErr := provisioner.Exists(ctx, name)
			if existsErr != nil {
				return fmt.Errorf("check cluster existence: %w", existsErr)
			}

			if !exists {
				return nil
			}

			return provisioner.Delete(ctx, name)
		},
	)

	s.mu.Lock()
	defer s.mu.Unlock()

	if err != nil {
		slog.Error("cluster deletion failed",
			"cluster", name, "distribution", distribution, "error", err)

		current := s.jobs[name]
		if current != nil {
			current.phase = v1alpha1.ClusterPhaseFailed
			current.err = err
		}

		return
	}

	delete(s.jobs, name)
}

// runProvisioner builds a provisioner for the distribution and runs the supplied action against it.
func (s *Service) runProvisioner(
	ctx context.Context,
	name string,
	distribution v1alpha1.Distribution,
	action func(clusterprovisioner.Provisioner) error,
) error {
	provisioner, err := s.buildProvisioner(ctx, distribution, name)
	if err != nil {
		return err
	}

	return action(provisioner)
}

// enumerate discovers existing clusters across the local distributions, keyed by name. Errors for
// individual distributions are swallowed (best-effort), matching `ksail cluster list`.
func (s *Service) enumerate(ctx context.Context) map[string]v1alpha1.Distribution {
	found := make(map[string]v1alpha1.Distribution)

	for _, distribution := range listDistributions() {
		provisioner, err := s.buildProvisioner(ctx, distribution, "")
		if err != nil {
			continue
		}

		names, listErr := provisioner.List(ctx)
		if listErr != nil {
			continue
		}

		for _, name := range names {
			if _, ok := found[name]; !ok {
				found[name] = distribution
			}
		}
	}

	return found
}

func (s *Service) buildProvisioner(
	ctx context.Context,
	distribution v1alpha1.Distribution,
	name string,
) (clusterprovisioner.Provisioner, error) {
	factory, err := s.newFactory(distribution, name)
	if err != nil {
		return nil, err
	}

	cluster := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{Cluster: v1alpha1.ClusterSpec{Distribution: distribution}},
	}

	provisioner, _, err := factory.Create(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("create %s provisioner: %w", distribution, err)
	}

	return provisioner, nil
}

// newCluster builds the Kubernetes-shaped Cluster the web UI consumes from a local cluster's name,
// distribution, and phase.
func newCluster(
	name string,
	distribution v1alpha1.Distribution,
	phase v1alpha1.ClusterPhase,
) v1alpha1.Cluster {
	cluster := v1alpha1.Cluster{}
	cluster.Name = name
	cluster.Namespace = localNamespace
	cluster.Spec.Cluster.Distribution = distribution
	cluster.Spec.Cluster.Provider = v1alpha1.ProviderDocker
	cluster.Status.Phase = phase

	return cluster
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
	//nolint:exhaustive // K3s/VCluster/KWOK go through SimpleDistributionConfig; EKS is unavailable.
	switch distribution {
	case v1alpha1.DistributionVanilla:
		// NewKindCluster sets the TypeMeta; SetDefaultsCluster adds the default control-plane node.
		// An empty v1alpha4.Cluster{} is rejected by Kind with "unknown apiVersion".
		kindCluster := kindconfigmanager.NewKindCluster(name, "", "")
		v1alpha4.SetDefaultsCluster(kindCluster)

		return &clusterprovisioner.DistributionConfig{Kind: kindCluster}, nil
	case v1alpha1.DistributionTalos:
		return &clusterprovisioner.DistributionConfig{Talos: &talosconfigmanager.Configs{}}, nil
	default:
		// K3s, VCluster, KWOK need only the name (shared with the operator backend); EKS and any
		// other distribution cannot be provisioned locally.
		config := clusterprovisioner.SimpleDistributionConfig(distribution, name)
		if config != nil {
			return config, nil
		}

		return nil, errDistributionUnavailable(distribution)
	}
}

func errDistributionUnavailable(distribution v1alpha1.Distribution) error {
	return fmt.Errorf(
		"%w: distribution %q is not available locally",
		api.ErrNotSupported,
		distribution,
	)
}
