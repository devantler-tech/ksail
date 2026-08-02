package clusterapi

import (
	"context"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// SetDynamicClientForTest overrides the dynamic-client builder so resource-browser tests can inject a
// fake client instead of resolving a real kubeconfig context.
func (s *Service) SetDynamicClientForTest(
	build func(ctx context.Context, clusterName string) (dynamic.Interface, error),
) {
	s.newDynamicClient = build
}

// SetLogClientForTest overrides the clientset builder so pod-log tests can inject a fake clientset
// instead of resolving a real kubeconfig context.
func (s *Service) SetLogClientForTest(
	build func(ctx context.Context, clusterName string) (kubernetes.Interface, error),
) {
	s.newLogClient = build
}

// ContextForCluster exposes contextForCluster for black-box tests of name→context resolution.
func ContextForCluster(kubeconfigPath, clusterName string) (string, error) {
	return contextForCluster(kubeconfigPath, clusterName)
}

// SetKubeconfigPathForTest overrides the kubeconfig path every cluster client (and the resource
// browser / kubeconfig export) reads from via the restConfigForCluster seam, so tests can point at a
// temp kubeconfig instead of the user's real one.
func (s *Service) SetKubeconfigPathForTest(path string) {
	s.kubeconfigPath = func() string { return path }
}

// SetRESTConfigForClusterForTest overrides the single kubeconfig seam, so a test can drive every
// derived client builder (dynamic, apply, log, exec) from one fake/synthetic rest.Config instead of
// injecting four separate fakes.
func (s *Service) SetRESTConfigForClusterForTest(
	build func(clusterName string) (*rest.Config, error),
) {
	s.restConfigForCluster = build
}

// SetApplyClientForTest overrides the apply-client builder so manifest-apply tests can inject a fake
// dynamic client + a static REST mapper instead of resolving a real cluster.
func (s *Service) SetApplyClientForTest(
	build func(ctx context.Context, clusterName string) (dynamic.Interface, meta.RESTMapper, error),
) {
	s.newApplyClient = build
}

// SetDockerStatusForTest overrides discovery's Docker run-state probe so a test can drive a cluster's
// running/stopped state without a real Docker daemon, exercising the local backend's stopped-cluster
// rendering (no Ready phase + a Ready=False/reason=Stopped condition).
func (s *Service) SetDockerStatusForTest(
	probe func(ctx context.Context, distribution v1alpha1.Distribution, name string) clusterdiscovery.RunState,
) {
	s.discoverer.DockerStatus = probe
}

// SetLoadClusterSpecForTest overrides the ownership-state read that clearedFailedEKSCreate performs
// with the lock released. A test substitutes a loader that mutates the service's job table, which
// lands the mutation precisely inside the unlocked window and makes the race deterministic — no
// goroutines, no sleeps, no flakiness.
func (s *Service) SetLoadClusterSpecForTest(
	load func(clusterName string) (*v1alpha1.ClusterSpec, error),
) {
	s.loadClusterSpec = load
}

// ReplaceJobWithFailedStopForTest swaps the tracked job for one that began as a stop and ended in
// Failed, mirroring what a start/stop registration does. It is the state a failed-create clearing
// path must refuse: the remote cluster may well still be running.
func (s *Service) ReplaceJobWithFailedStopForTest(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobs[name] = &job{
		distribution: v1alpha1.DistributionEKS,
		provider:     v1alpha1.ProviderAWS,
		phase:        v1alpha1.ClusterPhaseFailed,
		origin:       v1alpha1.ClusterPhaseStopped,
		startedAt:    time.Now(),
	}
}

// JobPresentForTest reports whether a job is still tracked for the cluster, so a test can assert the
// refusal preserved the row rather than clearing it.
func (s *Service) JobPresentForTest(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, found := s.jobs[name]

	return found
}

// NewTestService returns a Service whose provisioner factory is overridden, so black-box tests can
// substitute fake provisioners without touching the real Docker-backed factory. Discovery is
// restricted to the Docker provider so tests stay hermetic — they never reach out to cloud
// providers (Hetzner/Omni/AWS) or a host Kubernetes cluster based on the developer's environment.
func NewTestService(factory FactoryFunc) *Service {
	service := NewService()
	service.newFactory = factory
	service.discoverProviders = []v1alpha1.Provider{v1alpha1.ProviderDocker}
	// Point the kubeconfig at nowhere by default so List's endpoint enrichment never reads the
	// developer's real kubeconfig; tests that need one inject it via SetKubeconfigPathForTest.
	service.kubeconfigPath = func() string { return "" }
	// Stub the AWS-touching half of the EKS mutation guard so tests stay hermetic. Tests that are
	// about the guard itself override it with SetEKSOwnershipGuardForTest.
	service.captureEKSIdentity = func(context.Context, string) error { return nil }
	service.resolveEKSGuard = func(
		context.Context,
		string,
	) (credentials.AWSResolution, eksidentity.Verifier, error) {
		return credentials.AWSResolution{}, func(context.Context) error { return nil }, nil
	}

	return service
}

// SetEKSOwnershipCaptureForTest overrides the create-time identity capture, so a test can drive a
// failing capture without AWS.
func (s *Service) SetEKSOwnershipCaptureForTest(
	capture func(ctx context.Context, name string) error,
) {
	s.captureEKSIdentity = capture
}

// SetEKSOwnershipTimeoutForTest shortens the bound on the ownership resolution's network calls, so a
// test can drive the deadline path without waiting the production budget.
func (s *Service) SetEKSOwnershipTimeoutForTest(d time.Duration) {
	s.eksOwnershipTimeout = d
}

// SetEKSOwnershipGuardForTest overrides the AWS-touching half of the EKS mutation guard, so a test
// can drive a refusing or accepting immutable-identity check without AWS credentials.
func (s *Service) SetEKSOwnershipGuardForTest(
	guard func(
		ctx context.Context,
		name string,
	) (credentials.AWSResolution, eksidentity.Verifier, error),
) {
	s.resolveEKSGuard = guard
}

// ExportEKSConfigForCreate exposes eksDistributionConfig for testing the generated eks.yaml. It
// returns the written config path and the resolved region.
func ExportEKSConfigForCreate(name string) (string, string, error) {
	config, err := eksDistributionConfig(name)
	if err != nil {
		return "", "", err
	}

	return config.EKS.ConfigPath, config.EKS.Region, nil
}

// ReplaceJobWithAnotherFailedEKSCreateForTest swaps the tracked job for a DIFFERENT job that is also
// a failed EKS create. Every field the failed-create predicate tests is identical, so only comparing
// the entry's identity can tell the two apart — which is what stops a delete clearing (and hiding)
// the failure of a create it never approved.
func (s *Service) ReplaceJobWithAnotherFailedEKSCreateForTest(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobs[name] = &job{
		distribution: v1alpha1.DistributionEKS,
		provider:     v1alpha1.ProviderAWS,
		phase:        v1alpha1.ClusterPhaseFailed,
		origin:       v1alpha1.ClusterPhaseProvisioning,
		startedAt:    time.Now(),
	}
}
