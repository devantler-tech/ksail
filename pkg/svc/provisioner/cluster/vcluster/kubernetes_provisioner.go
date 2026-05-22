package vclusterprovisioner

import (
	"context"
	"fmt"
	"os"
	"time"

	vclusterconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/vcluster"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	loftlog "github.com/loft-sh/log"
	"github.com/loft-sh/vcluster/pkg/cli"
	cliconfig "github.com/loft-sh/vcluster/pkg/cli/config"
	"github.com/loft-sh/vcluster/pkg/cli/find"
	"github.com/loft-sh/vcluster/pkg/cli/flags"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const (
	// vclusterNamespacePrefix is the namespace prefix for vCluster instances on Kubernetes.
	vclusterNamespacePrefix = "vcluster-"
	// vclusterSecretPrefix is the prefix for vCluster kubeconfig secrets ("vc-<name>").
	vclusterSecretPrefix = "vc-"
	// vclusterKubeconfigKey is the key within the kubeconfig Secret.
	vclusterKubeconfigKey = "config"
	// vclusterAPIServerPort is the API server port exposed by the vCluster pod.
	vclusterAPIServerPort = 8443
	// vclusterServiceAPIPort is the port the vCluster API Service exposes inside the host cluster.
	vclusterServiceAPIPort = 443
	// vclusterInClusterServerName is the TLS server name on the vCluster API server certificate. The
	// in-cluster Service DNS name is not a SAN, so the served certificate is verified against this
	// name (with the kubeconfig's CA) while connecting to the Service address.
	vclusterInClusterServerName = "kubernetes"
	// vclusterWaitTimeout is the maximum time to wait for vCluster readiness.
	vclusterWaitTimeout = 10 * time.Minute
	// vclusterWaitInterval is the polling interval when waiting for the cluster.
	vclusterWaitInterval = 5 * time.Second
)

// KubernetesProvisioner provisions vCluster instances on a host Kubernetes cluster
// using the vCluster SDK's Helm driver. Unlike the Docker-based Provisioner (Vind),
// this deploys vCluster as a standard Helm release into a namespace on the host cluster.
//
// Kubeconfig is extracted manually from the vc-<name> Secret and a port-forward is
// established to the vCluster pod, bypassing ConnectHelm which blocks indefinitely.
type KubernetesProvisioner struct {
	clusterName      string
	hostContext      string
	kubeconfigPath   string
	hostClientset    kubernetes.Interface
	restConfig       *rest.Config
	k8sProvider      *kubernetesprovider.Provider
	dynamicClient    dynamic.Interface
	gatewayClassName string
	valuesPath       string
	disableFlannel   bool
}

// KubernetesProvisionerConfig holds configuration for creating a KubernetesProvisioner.
type KubernetesProvisionerConfig struct {
	// ClusterName is the nested vCluster name.
	ClusterName string
	// HostContext is the kubeconfig context for the host Kubernetes cluster.
	HostContext string
	// KubeconfigPath is the path to the kubeconfig file.
	KubeconfigPath string
	// HostClientset is the Kubernetes client for the host cluster.
	HostClientset kubernetes.Interface
	// RestConfig is the REST config for the host cluster (needed for port-forwarding).
	RestConfig *rest.Config
	// K8sProvider is the Kubernetes infrastructure provider (needed for port-forwarding).
	K8sProvider *kubernetesprovider.Provider
	// DynamicClient is the dynamic client for Gateway API resources.
	DynamicClient dynamic.Interface
	// GatewayClassName is the Gateway class for API exposure (empty = no gateway).
	GatewayClassName string
	// ValuesPath is the optional path to a vcluster.yaml values file.
	ValuesPath string
	// DisableFlannel disables the built-in flannel CNI in the vCluster.
	DisableFlannel bool
}

// NewKubernetesProvisioner creates a KubernetesProvisioner for vCluster-on-Kubernetes.
func NewKubernetesProvisioner(cfg KubernetesProvisionerConfig) (*KubernetesProvisioner, error) {
	kubeconfigPath, err := k8s.ResolveKubeconfigPath(cfg.KubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	return &KubernetesProvisioner{
		clusterName:      cfg.ClusterName,
		hostContext:      cfg.HostContext,
		kubeconfigPath:   kubeconfigPath,
		hostClientset:    cfg.HostClientset,
		restConfig:       cfg.RestConfig,
		k8sProvider:      cfg.K8sProvider,
		dynamicClient:    cfg.DynamicClient,
		gatewayClassName: cfg.GatewayClassName,
		valuesPath:       cfg.ValuesPath,
		disableFlannel:   cfg.DisableFlannel,
	}, nil
}

// Create provisions a vCluster on the host Kubernetes cluster using the Helm driver.
// After chart installation, it manually extracts the kubeconfig from the vc-<name>
// Secret and sets up a port-forward to the vCluster pod (bypassing ConnectHelm which
// blocks indefinitely on port-forwarding).
//
//nolint:funlen,cyclop // sequential setup with many error-checks and branches
func (p *KubernetesProvisioner) Create(
	ctx context.Context,
	name string,
) error {
	// sequential setup steps
	clusterName := p.clusterName
	if clusterName == "" {
		clusterName = name
	}

	namespace := vclusterNamespacePrefix + clusterName

	// Preserve the host kubeconfig's current-context (which MergeKubeconfig would otherwise
	// overwrite with the nested cluster) when the host is resolved from current-context. With an
	// explicit host context configured, leave the user pointed at the new nested cluster.
	restoreContext, err := k8s.PreserveCurrentContextUnlessExplicit(p.kubeconfigPath, p.hostContext)
	if err != nil {
		return fmt.Errorf("preserve host kubeconfig context: %w", err)
	}

	defer restoreContext()

	// Step 1: Pre-create the namespace with KSail labels so it is discoverable
	// via `ksail cluster list --provider Kubernetes`. Setting CreateNamespace: false
	// below prevents the Helm driver from creating it without our labels.
	nsObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: kubernetesprovider.CommonLabels(clusterName),
		},
	}

	_, nsErr := p.hostClientset.CoreV1().Namespaces().Create(
		ctx, nsObj, metav1.CreateOptions{},
	)
	if nsErr != nil && !apierrors.IsAlreadyExists(nsErr) {
		return fmt.Errorf("pre-create vCluster namespace %s: %w", namespace, nsErr)
	}

	// Step 1b: Resolve a stable, server-side exposure (Gateway → NodePort) for the vCluster API
	// server. The LoadBalancer tier is skipped to avoid the host LB controller (e.g. K3s
	// klipper-lb) binding the API server port on the node. The Service targets the vCluster pods,
	// which the Helm release creates; the address is resolved up-front for the proxy cert SANs.
	exposure, err := p.k8sProvider.ResolveExposure(
		ctx, p.dynamicClient,
		kubernetesprovider.APIExposureSpec{
			ClusterName:      clusterName,
			Namespace:        namespace,
			BackendSelector:  map[string]string{"app": "vcluster", "release": clusterName},
			APIPort:          vclusterAPIServerPort,
			GatewayClassName: p.gatewayClassName,
			HostAddress:      p.restConfig.Host,
			SkipLoadBalancer: true,
		},
	)
	if err != nil {
		return fmt.Errorf("expose vCluster API server: %w", err)
	}

	// Step 2: Deploy the vCluster Helm chart
	opts := &cli.CreateOptions{
		ChartVersion:    vclusterconfigmanager.ChartVersion(),
		ChartName:       "vcluster",
		ChartRepo:       "https://charts.loft.sh",
		Connect:         false,
		CreateNamespace: false,
	}

	valuesFiles, cleanup, err := buildValuesFiles(p.valuesPath, p.disableFlannel, exposure.Address)
	if err != nil {
		return fmt.Errorf("prepare values files: %w", err)
	}
	defer cleanup()

	opts.Values = valuesFiles

	globalFlags := p.newHostGlobalFlags(namespace)
	logger := p.newLogger()

	_, _ = fmt.Fprintf(os.Stdout,
		"► creating vCluster %q in namespace %s on host context %s\n",
		clusterName, namespace, p.hostContext,
	)

	err = cli.CreateHelm(ctx, opts, globalFlags, clusterName, logger)
	if err != nil {
		return fmt.Errorf("create vCluster via Helm: %w", err)
	}

	// Step 3: Wait for the kubeconfig Secret to appear
	_, _ = fmt.Fprintln(os.Stdout, "► waiting for vCluster kubeconfig secret")

	kubeconfigData, err := p.waitForKubeconfigSecret(ctx, clusterName, namespace)
	if err != nil {
		return fmt.Errorf("get kubeconfig secret: %w", err)
	}

	// Step 4: Rewrite kubeconfig to point at the stable exposure address.
	contextName := "vcluster-" + clusterName

	rewrittenKubeconfig, err := rewriteVClusterKubeconfig(
		kubeconfigData, exposure.ServerURL(), clusterName,
	)
	if err != nil {
		return fmt.Errorf("rewrite vCluster kubeconfig: %w", err)
	}

	// Step 5: Merge kubeconfig into the host kubeconfig file
	if p.kubeconfigPath != "" {
		err := k8s.MergeKubeconfig(p.kubeconfigPath, rewrittenKubeconfig)
		if err != nil {
			return fmt.Errorf("merge kubeconfig: %w", err)
		}
	}

	_, _ = fmt.Fprintf(os.Stdout, "✓ vCluster %q ready (context: %s)\n", clusterName, contextName)

	return nil
}

// Delete removes the vCluster from the host cluster using the Helm driver.
func (p *KubernetesProvisioner) Delete(ctx context.Context, name string) error {
	clusterName := p.clusterName
	if clusterName == "" {
		clusterName = name
	}

	namespace := vclusterNamespacePrefix + clusterName

	deleteOpts := &cli.DeleteOptions{
		DeleteNamespace: true,
		DeleteContext:   true,
		IgnoreNotFound:  true,
	}

	globalFlags := p.newHostGlobalFlags(namespace)
	logger := p.newLogger()

	_, _ = fmt.Fprintf(
		os.Stdout,
		"► deleting vCluster %q from namespace %s\n",
		clusterName,
		namespace,
	)

	// platformClient is nil — no platform integration for local clusters.
	err := cli.DeleteHelm(ctx, nil, deleteOpts, globalFlags, clusterName, logger)
	if err != nil {
		return fmt.Errorf("delete vCluster via Helm: %w", err)
	}

	// Clean up kubeconfig entries
	contextName := "vcluster-" + clusterName
	_ = k8s.CleanupKubeconfig(p.kubeconfigPath, contextName, contextName, contextName, os.Stdout)

	_, _ = fmt.Fprintf(os.Stdout, "✓ vCluster %q deleted\n", clusterName)

	return nil
}

// Exists checks whether the vCluster namespace exists on the host cluster.
func (p *KubernetesProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	clusterName := p.clusterName
	if clusterName == "" {
		clusterName = name
	}

	namespace := vclusterNamespacePrefix + clusterName

	_, err := p.hostClientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("check namespace %s: %w", namespace, err)
	}

	// Also check that the vCluster release exists in the namespace
	globalFlags := p.newHostGlobalFlags(namespace)
	logger := p.newLogger()

	_, findErr := find.GetVCluster(ctx, globalFlags.Context, clusterName, namespace, logger)
	if findErr != nil {
		// GetVCluster returns an error when the vCluster is not found — this is
		// expected and means the cluster does not exist.
		return false, nil //nolint:nilerr // error means "not found"
	}

	return true, nil
}

// Kubeconfig returns a kubeconfig for the named vCluster reachable from inside the host cluster
// (where the operator runs), with the API server rewritten to the in-cluster Service address
// (https://<name>.vcluster-<name>.svc:443) and the TLS server name set to the cert's SAN. It
// satisfies the clusterprovisioner.Connector capability and returns clustererr.ErrKubeconfigNotReady
// while the vc-<name> Secret has not been published yet.
func (p *KubernetesProvisioner) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	clusterName := p.clusterName
	if clusterName == "" {
		clusterName = name
	}

	if clusterName == "" {
		return nil, fmt.Errorf("%w: vcluster name not set", clustererr.ErrConfigNil)
	}

	namespace := vclusterNamespacePrefix + clusterName
	secretName := vclusterSecretPrefix + clusterName

	secret, err := p.hostClientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, clustererr.ErrKubeconfigNotReady
	}

	if err != nil {
		return nil, fmt.Errorf("get vcluster kubeconfig secret %s/%s: %w", namespace, secretName, err)
	}

	raw := secret.Data[vclusterKubeconfigKey]
	if len(raw) == 0 {
		return nil, clustererr.ErrKubeconfigNotReady
	}

	config, err := clientcmd.Load(raw)
	if err != nil {
		return nil, fmt.Errorf("parse vcluster kubeconfig: %w", err)
	}

	endpoint := fmt.Sprintf("https://%s.%s.svc:%d", clusterName, namespace, vclusterServiceAPIPort)
	for _, cluster := range config.Clusters {
		cluster.Server = endpoint
		cluster.TLSServerName = vclusterInClusterServerName
	}

	out, err := clientcmd.Write(*config)
	if err != nil {
		return nil, fmt.Errorf("serialize vcluster kubeconfig: %w", err)
	}

	return out, nil
}

// Start is not supported for Helm-based vClusters (they run as pods).
func (p *KubernetesProvisioner) Start(_ context.Context, _ string) error {
	return fmt.Errorf(
		"start not supported for vCluster on Kubernetes (pods are always running): %w",
		clustererr.ErrOperationNotSupported,
	)
}

// Stop is not supported for Helm-based vClusters (they run as pods).
func (p *KubernetesProvisioner) Stop(_ context.Context, _ string) error {
	return fmt.Errorf("stop not supported for vCluster on Kubernetes (pods are always running): %w",
		clustererr.ErrOperationNotSupported)
}

// List returns all vCluster instances discovered by the SDK.
func (p *KubernetesProvisioner) List(ctx context.Context) ([]string, error) {
	logger := p.newLogger()

	vclusters, err := find.ListVClusters(ctx, p.hostContext, "", "", logger)
	if err != nil {
		return nil, fmt.Errorf("list vClusters: %w", err)
	}

	var names []string
	for _, vc := range vclusters {
		names = append(names, vc.Name)
	}

	return names, nil
}

// waitForKubeconfigSecret polls for the vc-<name> Secret until it contains
// the kubeconfig data, matching the approach used by the vCluster SDK itself.
func (p *KubernetesProvisioner) waitForKubeconfigSecret(
	ctx context.Context, clusterName, namespace string,
) ([]byte, error) {
	secretName := vclusterSecretPrefix + clusterName

	var kubeconfigData []byte

	err := wait.PollUntilContextTimeout(
		ctx, vclusterWaitInterval, vclusterWaitTimeout, true,
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

			data, ok := secret.Data[vclusterKubeconfigKey]
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

// rewriteVClusterKubeconfig parses the vCluster-generated kubeconfig, rewrites the
// server URL to use the stable exposure address, and renames all entries for uniqueness.
func rewriteVClusterKubeconfig(
	kubeconfigBytes []byte,
	serverURL string,
	clusterName string,
) ([]byte, error) {
	config, err := clientcmd.Load(kubeconfigBytes)
	if err != nil {
		return nil, fmt.Errorf("parse vCluster kubeconfig: %w", err)
	}

	contextName := "vcluster-" + clusterName

	// Build a new config with renamed entries
	newConfig := clientcmdapi.NewConfig()
	newConfig.CurrentContext = contextName

	// Copy and rename cluster entries
	for _, cluster := range config.Clusters {
		newCluster := cluster.DeepCopy()
		newCluster.Server = serverURL
		newConfig.Clusters[contextName] = newCluster
	}

	// Copy and rename auth info (user) entries
	for _, authInfo := range config.AuthInfos {
		newConfig.AuthInfos[contextName] = authInfo.DeepCopy()
	}

	// Create the new context
	newConfig.Contexts[contextName] = &clientcmdapi.Context{
		Cluster:  contextName,
		AuthInfo: contextName,
	}

	out, err := clientcmd.Write(*newConfig)
	if err != nil {
		return nil, fmt.Errorf("serialize rewritten kubeconfig: %w", err)
	}

	return out, nil
}

// newHostGlobalFlags creates global flags configured for the host cluster.
func (p *KubernetesProvisioner) newHostGlobalFlags(namespace string) *flags.GlobalFlags {
	configPath, err := cliconfig.DefaultFilePath()
	if err != nil {
		configPath = ""
	}

	return &flags.GlobalFlags{
		Config:    configPath,
		Context:   p.hostContext,
		Namespace: namespace,
	}
}

// newLogger creates a loft-sh/log Logger that writes to stdout.
func (p *KubernetesProvisioner) newLogger() loftlog.Logger {
	return loftlog.NewStreamLogger(os.Stdout, os.Stderr, logrus.InfoLevel)
}
