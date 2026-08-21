package operator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/controller"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// HostClusterName is the well-known name of the Cluster resource the operator self-registers to
// represent the cluster it runs ON, so the hub itself shows up in the cluster list and can be
// browsed like any managed cluster (cf. Rancher's "local" cluster, Argo CD's "in-cluster"
// destination, Headlamp's "main" context).
const HostClusterName = v1alpha1.HostClusterName

// defaultHostClusterNamespace is the last-resort namespace for the host registration, used when
// the operator runs outside a cluster (e.g. local development against a kubeconfig).
const defaultHostClusterNamespace = "default"

// Bounded retry for the startup registration: the API server may briefly refuse writes right after
// the operator starts (e.g. webhook or cache warm-up), so transient failures are retried.
const (
	hostClusterRegistrationInterval = 2 * time.Second
	hostClusterRegistrationTimeout  = time.Minute
)

// ErrHostClusterNameTaken is returned when a Cluster resource already occupies the well-known host
// name without carrying the host-cluster label. The operator never adopts it (the user owns it);
// registration is skipped instead.
var ErrHostClusterNameTaken = errors.New(
	"host cluster registration skipped: a cluster with the reserved name exists without the " +
		v1alpha1.HostClusterLabel + " label",
)

// HostClusterNamespace resolves the namespace the host Cluster resource is registered in: the
// operator's own namespace (POD_NAMESPACE downward-API env var, then the projected ServiceAccount
// namespace file — see k8s.InClusterNamespace), then "default" (running outside a cluster).
func HostClusterNamespace() string {
	if namespace := k8s.InClusterNamespace(); namespace != "" {
		return namespace
	}

	return defaultHostClusterNamespace
}

// EnsureHostCluster registers the host cluster: it creates the well-known host Cluster resource,
// labelled v1alpha1.HostClusterLabel, in the given namespace when it does not exist yet. The spec
// is left empty on purpose — the operator does not manage the host cluster's lifecycle, so the
// resource only carries observed status. Idempotent: an existing registration is left untouched,
// and a same-named resource without the label is never adopted (ErrHostClusterNameTaken).
func EnsureHostCluster(ctx context.Context, hub client.Client, namespace string) error {
	var existing v1alpha1.Cluster

	key := types.NamespacedName{Namespace: namespace, Name: HostClusterName}

	err := hub.Get(ctx, key, &existing)
	if err == nil {
		if existing.IsHostClusterRegistration(namespace) {
			return nil
		}

		return fmt.Errorf("%w: %s/%s", ErrHostClusterNameTaken, namespace, HostClusterName)
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("check host cluster registration: %w", err)
	}

	cluster := &v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      HostClusterName,
			Namespace: namespace,
			Labels:    map[string]string{v1alpha1.HostClusterLabel: "true"},
		},
	}

	createErr := hub.Create(ctx, cluster)
	// Tolerate a concurrent registration (a competing replica created it first).
	if createErr != nil && !apierrors.IsAlreadyExists(createErr) {
		return fmt.Errorf("register host cluster: %w", createErr)
	}

	return nil
}

// AddHostClusterRegistration adds a startup task to the manager that ensures the host Cluster
// resource exists. The runnable is leader-gated (only the active operator registers) and
// best-effort: failures are retried briefly and then logged — a missing host registration only
// means the hub does not appear in the cluster list, so it must never crash the operator.
func AddHostClusterRegistration(mgr ctrl.Manager, namespace string) error {
	runnable := manager.RunnableFunc(func(ctx context.Context) error {
		log := logf.FromContext(ctx).WithName("host-cluster-registration")

		err := wait.PollUntilContextTimeout(
			ctx,
			hostClusterRegistrationInterval,
			hostClusterRegistrationTimeout,
			true,
			func(ctx context.Context) (bool, error) {
				ensureErr := EnsureHostCluster(ctx, mgr.GetClient(), namespace)
				if ensureErr == nil {
					return true, nil
				}

				// A name collision is terminal: retrying cannot resolve it, the user owns the resource.
				if errors.Is(ensureErr, ErrHostClusterNameTaken) {
					return false, ensureErr
				}

				log.Info("retrying host cluster registration", "error", ensureErr.Error())

				return false, nil
			},
		)
		if err != nil {
			log.Error(
				err,
				"host cluster registration failed; the host cluster will not appear in the cluster list",
			)
		}

		return nil
	})

	err := mgr.Add(runnable)
	if err != nil {
		return fmt.Errorf("add host cluster registration: %w", err)
	}

	return nil
}

// NewHostStatusObserver returns a StatusObserver for the self-registered host cluster, reporting
// the API endpoint and node readiness through the operator's own credentials (the manager's REST
// config). The hub reader argument is unused: the host cluster is observed directly, not through a
// published kubeconfig Secret.
func NewHostStatusObserver(restConfig *rest.Config) controller.StatusObserver {
	return func(
		ctx context.Context,
		_ client.Reader,
		_ *v1alpha1.Cluster,
	) (controller.ObservedStatus, error) {
		clientset, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			return controller.ObservedStatus{}, fmt.Errorf("build host clientset: %w", err)
		}

		observed := controller.ObservedStatus{Endpoint: restConfig.Host}

		ready, total, err := countReadyNodes(ctx, clientset)
		if err != nil {
			// The endpoint is still useful; surface the node-count failure for logging.
			return observed, fmt.Errorf("count host nodes: %w", err)
		}

		observed.NodesReady = ready
		observed.NodesTotal = total
		observed.NodesObserved = true

		return observed, nil
	}
}
