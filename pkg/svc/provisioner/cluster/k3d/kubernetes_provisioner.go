package k3dprovisioner

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	k3kv1beta1 "github.com/rancher/k3k/pkg/apis/k3k.io/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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
	// k3kWaitTimeout is the maximum time to wait for the k3k cluster to become ready.
	k3kWaitTimeout = 10 * time.Minute
	// k3kWaitInterval is the polling interval when waiting for the cluster.
	k3kWaitInterval = 5 * time.Second
	// k3kHelmTimeout is the timeout for Helm operations.
	k3kHelmTimeout = 5 * time.Minute
	// k3kAPIServerPort is the API server port used by k3k clusters.
	k3kAPIServerPort = 6443
)

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
	}, nil
}

// Create provisions a K3s cluster using the k3k operator on the host Kubernetes cluster.
//
//nolint:funlen // sequential setup steps
func (p *K3kProvisioner) Create(ctx context.Context, name string) error {
	clusterName := p.clusterName
	if clusterName == "" {
		clusterName = name
	}

	namespace := k3kNamespacePrefix + clusterName

	// Step 1: Ensure the k3k operator is installed
	_, _ = fmt.Fprintln(os.Stdout, "► ensuring k3k operator is installed")

	err := p.ensureK3kOperator(ctx)
	if err != nil {
		return fmt.Errorf("ensure k3k operator: %w", err)
	}

	// Step 2: Create the namespace for this cluster
	_, _ = fmt.Fprintf(os.Stdout, "► creating namespace %s\n", namespace)

	err = p.ensureNamespace(ctx, namespace)
	if err != nil {
		return fmt.Errorf("ensure namespace: %w", err)
	}

	// Step 2b: Resolve a stable, server-side exposure (Gateway → LoadBalancer → NodePort) for the
	// K3s API server. The Service targets the k3k server pods, which the operator provisions once
	// the Cluster CR is applied; the address is assigned independently and survives the CLI exit.
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
		return fmt.Errorf("expose K3s API server: %w", err)
	}

	// Step 3: Create the k3k Cluster CR (with the exposure address in the server cert SANs)
	_, _ = fmt.Fprintf(os.Stdout, "► creating k3k Cluster CR for %s\n", clusterName)

	err = p.createClusterCR(ctx, clusterName, namespace, exposure.Address)
	if err != nil {
		return fmt.Errorf("create k3k cluster CR: %w", err)
	}

	// Step 4: Wait for the cluster to become ready
	_, _ = fmt.Fprintln(os.Stdout, "► waiting for k3k cluster to become ready")

	err = p.waitForClusterReady(ctx, clusterName, namespace)
	if err != nil {
		return fmt.Errorf("wait for k3k cluster ready: %w", err)
	}

	// Step 5: Wait for the kubeconfig Secret to appear
	_, _ = fmt.Fprintln(os.Stdout, "► waiting for kubeconfig secret")

	kubeconfigData, err := p.waitForKubeconfigSecret(ctx, clusterName, namespace)
	if err != nil {
		return fmt.Errorf("get kubeconfig secret: %w", err)
	}

	// Step 6: Rewrite the kubeconfig to point at the stable exposure address.
	kubeconfigStr := rewriteK3kKubeconfig(string(kubeconfigData), exposure.ServerURL(), clusterName)

	// Step 7: Merge kubeconfig into the host kubeconfig file
	if p.kubeconfigPath != "" {
		err := k8s.MergeKubeconfig(p.kubeconfigPath, []byte(kubeconfigStr))
		if err != nil {
			return fmt.Errorf("merge kubeconfig: %w", err)
		}
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
//
// jscpd:ignore-start
func (p *K3kProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	clusterName := p.clusterName
	if clusterName == "" {
		clusterName = name
	}

	namespace := k3kNamespacePrefix + clusterName

	_, err := p.hostClientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("check namespace %s: %w", namespace, err)
	}

	return true, nil
}

// jscpd:ignore-end

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
	nsObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
			Labels: map[string]string{
				"ksail.io/managed-by": "ksail",
				"ksail.io/cluster":    p.clusterName,
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
		ctx, k3kWaitInterval, k3kWaitTimeout, true,
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

// jscpd:ignore-start
// waitForKubeconfigSecret polls for the kubeconfig Secret created by the k3k operator.
func (p *K3kProvisioner) waitForKubeconfigSecret(
	ctx context.Context,
	clusterName, namespace string,
) ([]byte, error) {
	// k3k names the kubeconfig secret as k3k-<clusterName>-kubeconfig
	secretName := fmt.Sprintf("k3k-%s-%s", clusterName, k3kKubeconfigSecretSuffix)

	var kubeconfigData []byte

	err := wait.PollUntilContextTimeout(
		ctx, k3kWaitInterval, k3kWaitTimeout, true,
		func(ctx context.Context) (bool, error) {
			secret, err := p.hostClientset.CoreV1().Secrets(namespace).Get(
				ctx, secretName, metav1.GetOptions{},
			)
			if apierrors.IsNotFound(err) {
				return false, nil
			}

			if err != nil {
				return false, fmt.Errorf("get kubeconfig secret: %w", err)
			}

			data, ok := secret.Data[k3kKubeconfigKey]
			if !ok || len(data) == 0 {
				return false, nil
			}

			kubeconfigData = data

			return true, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("wait for kubeconfig secret: %w", err)
	}

	return kubeconfigData, nil
}

// jscpd:ignore-end

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
