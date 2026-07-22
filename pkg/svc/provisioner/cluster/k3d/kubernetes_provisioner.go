package k3dprovisioner

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/nested"
	k3kv1beta1 "github.com/rancher/k3k/pkg/apis/k3k.io/v1beta1"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// k3kChartRepo is the Helm chart repository URL for k3k.
	k3kChartRepo = "https://rancher.github.io/k3k"
	// k3kChartName is the Helm chart name for k3k.
	k3kChartName = "k3k/k3k"
	// k3kReleaseName is the Helm release name for the k3k operator.
	k3kReleaseName = "k3k"
	// k3kSystemNamespace is the namespace where the k3k operator is installed.
	k3kSystemNamespace = "k3k-system"
	// k3kNamespacePrefix is the namespace prefix for k3k clusters.
	k3kNamespacePrefix = "k3k-"
	// k3kKubeconfigSecretSuffix is appended to the cluster name for the kubeconfig Secret.
	k3kKubeconfigSecretSuffix = "kubeconfig"
	// k3kKubeconfigKey is the key in the kubeconfig Secret.
	k3kKubeconfigKey = "kubeconfig.yaml"
	// k3kWaitTimeout is the default maximum time to wait for the k3k cluster to become
	// ready. Overridable via the KSAIL_NESTED_READY_TIMEOUT environment variable (see
	// nested.ReadyTimeoutEnvVar / k3kReadyTimeout) so CI can grant a slow-but-healthy
	// nested cluster more headroom under runner contention without changing the default.
	k3kWaitTimeout = 10 * time.Minute
	// k3kWaitInterval is the polling interval when waiting for the cluster.
	k3kWaitInterval = 5 * time.Second
	// k3kHelmTimeout is the timeout for Helm operations.
	k3kHelmTimeout = 5 * time.Minute
	// k3kAPIServerPort is the API server port used by k3k clusters.
	k3kAPIServerPort = 6443
	// k3kAPIServerPFTimeout bounds how long Create waits for a server pod to become
	// port-forwardable when wiring the in-session nested API endpoint.
	k3kAPIServerPFTimeout = 2 * time.Minute
)

// errNoRunningServerPod is recorded while polling for a port-forwardable k3k server
// pod, so a port-forward timeout surfaces "pod not ready" rather than a bare deadline.
var errNoRunningServerPod = errors.New("no running k3k server pod found")

// K3kProvisioner creates and manages K3s clusters on a host Kubernetes cluster
// using the rancher/k3k operator. Unlike the DinD approach used for Kind, k3k
// provisions K3s server/agent pods natively as Kubernetes workloads.
type K3kProvisioner struct {
	// Embed the standard K3d provisioner for component detection and interface compliance.
	*Provisioner

	hostClientset    kubernetes.Interface
	restConfig       *rest.Config
	k8sProvider      *kubernetesprovider.Provider
	dynamicClient    dynamic.Interface
	clusterName      string
	kubeconfigPath   string
	hostContext      string
	gatewayClassName string

	// k3k-specific configuration
	controlPlanes int32
	workers       int32
	podCIDR       string
	serviceCIDR   string
	serverArgs    []string
	// k3sVersion pins the nested K3s version (image tag, e.g. "v1.36.1-k3s1"). Empty
	// lets k3k inherit the host cluster's Kubernetes version. Pinned to the standalone
	// K3d version so the nested apiserver matches the proven standalone config (e.g. it
	// serves admissionregistration.k8s.io/v1beta1 for Calico's CRD chart).
	k3sVersion string

	// apiServerPortForward is a session port-forward to the nested k3k API server,
	// established during Create so in-session post-creation setup (e.g. CNI install)
	// can reach the nested API. The NodePort exposure address is not host-reachable on
	// common setups (kind does not map NodePorts to the host). It lives for the CLI
	// process and is closed on Delete.
	apiServerPortForward *kubernetesprovider.PortForwardSession
}

// K3kProvisionerConfig holds configuration for creating a K3kProvisioner.
type K3kProvisionerConfig struct {
	// K3dProvisioner is the inner K3d provisioner (used for component detection).
	K3dProvisioner *Provisioner
	// HostClientset is the Kubernetes clientset for the host cluster.
	HostClientset kubernetes.Interface
	// RestConfig is the REST config for the host cluster.
	RestConfig *rest.Config
	// K8sProvider is the Kubernetes infrastructure provider.
	K8sProvider *kubernetesprovider.Provider
	// DynamicClient is the dynamic client for Gateway API resources.
	DynamicClient dynamic.Interface
	// ClusterName is the nested cluster name.
	ClusterName string
	// KubeconfigPath is the path to store the nested cluster kubeconfig.
	KubeconfigPath string
	// GatewayClassName is the Gateway class for API exposure (empty = no gateway).
	GatewayClassName string
	// ControlPlanes is the number of K3s server pods.
	ControlPlanes int32
	// Workers is the number of K3s agent pods (virtual mode only).
	Workers int32
	// PodCIDR is the pod CIDR for the nested cluster.
	PodCIDR string
	// ServiceCIDR is the service CIDR for the nested cluster.
	ServiceCIDR string
	// HostContext is the kubeconfig context to use for the host cluster.
	HostContext string
	// ServerArgs are extra K3s server args (e.g. "--kube-apiserver-arg=...") passed
	// through to the embedded k3s server via the k3k Cluster spec's serverArgs field.
	ServerArgs []string
	// K3sVersion pins the nested K3s version (image tag, e.g. "v1.36.1-k3s1"). Empty
	// lets k3k inherit the host cluster's Kubernetes version.
	K3sVersion string
}

// NewK3kProvisioner creates a K3kProvisioner for managing K3s clusters via k3k.
func NewK3kProvisioner(cfg K3kProvisionerConfig) (*K3kProvisioner, error) {
	kubeconfigPath, err := k8s.ResolveKubeconfigPath(cfg.KubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	controlPlanes := cfg.ControlPlanes
	if controlPlanes <= 0 {
		controlPlanes = 1
	}

	workers := max(0, cfg.Workers)

	return &K3kProvisioner{
		Provisioner:      cfg.K3dProvisioner,
		hostClientset:    cfg.HostClientset,
		restConfig:       cfg.RestConfig,
		k8sProvider:      cfg.K8sProvider,
		dynamicClient:    cfg.DynamicClient,
		clusterName:      cfg.ClusterName,
		kubeconfigPath:   kubeconfigPath,
		hostContext:      cfg.HostContext,
		gatewayClassName: cfg.GatewayClassName,
		controlPlanes:    controlPlanes,
		workers:          workers,
		podCIDR:          cfg.PodCIDR,
		serviceCIDR:      cfg.ServiceCIDR,
		serverArgs:       cfg.ServerArgs,
		k3sVersion:       cfg.K3sVersion,
	}, nil
}

// Create provisions a K3s cluster using the k3k operator on the host Kubernetes cluster.
func (p *K3kProvisioner) Create(ctx context.Context, name string) error {
	clusterName := p.clusterName
	if clusterName == "" {
		clusterName = name
	}

	namespace := k3kNamespacePrefix + clusterName

	// Preserve the host kubeconfig's current-context (which MergeKubeconfig would otherwise
	// overwrite with the nested cluster) when the host is resolved from current-context. With an
	// explicit host context configured, leave the user pointed at the new nested cluster.
	restoreContext, err := k8s.PreserveCurrentContextUnlessExplicit(p.kubeconfigPath, p.hostContext)
	if err != nil {
		return fmt.Errorf("preserve host kubeconfig context: %w", err)
	}

	defer restoreContext()

	exposure, err := p.setupCluster(ctx, clusterName, namespace)
	if err != nil {
		return err
	}

	// Opt-in (KSAIL_NESTED_DEBUG) diagnostic: confirm the pinned K3s version and
	// feature-gate serverArgs actually reached the nested apiserver (Calico's CRD
	// chart needs admissionregistration.k8s.io/v1beta1 MutatingAdmissionPolicy).
	p.dumpNestedK3sDiagnostics(ctx, clusterName, namespace)

	// Persist the stable exposure URL in the kubeconfig (so it stays valid after the
	// CLI exits), and make it reachable for in-process post-creation setup (e.g.
	// installing Calico) by binding a session port-forward to that same NodePort. The
	// NodePort address is otherwise typically not host-reachable. Best-effort: if the
	// forward can't be established, the persisted endpoint is still the stable address
	// (default-CNI clusters don't need to reach the nested API during creation).
	serverURL := exposure.ServerURL()

	err = p.startAPIServerPortForward(ctx, clusterName, namespace, int(exposure.Port))
	if err != nil {
		_, _ = fmt.Fprintf(
			os.Stdout,
			"⚠ could not port-forward nested API server on port %d (%v); "+
				"post-creation CNI install may be unreachable\n",
			exposure.Port, err,
		)
	}

	err = p.connectAndMergeKubeconfig(ctx, clusterName, namespace, serverURL)
	if err != nil {
		// Avoid leaking the port-forward goroutine/listener if a later step fails.
		p.closeAPIServerPortForward()

		return err
	}

	_, _ = fmt.Fprintf(
		os.Stdout,
		"✓ k3k cluster %q ready (context: k3k-%s)\n",
		clusterName,
		clusterName,
	)

	return nil
}

// Delete removes the k3k cluster by deleting the Cluster CR and namespace.
func (p *K3kProvisioner) Delete(ctx context.Context, name string) error {
	clusterName := p.clusterName
	if clusterName == "" {
		clusterName = name
	}

	p.closeAPIServerPortForward()

	namespace := k3kNamespacePrefix + clusterName

	// Delete the namespace (cascading delete removes Cluster CR, pods, services, etc.)
	_, _ = fmt.Fprintf(os.Stdout, "► deleting namespace %s\n", namespace)

	err := p.hostClientset.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete namespace %s: %w", namespace, err)
	}

	// Clean up kubeconfig entries
	contextName := "k3k-" + clusterName
	_ = k8s.CleanupKubeconfig(p.kubeconfigPath, contextName, contextName, contextName, os.Stdout)

	_, _ = fmt.Fprintf(os.Stdout, "✓ k3k cluster %q deleted\n", clusterName)

	return nil
}

// Exists checks whether the k3k cluster namespace exists.
func (p *K3kProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	clusterName := p.clusterName
	if clusterName == "" {
		clusterName = name
	}

	namespace := k3kNamespacePrefix + clusterName

	//nolint:wrapcheck // helper already wraps with "check namespace" context
	return nested.NamespaceExists(ctx, p.hostClientset, namespace)
}

// Start is not supported for k3k clusters (pods are always running).
func (p *K3kProvisioner) Start(_ context.Context, _ string) error {
	return fmt.Errorf(
		"start not supported for k3k clusters (pods are managed by the k3k operator): %w",
		clustererr.ErrOperationNotSupported,
	)
}

// Stop is not supported for k3k clusters (pods are managed by the operator).
func (p *K3kProvisioner) Stop(_ context.Context, _ string) error {
	return fmt.Errorf(
		"stop not supported for k3k clusters (pods are managed by the k3k operator): %w",
		clustererr.ErrOperationNotSupported,
	)
}

// List returns all k3k cluster namespaces found on the host cluster.
func (p *K3kProvisioner) List(ctx context.Context) ([]string, error) {
	namespaces, err := p.hostClientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=ksail",
	})
	if err != nil {
		return nil, fmt.Errorf("list k3k namespaces: %w", err)
	}

	var names []string

	for _, ns := range namespaces.Items {
		if clusterName, ok := strings.CutPrefix(ns.Name, k3kNamespacePrefix); ok {
			names = append(names, clusterName)
		}
	}

	return names, nil
}

// Kubeconfig returns a kubeconfig for the named k3k cluster reachable from inside the host
// cluster (where the operator runs), with the API server rewritten to the in-cluster k3k server
// Service endpoint (https://k3k-<name>-service.k3k-<name>:443 — a DNS SAN on the served
// certificate, see ConnectionFor). The rewrite is required because k3k substitutes the CR's first
// TLS SAN (127.0.0.1 here) into the published kubeconfig's server URL, which is unreachable from
// an operator pod. It satisfies the clusterprovisioner.Connector capability and returns
// clustererr.ErrKubeconfigNotReady while the k3k-<name>-kubeconfig Secret has not been published
// yet.
func (p *K3kProvisioner) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	clusterName := p.clusterName
	if clusterName == "" {
		clusterName = name
	}

	conn := ConnectionFor(clusterName)

	raw, err := nested.ConnectorKubeconfig(
		ctx, p.hostClientset, clusterName,
		conn.Namespace, conn.SecretName, k3kKubeconfigKey,
	)
	if err != nil {
		return nil, fmt.Errorf("k3k: %w", err)
	}

	config, err := clientcmd.Load(raw)
	if err != nil {
		return nil, fmt.Errorf("parse k3k kubeconfig: %w", err)
	}

	for _, cluster := range config.Clusters {
		cluster.Server = conn.Endpoint
	}

	out, err := clientcmd.Write(*config)
	if err != nil {
		return nil, fmt.Errorf("serialize k3k kubeconfig: %w", err)
	}

	return out, nil
}

// connectAndMergeKubeconfig waits for the k3k kubeconfig Secret, rewrites the kubeconfig to
// point at the stable exposure address, and merges it into the host kubeconfig file.
func (p *K3kProvisioner) connectAndMergeKubeconfig(
	ctx context.Context,
	clusterName, namespace, serverURL string,
) error {
	_, _ = fmt.Fprintln(os.Stdout, "► waiting for kubeconfig secret")

	kubeconfigData, err := p.waitForKubeconfigSecret(ctx, clusterName, namespace)
	if err != nil {
		return fmt.Errorf("get kubeconfig secret: %w", err)
	}

	// k3k kubeconfig uses the ClusterIP/NodePort service address — rewrite it to the stable
	// exposure address the server cert SANs were issued for.
	kubeconfigStr := rewriteK3kKubeconfig(string(kubeconfigData), serverURL, clusterName)

	if p.kubeconfigPath != "" {
		mergeErr := k8s.MergeKubeconfig(p.kubeconfigPath, []byte(kubeconfigStr))
		if mergeErr != nil {
			return fmt.Errorf("merge kubeconfig: %w", mergeErr)
		}
	}

	return nil
}

// startAPIServerPortForward establishes a port-forward to a running k3k server pod's API
// port, bound to localPort on 127.0.0.1 so the stable exposure URL (https://127.0.0.1:
// <localPort>) is reachable in-process. The server certificate includes a 127.0.0.1 SAN
// (see buildClusterCR), so TLS verification succeeds over the forward. It polls for a
// Running server pod and retries the forward until one is ready, since the pod may still be
// coming up just after the cluster reports Ready.
func (p *K3kProvisioner) startAPIServerPortForward(
	ctx context.Context,
	clusterName, namespace string,
	localPort int,
) error {
	selector := fmt.Sprintf("cluster=%s,role=server", clusterName)

	var (
		session *kubernetesprovider.PortForwardSession
		lastErr error
	)

	err := wait.PollUntilContextTimeout(
		ctx, k3kWaitInterval, k3kAPIServerPFTimeout, true,
		func(ctx context.Context) (bool, error) {
			pods, listErr := p.hostClientset.CoreV1().
				Pods(namespace).
				List(ctx, metav1.ListOptions{LabelSelector: selector})
			if listErr != nil {
				return false, fmt.Errorf("list k3k server pods: %w", listErr)
			}

			podName := firstRunningPodName(pods.Items)
			if podName == "" {
				lastErr = fmt.Errorf("%w (selector %q)", errNoRunningServerPod, selector)

				return false, nil
			}

			pfSession, pfErr := p.k8sProvider.StartPortForwardInNamespaceOnLocalPort(
				ctx, p.restConfig, namespace, podName, k3kAPIServerPort, localPort,
			)
			if pfErr != nil {
				// The pod may not yet be accepting connections; record the cause and
				// keep polling rather than aborting so a not-yet-ready pod is retried.
				lastErr = pfErr

				//nolint:nilerr // intentional: transient port-forward failure → retry
				return false, nil
			}

			session = pfSession

			return true, nil
		},
	)
	if err != nil {
		if lastErr != nil {
			return fmt.Errorf(
				"port-forward nested API server: %w; last attempt: %w",
				err,
				lastErr,
			)
		}

		return fmt.Errorf("port-forward nested API server: %w", err)
	}

	p.apiServerPortForward = session

	return nil
}

// closeAPIServerPortForward tears down the nested API port-forward if one is active.
func (p *K3kProvisioner) closeAPIServerPortForward() {
	if p.apiServerPortForward != nil {
		p.apiServerPortForward.Close()
		p.apiServerPortForward = nil
	}
}

// dumpNestedK3sDiagnostics is an opt-in (KSAIL_NESTED_DEBUG) diagnostic that prints the
// k3k Cluster CR's Version/ServerArgs (what ksail requested) and the resolved k3s server
// pod's image + command/args (what the operator created). It reveals whether the pinned
// K3s version and feature-gate serverArgs actually reach the nested apiserver — e.g. so
// it serves admissionregistration.k8s.io/v1beta1 for Calico's CRD chart. No-op unless the
// env var is set.
func (p *K3kProvisioner) dumpNestedK3sDiagnostics(
	ctx context.Context,
	clusterName, namespace string,
) {
	if !nested.DebugEnabled() {
		return
	}

	writer := os.Stdout

	// What ksail asked the operator for.
	restClient, _, err := p.buildK3kRESTClient()
	if err == nil {
		cluster := &k3kv1beta1.Cluster{}

		getErr := restClient.Get().
			AbsPath("/apis/k3k.io/v1beta1").
			Namespace(namespace).
			Resource("clusters").
			Name(clusterName).
			Do(ctx).
			Into(cluster)
		if getErr == nil {
			_, _ = fmt.Fprintf(writer,
				"diagnostics: k3k Cluster %q spec.version=%q spec.serverArgs=%v\n",
				clusterName, cluster.Spec.Version, cluster.Spec.ServerArgs)
		}
	}

	// What the operator actually created: the image reveals the effective K3s version,
	// and the command/args reveal whether the feature-gate serverArgs reached the server.
	pods, err := p.hostClientset.CoreV1().
		Pods(namespace).
		List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("cluster=%s,role=server", clusterName),
		})
	if err != nil {
		_, _ = fmt.Fprintf(writer, "diagnostics: list k3k server pods failed: %v\n", err)

		return
	}

	for idx := range pods.Items {
		pod := pods.Items[idx]
		for cIdx := range pod.Spec.Containers {
			cnt := pod.Spec.Containers[cIdx]
			_, _ = fmt.Fprintf(writer,
				"diagnostics: server pod %q container %q image=%q command=%v args=%v\n",
				pod.Name, cnt.Name, cnt.Image, cnt.Command, cnt.Args)
		}
	}
}

// firstRunningPodName returns the name of the first Running pod, or "" if none are Running.
func firstRunningPodName(pods []corev1.Pod) string {
	for idx := range pods {
		if pods[idx].Status.Phase == corev1.PodRunning {
			return pods[idx].Name
		}
	}

	return ""
}

// setupCluster installs the k3k operator, creates the cluster namespace, resolves a stable
// server-side API exposure, creates the Cluster CR (with the exposure address in its cert SANs),
// and waits for the cluster to become ready. It returns the resolved exposure.
func (p *K3kProvisioner) setupCluster(
	ctx context.Context,
	clusterName, namespace string,
) (*kubernetesprovider.ExposureResult, error) {
	_, _ = fmt.Fprintln(os.Stdout, "► ensuring k3k operator is installed")

	err := p.ensureK3kOperator(ctx)
	if err != nil {
		return nil, fmt.Errorf("ensure k3k operator: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stdout, "► creating namespace %s\n", namespace)

	err = p.ensureNamespace(ctx, namespace)
	if err != nil {
		return nil, fmt.Errorf("ensure namespace: %w", err)
	}

	// Resolve a stable, server-side exposure (Gateway → NodePort) for the K3s API server. The
	// Service targets the k3k server pods, which the operator provisions once the Cluster CR is
	// applied; the address is assigned independently and survives the CLI exit. The LoadBalancer
	// tier is skipped so K3s klipper-lb never binds the API port on the host node.
	exposure, err := p.k8sProvider.ResolveExposure(
		ctx, p.dynamicClient,
		kubernetesprovider.APIExposureSpec{
			ClusterName:      clusterName,
			Namespace:        namespace,
			BackendSelector:  map[string]string{"cluster": clusterName, "role": "server"},
			APIPort:          k3kAPIServerPort,
			GatewayClassName: p.gatewayClassName,
			HostAddress:      p.restConfig.Host,
			SkipLoadBalancer: true,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("expose K3s API server: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stdout, "► creating k3k Cluster CR for %s\n", clusterName)

	err = p.createClusterCR(ctx, clusterName, namespace, exposure.Address)
	if err != nil {
		return nil, fmt.Errorf("create k3k cluster CR: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout, "► waiting for k3k cluster to become ready")

	err = p.waitForClusterReady(ctx, clusterName, namespace)
	if err != nil {
		p.dumpNestedK3sFailureDiagnostics(ctx, namespace)

		return nil, fmt.Errorf("wait for k3k cluster ready: %w", err)
	}

	return exposure, nil
}

// dumpNestedK3sFailureDiagnostics is an opt-in (KSAIL_NESTED_DEBUG) diagnostic that dumps the
// stuck nested cluster's pod states, events, and logs (the k3k server pods in the cluster
// namespace and the k3k operator in k3k-system) when the cluster never reaches Ready. It
// reveals why the embedded k3s server fails to start on a given host (image pull, scheduling,
// or crash). No-op unless the env var is set.
func (p *K3kProvisioner) dumpNestedK3sFailureDiagnostics(ctx context.Context, namespace string) {
	nested.DumpFailureDiagnostics(ctx, p.hostClientset, namespace, k3kSystemNamespace)
}

// ensureK3kOperator installs the k3k Helm chart if it isn't already present.
func (p *K3kProvisioner) ensureK3kOperator(ctx context.Context) error {
	helmClient, err := helm.NewClient(p.kubeconfigPath, p.hostContext)
	if err != nil {
		return fmt.Errorf("create helm client: %w", err)
	}

	// Check if k3k is already installed
	exists, err := helmClient.ReleaseExists(ctx, k3kReleaseName, k3kSystemNamespace)
	if err != nil {
		return fmt.Errorf("check k3k release: %w", err)
	}

	if exists {
		_, _ = fmt.Fprintln(os.Stdout, "  k3k operator already installed")

		return nil
	}

	// Install k3k via Helm
	_, _ = fmt.Fprintln(os.Stdout, "  installing k3k operator via Helm")

	repoConfig := helm.RepoConfig{
		Name:     "k3k",
		RepoName: "k3k",
		URL:      k3kChartRepo,
	}

	chartConfig := helm.ChartConfig{
		ReleaseName:     k3kReleaseName,
		ChartName:       k3kChartName,
		Namespace:       k3kSystemNamespace,
		CreateNamespace: true,
		RepoURL:         k3kChartRepo,
	}

	err = helm.InstallOrUpgradeChart(ctx, helmClient, repoConfig, chartConfig, k3kHelmTimeout)
	if err != nil {
		return fmt.Errorf("install k3k chart: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout, "  k3k operator installed successfully")

	return nil
}

// ensureNamespace creates the namespace if it doesn't exist, with ksail labels.
func (p *K3kProvisioner) ensureNamespace(ctx context.Context, namespace string) error {
	err := p.ensurePrivilegedPodGuard(ctx)
	if err != nil {
		return err
	}

	nsObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
			Labels: map[string]string{
				"ksail.io/managed-by": "ksail",
				"ksail.io/cluster":    p.clusterName,
				// The k3k server runs embedded k3s and requires a privileged container.
				// Hosts that enforce Pod Security Admission at the "baseline" level (e.g.
				// Talos, which defaults namespaces to baseline) would otherwise reject the
				// server StatefulSet's pod with a "violates PodSecurity baseline: privileged"
				// FailedCreate error, leaving the k3k cluster stuck Provisioning. The
				// admission guard created above scopes that namespace-wide PSA exemption so
				// arbitrary pods cannot use it for privileged host access.
				"pod-security.kubernetes.io/enforce": "privileged",
			},
		},
	}

	_, err := p.hostClientset.CoreV1().Namespaces().Create(ctx, nsObj, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("create namespace: %w", err)
	}

	return nil
}

func (p *K3kProvisioner) ensurePrivilegedPodGuard(ctx context.Context) error {
	name := p.privilegedPodGuardName()
	failurePolicy := admissionv1.Fail
	matchPolicy := admissionv1.Equivalent
	serverPodNamePrefix := fmt.Sprintf("k3k-%s-server-", p.clusterName)

	policy := &admissionv1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"ksail.io/managed-by": "ksail",
				"ksail.io/cluster":    p.clusterName,
			},
		},
		Spec: admissionv1.ValidatingAdmissionPolicySpec{
			FailurePolicy: &failurePolicy,
			MatchConstraints: &admissionv1.MatchResources{
				ResourceRules: []admissionv1.NamedRuleWithOperations{
					{
						RuleWithOperations: admissionv1.RuleWithOperations{
							Operations: []admissionv1.OperationType{admissionv1.Create, admissionv1.Update},
							Rule: admissionv1.Rule{
								APIGroups:   []string{""},
								APIVersions: []string{"v1"},
								Resources:   []string{"pods"},
							},
						},
					},
				},
				MatchPolicy: &matchPolicy,
			},
			Validations: []admissionv1.Validation{
				{
					Expression: fmt.Sprintf(
						"!variables.isUnsafePod || (object.metadata.name.startsWith('%s') && object.metadata.labels['cluster'] == '%s' && object.metadata.labels['role'] == 'server')",
						serverPodNamePrefix,
						p.clusterName,
					),
					Message: "only KSail-managed k3k server pods may use privileged or host-level pod settings in this namespace",
				},
			},
			Variables: []admissionv1.Variable{
				{
					Name:       "isUnsafePod",
					Expression: "object.spec.hostPID == true || object.spec.hostIPC == true || object.spec.hostNetwork == true || (has(object.spec.volumes) && object.spec.volumes.exists(v, has(v.hostPath))) || object.spec.containers.exists(c, has(c.securityContext) && c.securityContext.privileged == true) || (has(object.spec.initContainers) && object.spec.initContainers.exists(c, has(c.securityContext) && c.securityContext.privileged == true)) || (has(object.spec.ephemeralContainers) && object.spec.ephemeralContainers.exists(c, has(c.securityContext) && c.securityContext.privileged == true))",
				},
			},
		},
	}

	_, err := p.hostClientset.AdmissionregistrationV1().ValidatingAdmissionPolicies().Create(ctx, policy, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := p.hostClientset.AdmissionregistrationV1().ValidatingAdmissionPolicies().Get(
			ctx, name, metav1.GetOptions{},
		)
		if getErr != nil {
			return fmt.Errorf("get existing k3k privileged pod guard policy: %w", getErr)
		}

		policy.ResourceVersion = existing.ResourceVersion
		_, err = p.hostClientset.AdmissionregistrationV1().ValidatingAdmissionPolicies().Update(
			ctx, policy, metav1.UpdateOptions{},
		)
	}
	if err != nil {
		return fmt.Errorf("create k3k privileged pod guard policy: %w", err)
	}

	binding := &admissionv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"ksail.io/managed-by": "ksail",
				"ksail.io/cluster":    p.clusterName,
			},
		},
		Spec: admissionv1.ValidatingAdmissionPolicyBindingSpec{
			PolicyName: name,
			MatchResources: &admissionv1.MatchResources{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"ksail.io/managed-by": "ksail",
						"ksail.io/cluster":    p.clusterName,
					},
				},
			},
			ValidationActions: []admissionv1.ValidationAction{admissionv1.Deny},
		},
	}

	_, err = p.hostClientset.AdmissionregistrationV1().ValidatingAdmissionPolicyBindings().Create(ctx, binding, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := p.hostClientset.AdmissionregistrationV1().ValidatingAdmissionPolicyBindings().Get(
			ctx, name, metav1.GetOptions{},
		)
		if getErr != nil {
			return fmt.Errorf("get existing k3k privileged pod guard binding: %w", getErr)
		}

		binding.ResourceVersion = existing.ResourceVersion
		_, err = p.hostClientset.AdmissionregistrationV1().ValidatingAdmissionPolicyBindings().Update(
			ctx, binding, metav1.UpdateOptions{},
		)
	}
	if err != nil {
		return fmt.Errorf("create k3k privileged pod guard binding: %w", err)
	}

	return nil
}

func (p *K3kProvisioner) privilegedPodGuardName() string {
	name := fmt.Sprintf("ksail-k3k-%s-pod-security", p.clusterName)
	if len(name) <= 63 {
		return name
	}

	sum := sha256.Sum256([]byte(name))

	return fmt.Sprintf("%.54s-%x", name, sum[:4])
}

// buildClusterCR constructs the k3k Cluster custom resource with the provisioner's configuration.
// certSAN, when non-empty, is added to the server certificate SANs so the API server cert is
// valid for the stable exposure address. The Service is left as ClusterIP — external exposure is
// handled by the ksail-managed Service/Gateway resolved separately.
func (p *K3kProvisioner) buildClusterCR(
	clusterName, namespace, certSAN string,
) *k3kv1beta1.Cluster {
	tlsSANs := []string{"127.0.0.1", "localhost"}
	if certSAN != "" {
		tlsSANs = append(tlsSANs, certSAN)
	}

	cluster := &k3kv1beta1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "k3k.io/v1beta1",
			Kind:       "Cluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
			Labels: map[string]string{
				"ksail.io/managed-by": "ksail",
				"ksail.io/cluster":    clusterName,
			},
		},
		Spec: k3kv1beta1.ClusterSpec{
			Servers: &p.controlPlanes,
			Agents:  &p.workers,
			Mode:    k3kv1beta1.VirtualClusterMode,
			Persistence: k3kv1beta1.PersistenceConfig{
				Type: k3kv1beta1.EphemeralPersistenceMode,
			},
			TLSSANs: tlsSANs,
		},
	}

	if len(p.serverArgs) > 0 {
		cluster.Spec.ServerArgs = p.serverArgs
	}

	if p.k3sVersion != "" {
		cluster.Spec.Version = p.k3sVersion
	}

	if p.podCIDR != "" {
		cluster.Spec.ClusterCIDR = p.podCIDR
	}

	if p.serviceCIDR != "" {
		cluster.Spec.ServiceCIDR = p.serviceCIDR
	}

	return cluster
}

// createClusterCR creates the k3k Cluster custom resource.
func (p *K3kProvisioner) createClusterCR(
	ctx context.Context,
	clusterName, namespace, certSAN string,
) error {
	cluster := p.buildClusterCR(clusterName, namespace, certSAN)

	restClient, paramCodec, err := p.buildK3kRESTClient()
	if err != nil {
		return err
	}

	result := &k3kv1beta1.Cluster{}
	err = restClient.Post().
		AbsPath("/apis/k3k.io/v1beta1").
		Namespace(namespace).
		Resource("clusters").
		VersionedParams(&metav1.CreateOptions{}, paramCodec).
		Body(cluster).
		Do(ctx).
		Into(result)

	if apierrors.IsAlreadyExists(err) {
		_, _ = fmt.Fprintln(os.Stdout, "  k3k Cluster CR already exists")

		return nil
	}

	if err != nil {
		return fmt.Errorf("create cluster CR: %w", err)
	}

	return nil
}

// k3kReadyTimeout returns the readiness wait budget for nested k3k clusters,
// honoring the KSAIL_NESTED_READY_TIMEOUT override and falling back to k3kWaitTimeout.
func k3kReadyTimeout() time.Duration {
	return nested.ReadyTimeout(k3kWaitTimeout)
}

// waitForClusterReady polls the k3k Cluster status until it reports Ready.
func (p *K3kProvisioner) waitForClusterReady(
	ctx context.Context,
	clusterName, namespace string,
) error {
	restClient, _, err := p.buildK3kRESTClient()
	if err != nil {
		return err
	}

	err = wait.PollUntilContextTimeout(
		ctx, k3kWaitInterval, k3kReadyTimeout(), true,
		func(ctx context.Context) (bool, error) {
			cluster := &k3kv1beta1.Cluster{}

			err := restClient.Get().
				AbsPath("/apis/k3k.io/v1beta1").
				Namespace(namespace).
				Resource("clusters").
				Name(clusterName).
				Do(ctx).
				Into(cluster)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil
				}

				return false, fmt.Errorf("get cluster status: %w", err)
			}

			phase := cluster.Status.Phase
			switch phase { //nolint:exhaustive // default handles all non-Ready phases (e.g., Failed, Provisioning, Deleting)
			case k3kv1beta1.ClusterReady:
				return true, nil
			default:
				// k3k clusters can transiently enter Failed phase (e.g., bootstrap
				// secret timeout) before recovering. Keep polling until timeout.
				_, _ = fmt.Fprintf(os.Stdout, "  cluster phase: %s\n", phase)

				return false, nil
			}
		},
	)
	if err != nil {
		return fmt.Errorf("poll cluster ready: %w", err)
	}

	return nil
}

// buildK3kRESTClient creates a REST client configured for k3k CRD operations.
func (p *K3kProvisioner) buildK3kRESTClient() (*rest.RESTClient, runtime.ParameterCodec, error) {
	scheme := runtime.NewScheme()

	err := k3kv1beta1.AddToScheme(scheme)
	if err != nil {
		return nil, nil, fmt.Errorf("add k3k scheme: %w", err)
	}

	codecs := serializer.NewCodecFactory(scheme)
	paramCodec := runtime.NewParameterCodec(scheme)

	cfg := rest.CopyConfig(p.restConfig)
	cfg.ContentConfig = rest.ContentConfig{
		GroupVersion:         &k3kv1beta1.SchemeGroupVersion,
		NegotiatedSerializer: codecs,
	}
	cfg.APIPath = "/apis"

	restClient, err := rest.UnversionedRESTClientFor(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("create k3k REST client: %w", err)
	}

	return restClient, paramCodec, nil
}

// waitForKubeconfigSecret polls for the kubeconfig Secret created by the k3k operator.
func (p *K3kProvisioner) waitForKubeconfigSecret(
	ctx context.Context,
	clusterName, namespace string,
) ([]byte, error) {
	// k3k names the kubeconfig secret as k3k-<clusterName>-kubeconfig
	secretName := ConnectionFor(clusterName).SecretName

	//nolint:wrapcheck // helper already wraps with "wait for kubeconfig secret" context
	return nested.WaitForKubeconfigSecret(
		ctx, p.hostClientset, namespace, secretName, k3kKubeconfigKey,
		k3kWaitInterval, k3kReadyTimeout(),
	)
}

// rewriteK3kKubeconfig rewrites the k3k-generated kubeconfig to use the stable exposure
// address and renames context/cluster/user entries for uniqueness.
func rewriteK3kKubeconfig(kubeconfig, serverURL, clusterName string) string {
	// Replace the server address. The k3k kubeconfig uses the ClusterIP/NodePort address of the
	// service; rewrite it to the stable exposure address the cert SANs were issued for.
	lines := strings.Split(kubeconfig, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "server:") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = fmt.Sprintf("%sserver: %s", indent, serverURL)
		}
	}

	result := strings.Join(lines, "\n")

	// Rename context, cluster, and user from "default" to "k3k-<name>" for uniqueness
	contextName := "k3k-" + clusterName
	result = strings.ReplaceAll(result, "name: default", "name: "+contextName)
	result = strings.ReplaceAll(result, "cluster: default", "cluster: "+contextName)
	result = strings.ReplaceAll(result, "user: default", "user: "+contextName)
	result = strings.ReplaceAll(result, "current-context: default", "current-context: "+contextName)

	return result
}
