package nested

import (
	"context"
	"fmt"
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// DinDLifecycle bundles the host-cluster dependencies the DinD-based nested
// provisioners (Kind, KWOK, Talos) share, and provides the Delete/Exists/List/
// SetupDinD flows that were previously copy-pasted across those wrappers (and
// silenced with jscpd:ignore markers).
//
// Construct it once per provisioner with the resolved K8s provider, dynamic
// client, REST config, and kubeconfig path; the per-call cluster name (already
// resolved by the provisioner) is passed to each method.
type DinDLifecycle struct {
	// Provider is the host Kubernetes infrastructure provider.
	Provider *kubernetesprovider.Provider
	// DynamicClient is the dynamic client for Gateway API teardown.
	DynamicClient dynamic.Interface
	// RestConfig is the host REST config used for the Docker-API exec tunnel.
	RestConfig *rest.Config
	// KubeconfigPath is the kubeconfig file cleaned up on Delete. When empty,
	// kubeconfig cleanup is skipped.
	KubeconfigPath string
	// LogWriter receives kubeconfig-cleanup output. When nil, io.Discard is used.
	LogWriter io.Writer
}

// Delete tears down a DinD-hosted nested cluster: it best-effort deletes the
// child cluster inside DinD through the inner provisioner (run with DOCKER_HOST
// pointed at the DinD Docker API via an exec tunnel), then removes the API
// exposure, DinD pod, and namespace, and finally cleans up the host kubeconfig
// entries for contextName.
//
// target is the resolved cluster name (used for DinD/namespace teardown and the
// kubeconfig context). innerDelete performs the in-DinD delete; its callers pass
// the name the inner SDK expects — Kind passes the resolved target while KWOK
// passes the raw user-supplied name, a divergence this helper preserves by taking
// innerDelete as a closure.
func (l DinDLifecycle) Delete(
	ctx context.Context,
	target, contextName string,
	innerDelete func() error,
) error {
	// Best-effort: delete the child cluster inside DinD via the inner SDK.
	dockerPF, pfErr := l.Provider.StartExecTunnel(
		ctx, l.RestConfig, target,
		kubernetesprovider.DinDPodName, kubernetesprovider.DinDContainerName,
		kubernetesprovider.DinDDockerPort,
	)
	if pfErr == nil {
		defer dockerPF.Close()

		_ = kubernetesprovider.WithRemoteDockerHost(dockerPF, innerDelete)
	}

	// Clean up API exposure, DinD, and namespace.
	err := l.Provider.TeardownDinD(ctx, l.DynamicClient, target)
	if err != nil {
		return fmt.Errorf("teardown DinD: %w", err)
	}

	if l.KubeconfigPath == "" {
		return nil
	}

	writer := l.LogWriter
	if writer == nil {
		writer = io.Discard
	}

	err = k8s.CleanupKubeconfig(l.KubeconfigPath, contextName, contextName, contextName, writer)
	if err != nil {
		return fmt.Errorf("cleanup kubeconfig: %w", err)
	}

	return nil
}

// Exists reports whether the DinD-hosted nested cluster exists by checking for
// its node pod(s) in the host cluster.
func (l DinDLifecycle) Exists(ctx context.Context, target string) (bool, error) {
	exists, err := l.Provider.NodesExist(ctx, target)
	if err != nil {
		return false, fmt.Errorf("check nodes: %w", err)
	}

	return exists, nil
}

// List returns the names of all nested clusters managed by the host provider.
func (l DinDLifecycle) List(ctx context.Context) ([]string, error) {
	clusters, err := l.Provider.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}

	return clusters, nil
}

// SetupDinD creates the namespace and DinD pod for the nested cluster and waits
// for readiness. distribution labels the DinD pod; persistence configures the
// Docker data PVC.
func (l DinDLifecycle) SetupDinD(
	ctx context.Context,
	target, distribution string,
	persistence v1alpha1.KubernetesPersistence,
) error {
	err := l.Provider.SetupDinD(ctx, target, distribution, persistence)
	if err != nil {
		return fmt.Errorf("setup DinD: %w", err)
	}

	return nil
}
