// Package operator wires and runs the KSail Kubernetes operator: a controller-runtime
// manager that reconciles Cluster custom resources from inside a hub cluster.
package operator

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/internal/controller"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	operatorui "github.com/devantler-tech/ksail/v7/pkg/operator/ui"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
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
	// APIBindAddress is the address the REST API binds to (empty disables it).
	APIBindAddress string
	// ReadOnly puts the REST API in read-only mode, rejecting all mutating requests.
	ReadOnly bool
	// OIDC configures app-driven OIDC authentication for the REST API (disabled when empty).
	OIDC api.OIDCConfig
	// LeaderElection enables leader election to ensure a single active operator.
	LeaderElection bool
	// LeaderElectionID overrides the leader election lease name (optional).
	LeaderElectionID string
	// DevLogging selects human-readable console logs instead of production JSON logs.
	DevLogging bool
}

// Run builds the controller-runtime manager, registers the Cluster reconciler, and blocks
// until the supplied context is cancelled (e.g. on SIGTERM).
func Run(ctx context.Context, opts Options) error {
	// Configure controller-runtime's global logger. Without this, controller-runtime discards all
	// logs (reconcile events, API server, errors) and prints a "SetLogger was never called" warning.
	ctrl.SetLogger(zap.New(zap.UseDevMode(opts.DevLogging)))

	scheme, err := newScheme()
	if err != nil {
		return err
	}

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("load kubernetes config: %w", err)
	}

	mgr, err := ctrl.NewManager(restConfig, managerOptions(scheme, opts))
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}

	setupErr := setupManager(mgr, opts)
	if setupErr != nil {
		return setupErr
	}

	startErr := mgr.Start(ctx)
	if startErr != nil {
		return fmt.Errorf("start manager: %w", startErr)
	}

	return nil
}

// managerOptions builds the controller-runtime manager options from the operator Options.
func managerOptions(scheme *runtime.Scheme, opts Options) ctrl.Options {
	leaderID := opts.LeaderElectionID
	if leaderID == "" {
		leaderID = DefaultLeaderElectionID
	}

	return ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: opts.MetricsBindAddress},
		HealthProbeBindAddress: opts.HealthProbeBindAddress,
		LeaderElection:         opts.LeaderElection,
		LeaderElectionID:       leaderID,
	}
}

// setupManager registers the reconciler, health probes, and (optionally) the REST API server.
func setupManager(mgr ctrl.Manager, opts Options) error {
	reconciler := &controller.ClusterReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		NewProvisioner:    BuildProvisioner,
		ObserveStatus:     ObserveVClusterStatus,
		InstallComponents: InstallComponents,
		APIReader:         mgr.GetAPIReader(),
	}

	reconcilerErr := reconciler.SetupWithManager(mgr)
	if reconcilerErr != nil {
		return fmt.Errorf("set up cluster reconciler: %w", reconcilerErr)
	}

	healthErr := mgr.AddHealthzCheck("healthz", healthz.Ping)
	if healthErr != nil {
		return fmt.Errorf("add health check: %w", healthErr)
	}

	readyErr := mgr.AddReadyzCheck("readyz", healthz.Ping)
	if readyErr != nil {
		return fmt.Errorf("add ready check: %w", readyErr)
	}

	if opts.APIBindAddress != "" {
		server := &api.Server{
			Service:     api.NewCRClusterService(mgr.GetClient()),
			ReadOnly:    opts.ReadOnly,
			BindAddress: opts.APIBindAddress,
			OIDC:        opts.OIDC,
		}

		// Serve the dashboard from the operator itself when a UI was embedded at build time
		// (-tags ui), so the SPA and API share one origin and no reverse proxy is needed.
		if assets, ok := operatorui.Assets(); ok {
			server.StaticFS = assets
		}

		apiErr := mgr.Add(server)
		if apiErr != nil {
			return fmt.Errorf("add API server: %w", apiErr)
		}
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
