// Package operator wires and runs the KSail Kubernetes operator: a controller-runtime
// manager that reconciles Cluster custom resources from inside a hub cluster.
package operator

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/internal/controller"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/webui"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
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
	// HostCluster self-registers the cluster the operator runs on as a Cluster resource (named
	// "host", labelled ksail.io/host-cluster) so the hub itself appears in the cluster list and can
	// be browsed through the operator's own credentials.
	HostCluster bool
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
	hostNamespace := ""
	if opts.HostCluster {
		hostNamespace = HostClusterNamespace()
	}

	reconciler := &controller.ClusterReconciler{
		Client:               mgr.GetClient(),
		Scheme:               mgr.GetScheme(),
		NewProvisioner:       BuildProvisioner,
		ObserveStatus:        ObserveVClusterStatus,
		ObserveHostStatus:    NewHostStatusObserver(mgr.GetConfig()),
		HostClusterNamespace: hostNamespace,
		InstallComponents:    InstallComponents,
		APIReader:            mgr.GetAPIReader(),
	}

	reconcilerErr := reconciler.SetupWithManager(mgr)
	if reconcilerErr != nil {
		return fmt.Errorf("set up cluster reconciler: %w", reconcilerErr)
	}

	if opts.HostCluster {
		hostErr := AddHostClusterRegistration(mgr, hostNamespace)
		if hostErr != nil {
			return hostErr
		}
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
		return setupAPIServer(mgr, opts, hostNamespace)
	}

	return nil
}

// setupAPIServer registers the REST API server (and embedded dashboard) with the manager.
func setupAPIServer(mgr ctrl.Manager, opts Options, hostNamespace string) error {
	hub := mgr.GetClient()
	// Resolve a dynamic client for a cluster's managed (vcluster) child cluster, so the dashboard's
	// resource browser works against the operator backend too — not just the local `ksail open web`. The
	// self-registered host cluster is browsed through the operator's own credentials instead of a
	// published kubeconfig Secret.
	newChildClient := func(ctx context.Context, cluster *v1alpha1.Cluster) (dynamic.Interface, error) {
		if opts.HostCluster && cluster.IsHostClusterRegistration(hostNamespace) {
			dyn, err := dynamic.NewForConfig(mgr.GetConfig())
			if err != nil {
				return nil, fmt.Errorf("build host cluster dynamic client: %w", err)
			}

			return dyn, nil
		}

		return childClusterDynamicClient(ctx, hub, cluster)
	}

	server := &api.Server{
		Service:     NewCRClusterServiceWithResources(hub, newChildClient),
		ReadOnly:    opts.ReadOnly,
		BindAddress: opts.APIBindAddress,
		OIDC:        opts.OIDC,
		Mode:        api.ModeOperator,
	}

	// Serve the dashboard from the operator itself (same origin as the API, no reverse proxy).
	// webui.Assets always returns a filesystem; when the SPA was not built into pkg/webui/dist,
	// it holds only a placeholder and the server renders a "UI not built" page.
	server.StaticFS = webui.Assets()

	apiErr := mgr.Add(server)
	if apiErr != nil {
		return fmt.Errorf("add API server: %w", apiErr)
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
