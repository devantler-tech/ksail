// Package operator wires and runs the KSail Kubernetes operator: a controller-runtime
// manager that reconciles Cluster custom resources from inside a hub cluster.
package operator

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/internal/controller"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// DefaultLeaderElectionID is the lease name used when leader election is enabled.
const DefaultLeaderElectionID = "ksail-operator.ksail.io"

// Options configures the operator manager.
type Options struct {
	// MetricsBindAddress is the address the metrics endpoint binds to ("0" disables it).
	MetricsBindAddress string
	// HealthProbeBindAddress is the address the health/readiness probes bind to.
	HealthProbeBindAddress string
	// LeaderElection enables leader election to ensure a single active operator.
	LeaderElection bool
	// LeaderElectionID overrides the leader election lease name (optional).
	LeaderElectionID string
}

// Run builds the controller-runtime manager, registers the Cluster reconciler, and blocks
// until the supplied context is cancelled (e.g. on SIGTERM).
func Run(ctx context.Context, opts Options) error {
	scheme, err := newScheme()
	if err != nil {
		return err
	}

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("load kubernetes config: %w", err)
	}

	leaderID := opts.LeaderElectionID
	if leaderID == "" {
		leaderID = DefaultLeaderElectionID
	}

	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: opts.MetricsBindAddress},
		HealthProbeBindAddress: opts.HealthProbeBindAddress,
		LeaderElection:         opts.LeaderElection,
		LeaderElectionID:       leaderID,
	})
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}

	reconciler := &controller.ClusterReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		NewProvisioner: BuildProvisioner,
	}

	setupErr := reconciler.SetupWithManager(mgr)
	if setupErr != nil {
		return fmt.Errorf("set up cluster reconciler: %w", setupErr)
	}

	healthErr := mgr.AddHealthzCheck("healthz", healthz.Ping)
	if healthErr != nil {
		return fmt.Errorf("add health check: %w", healthErr)
	}

	readyErr := mgr.AddReadyzCheck("readyz", healthz.Ping)
	if readyErr != nil {
		return fmt.Errorf("add ready check: %w", readyErr)
	}

	startErr := mgr.Start(ctx)
	if startErr != nil {
		return fmt.Errorf("start manager: %w", startErr)
	}

	return nil
}

// newScheme builds the runtime scheme with the core Kubernetes types and the KSail API.
func newScheme() (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()

	clientErr := clientgoscheme.AddToScheme(scheme)
	if clientErr != nil {
		return nil, fmt.Errorf("register client-go scheme: %w", clientErr)
	}

	ksailErr := v1alpha1.AddToScheme(scheme)
	if ksailErr != nil {
		return nil, fmt.Errorf("register ksail scheme: %w", ksailErr)
	}

	return scheme, nil
}
