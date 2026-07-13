package clusterdiscovery

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	aksclient "github.com/devantler-tech/ksail/v7/pkg/client/aks"
	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	gkeclient "github.com/devantler-tech/ksail/v7/pkg/client/gke"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	awsprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/aws"
	azureprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/azure"
	gcpprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/gcp"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/omni"
	omniclient "github.com/siderolabs/omni/client/pkg/client"
	"k8s.io/client-go/kubernetes"
)

// listHetzner lists Talos clusters on Hetzner Cloud. With no token configured it skips silently.
func (d *Discoverer) listHetzner(ctx context.Context) ([]Cluster, error) {
	lister := d.Hetzner
	if lister == nil {
		token := d.resolver().Value(credentials.HetznerToken)
		if token == "" {
			return nil, nil
		}

		lister = hetzner.NewProviderFromToken(token)
	}

	names, err := lister.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("query Hetzner: %w", err)
	}

	return clustersWithDistribution(
		names,
		v1alpha1.DistributionTalos,
		v1alpha1.ProviderHetzner,
	), nil
}

// listOmni lists Talos clusters managed by Sidero Omni. Without both an endpoint and a service
// account key it skips silently.
func (d *Discoverer) listOmni(ctx context.Context) ([]Cluster, error) {
	lister := d.Omni
	if lister == nil {
		endpoint := d.resolver().Value(credentials.OmniEndpoint)
		serviceAccountKey := d.resolver().Value(credentials.OmniServiceAccountKey)

		if endpoint == "" || serviceAccountKey == "" {
			return nil, nil
		}

		client, err := omniclient.New(endpoint, omniclient.WithServiceAccount(serviceAccountKey))
		if err != nil {
			return nil, fmt.Errorf("create Omni client: %w", err)
		}

		lister = omni.NewProvider(client)
	}

	names, err := lister.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("query Omni: %w", err)
	}

	return clustersWithDistribution(names, v1alpha1.DistributionTalos, v1alpha1.ProviderOmni), nil
}

// listAWS lists EKS clusters. It skips silently unless AWS appears configured and the eksctl binary
// is on PATH, so the common no-AWS case costs nothing and never emits a warning.
func (d *Discoverer) listAWS(ctx context.Context) ([]Cluster, error) {
	lister := d.AWS
	if lister == nil {
		if !d.awsConfigured() || !d.eksctlAvailable() {
			return nil, nil
		}

		auth := credentials.ResolveAWS(d.resolver())
		eksctlOptions := credentials.OptionsForAWSChildEnvironment(
			auth, os.Environ(), eksctlclient.WithEnvironment, eksctlclient.RequireCredentialValues,
		)
		providerOptions := credentials.OptionsForAWSResolution(
			auth, awsprovider.WithCredentialValues, awsprovider.RequireCredentialValues,
		)

		client := eksctlclient.NewClient(eksctlOptions...)

		provider, err := awsprovider.NewProvider(
			client,
			d.resolver().Value(credentials.AWSRegion),
			providerOptions...,
		)
		if err != nil {
			return nil, fmt.Errorf("create AWS provider: %w", err)
		}

		lister = provider
	}

	names, err := lister.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("query EKS: %w", err)
	}

	return clustersWithDistribution(names, v1alpha1.DistributionEKS, v1alpha1.ProviderAWS), nil
}

// listGCP lists GKE clusters. It skips silently unless GCP appears configured (a project plus
// Application Default Credentials), so the common no-GCP case costs nothing and never emits a
// warning.
func (d *Discoverer) listGCP(ctx context.Context) ([]Cluster, error) {
	lister := d.GCP
	if lister == nil {
		if !d.gcpConfigured() {
			return nil, nil
		}

		client, err := gkeclient.NewClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("create GKE client: %w", err)
		}

		defer func() { _ = client.Close() }()

		provider, err := gcpprovider.NewProvider(
			client,
			d.resolver().Value(credentials.GCPProject),
			d.resolver().Value(credentials.GCPLocation),
		)
		if err != nil {
			return nil, fmt.Errorf("create GCP provider: %w", err)
		}

		lister = provider
	}

	names, err := lister.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("query GKE: %w", err)
	}

	return clustersWithDistribution(names, v1alpha1.DistributionGKE, v1alpha1.ProviderGCP), nil
}

// listAzure lists AKS clusters. It skips silently unless Azure appears configured (a subscription
// plus a resolvable credential source), so the common no-Azure case costs nothing and never emits
// a warning.
func (d *Discoverer) listAzure(ctx context.Context) ([]Cluster, error) {
	lister := d.Azure
	if lister == nil {
		if !d.azureConfigured() {
			return nil, nil
		}

		client, err := aksclient.NewClient(d.resolver().Value(credentials.AzureSubscriptionID))
		if err != nil {
			return nil, fmt.Errorf("create AKS client: %w", err)
		}

		provider, err := azureprovider.NewProvider(
			client,
			d.resolver().Value(credentials.AzureResourceGroup),
		)
		if err != nil {
			return nil, fmt.Errorf("create Azure provider: %w", err)
		}

		lister = provider
	}

	names, err := lister.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("query AKS: %w", err)
	}

	return clustersWithDistribution(names, v1alpha1.DistributionAKS, v1alpha1.ProviderAzure), nil
}

// listKubernetes lists clusters nested inside a host Kubernetes cluster. With no reachable host
// kubeconfig it skips silently.
func (d *Discoverer) listKubernetes(ctx context.Context) ([]Cluster, error) {
	lister := d.Kubernetes
	if lister == nil {
		provider, err := kubernetesProviderFromConfig()
		if err != nil {
			return nil, err
		}

		if provider == nil {
			return nil, nil
		}

		lister = provider
	}

	infos, err := lister.ListAllClustersWithDistribution(ctx)
	if err != nil {
		return nil, fmt.Errorf("query Kubernetes host: %w", err)
	}

	return kubernetesClusters(infos), nil
}

// kubernetesClusters converts the provider's ClusterInfo to discovered clusters, mapping the
// detected distribution string to its enum (falling back to Vanilla on an unknown value).
func kubernetesClusters(infos []kubernetesprovider.ClusterInfo) []Cluster {
	clusters := make([]Cluster, 0, len(infos))

	for _, info := range infos {
		var distribution v1alpha1.Distribution

		err := distribution.Set(info.Distribution)
		if err != nil {
			distribution = v1alpha1.DistributionVanilla
		}

		clusters = append(clusters, Cluster{
			Name:         info.Name,
			Distribution: distribution,
			Provider:     v1alpha1.ProviderKubernetes,
		})
	}

	return clusters
}

// awsConfigured reports whether AWS credentials appear set up: a complete pair of static keys or a
// named profile in the environment, or a shared credentials/config file under ~/.aws. Static
// credentials need BOTH the access key ID and the secret access key — neither alone lets eksctl
// authenticate — so a lone access key ID does not count. Region alone does not count either.
func (d *Discoverer) awsConfigured() bool {
	resolver := d.resolver()

	hasStaticKeys := resolver.Value(credentials.AWSAccessKeyID) != "" &&
		resolver.Value(credentials.AWSSecretAccessKey) != ""
	if hasStaticKeys || resolver.Value(credentials.AWSProfile) != "" {
		return true
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	for _, name := range []string{"credentials", "config"} {
		_, statErr := os.Stat(filepath.Join(home, ".aws", name))
		if statErr == nil {
			return true
		}
	}

	return false
}

// gcpConfigured reports whether GCP appears set up for GKE discovery: a project ID (which the GKE
// API scopes every list call to) plus Application Default Credentials. The project check comes
// first so an unset project skips without touching the host's credential files.
func (d *Discoverer) gcpConfigured() bool {
	if d.resolver().Value(credentials.GCPProject) == "" {
		return false
	}

	return gcpADCPresent()
}

// gcpADCPresent reports whether Google Application Default Credentials appear available: the
// GOOGLE_APPLICATION_CREDENTIALS variable is set, or gcloud's well-known ADC file exists. This is
// a presence probe only — the SDK performs the real credential resolution when a client is built.
func gcpADCPresent() bool {
	if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" {
		return true
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	_, statErr := os.Stat(
		filepath.Join(home, ".config", "gcloud", "application_default_credentials.json"),
	)

	return statErr == nil
}

// azureConfigured reports whether Azure appears set up for AKS discovery: a subscription ID (which
// every ARM list call is scoped to) plus a credential source. The subscription check comes first so
// an unset subscription skips without touching the host's credential files.
func (d *Discoverer) azureConfigured() bool {
	if d.resolver().Value(credentials.AzureSubscriptionID) == "" {
		return false
	}

	return azureCredentialPresent()
}

// azureCredentialPresent reports whether a credential source for the SDK's DefaultAzureCredential
// chain appears available: environment credentials (AZURE_CLIENT_ID / AZURE_TENANT_ID), or an
// Azure CLI profile under ~/.azure. This is a presence probe only — the SDK performs the real
// credential resolution when a client is built (managed-identity-only environments can still list
// by injecting a lister or setting the environment variables).
func azureCredentialPresent() bool {
	if os.Getenv("AZURE_CLIENT_ID") != "" || os.Getenv("AZURE_TENANT_ID") != "" {
		return true
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	_, statErr := os.Stat(filepath.Join(home, ".azure", "azureProfile.json"))

	return statErr == nil
}

// eksctlAvailable reports whether the eksctl binary (which the AWS provider shells out to) is on
// PATH. AWS discovery is skipped when it is not, to avoid emitting a warning on every poll.
func (d *Discoverer) eksctlAvailable() bool {
	lookPath := d.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	_, err := lookPath("eksctl")

	return err == nil
}

// kubernetesProviderFromConfig builds a host Kubernetes provider from KSAIL_HOST_KUBECONFIG /
// KSAIL_HOST_CONTEXT (or the default kubeconfig). It returns (nil, nil) when no usable host
// kubeconfig is present so the caller skips Kubernetes discovery silently. Mirrors the resolution
// `ksail cluster list` used before this logic was centralized here.
func kubernetesProviderFromConfig() (*kubernetesprovider.Provider, error) {
	expandedPath, err := k8s.HostKubeconfigPath()
	if err != nil {
		return nil, fmt.Errorf("expand kubeconfig path: %w", err)
	}

	// Skip silently when the kubeconfig file does not exist.
	_, statErr := os.Stat(expandedPath)
	if os.IsNotExist(statErr) {
		return nil, nil //nolint:nilnil // "no host kubeconfig" is a skip, not an error.
	}

	canonicalPath, err := fsutil.EvalCanonicalPath(expandedPath)
	if err != nil {
		return nil, fmt.Errorf("canonicalize kubeconfig path: %w", err)
	}

	restConfig, err := k8s.BuildRESTConfig(canonicalPath, os.Getenv("KSAIL_HOST_CONTEXT"))
	if err != nil {
		// Present but invalid/unreachable host cluster: skip silently.
		return nil, nil //nolint:nilnil,nilerr // intentionally swallow: host cluster unavailable.
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}

	provider, err := kubernetesprovider.NewProvider(clientset, v1alpha1.OptionsKubernetes{})
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes provider: %w", err)
	}

	return provider, nil
}
