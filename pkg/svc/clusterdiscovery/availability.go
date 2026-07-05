package clusterdiscovery

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
)

// dockerPingTimeout bounds the Docker daemon reachability probe so a hung daemon does not stall the
// availability response.
const dockerPingTimeout = 3 * time.Second

// Availability reports whether a provider can be used for cluster creation on this machine, and a
// human-readable reason when it cannot. It feeds the web UI's create-form gating (only providers
// the backend can actually reach are offered).
type Availability struct {
	Provider  v1alpha1.Provider
	Available bool
	Reason    string
}

// Availability reports per-provider availability for the given providers, in order. Checks are
// intentionally cheap: credential/binary presence for cloud providers and a short ping for Docker,
// so this is safe to call when serving the UI config endpoint.
func (d *Discoverer) Availability(
	ctx context.Context,
	providers []v1alpha1.Provider,
) []Availability {
	out := make([]Availability, 0, len(providers))
	for _, prov := range providers {
		out = append(out, d.providerAvailability(ctx, prov))
	}

	return out
}

func (d *Discoverer) providerAvailability(
	ctx context.Context,
	prov v1alpha1.Provider,
) Availability {
	switch prov {
	case v1alpha1.ProviderDocker:
		return d.dockerAvailability(ctx)
	case v1alpha1.ProviderHetzner:
		return d.requireCredentials(prov, credentials.HetznerToken)
	case v1alpha1.ProviderOmni:
		return d.requireCredentials(
			prov,
			credentials.OmniEndpoint,
			credentials.OmniServiceAccountKey,
		)
	case v1alpha1.ProviderAWS:
		return d.awsAvailability()
	case v1alpha1.ProviderGCP:
		return d.gcpAvailability()
	case v1alpha1.ProviderAzure:
		return d.azureAvailability()
	case v1alpha1.ProviderKubernetes:
		return kubernetesAvailability()
	default:
		return Availability{Provider: prov, Available: false, Reason: "unsupported provider"}
	}
}

// dockerAvailability probes the Docker daemon. Docker is the local default, so a clear reason helps
// users who have not started Docker Desktop / the daemon.
func (d *Discoverer) dockerAvailability(ctx context.Context) Availability {
	err := d.pingDocker(ctx)
	if err != nil {
		return Availability{
			Provider:  v1alpha1.ProviderDocker,
			Available: false,
			Reason:    "Docker daemon is not reachable",
		}
	}

	return Availability{Provider: v1alpha1.ProviderDocker, Available: true}
}

func (d *Discoverer) pingDocker(ctx context.Context) error {
	if d.DockerPing != nil {
		return d.DockerPing(ctx)
	}

	cli, err := dockerclient.GetDockerClient()
	if err != nil {
		return fmt.Errorf("get docker client: %w", err)
	}

	defer func() { _ = cli.Close() }()

	pingCtx, cancel := context.WithTimeout(ctx, dockerPingTimeout)
	defer cancel()

	_, err = cli.Ping(pingCtx)
	if err != nil {
		return fmt.Errorf("ping docker daemon: %w", err)
	}

	return nil
}

// requireCredentials reports a credential-gated provider as available only when every required
// credential resolves to a non-empty value, naming the missing variables otherwise.
func (d *Discoverer) requireCredentials(
	prov v1alpha1.Provider,
	keys ...credentials.Key,
) Availability {
	resolver := d.resolver()

	var missing []string

	for _, key := range keys {
		if resolver.Value(key) == "" {
			missing = append(missing, resolver.EnvVar(key))
		}
	}

	if len(missing) > 0 {
		return Availability{
			Provider:  prov,
			Available: false,
			Reason: fmt.Sprintf(
				"not configured: set %s (or configure it in Settings)",
				strings.Join(missing, " and "),
			),
		}
	}

	return Availability{Provider: prov, Available: true}
}

// awsAvailability requires both AWS credentials and the eksctl binary, since EKS operations shell
// out to eksctl.
func (d *Discoverer) awsAvailability() Availability {
	if !d.eksctlAvailable() {
		return Availability{
			Provider:  v1alpha1.ProviderAWS,
			Available: false,
			Reason:    "eksctl is not installed or not on PATH",
		}
	}

	if !d.awsConfigured() {
		return Availability{
			Provider:  v1alpha1.ProviderAWS,
			Available: false,
			Reason:    "AWS credentials are not configured",
		}
	}

	return Availability{Provider: v1alpha1.ProviderAWS, Available: true}
}

// gcpAvailability requires a Google Cloud project (every GKE API call is project-scoped) and
// Application Default Credentials for the SDK to authenticate with.
func (d *Discoverer) gcpAvailability() Availability {
	projectAvailability := d.requireCredentials(v1alpha1.ProviderGCP, credentials.GCPProject)
	if !projectAvailability.Available {
		return projectAvailability
	}

	if !gcpADCPresent() {
		return Availability{
			Provider:  v1alpha1.ProviderGCP,
			Available: false,
			Reason:    "Google Cloud Application Default Credentials are not configured",
		}
	}

	return Availability{Provider: v1alpha1.ProviderGCP, Available: true}
}

// azureAvailability requires an Azure subscription (every ARM call is subscription-scoped) and a
// credential source the SDK's DefaultAzureCredential chain can resolve.
func (d *Discoverer) azureAvailability() Availability {
	subscriptionAvailability := d.requireCredentials(
		v1alpha1.ProviderAzure,
		credentials.AzureSubscriptionID,
	)
	if !subscriptionAvailability.Available {
		return subscriptionAvailability
	}

	if !azureCredentialPresent() {
		return Availability{
			Provider:  v1alpha1.ProviderAzure,
			Available: false,
			Reason:    "Azure credentials are not configured (az login or AZURE_* variables)",
		}
	}

	return Availability{Provider: v1alpha1.ProviderAzure, Available: true}
}

// kubernetesAvailability reports the nested-Kubernetes provider as available when a host kubeconfig
// file is present. It does not connect (that would be too costly for the config endpoint); an
// unreachable host simply yields no clusters during discovery.
func kubernetesAvailability() Availability {
	expandedPath, err := k8s.HostKubeconfigPath()
	if err != nil {
		return Availability{
			Provider:  v1alpha1.ProviderKubernetes,
			Available: false,
			Reason:    "no host kubeconfig found",
		}
	}

	_, statErr := os.Stat(expandedPath)
	if statErr != nil {
		return Availability{
			Provider:  v1alpha1.ProviderKubernetes,
			Available: false,
			Reason:    "no host kubeconfig found",
		}
	}

	return Availability{Provider: v1alpha1.ProviderKubernetes, Available: true}
}
