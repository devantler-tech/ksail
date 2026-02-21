package talosprovisioner

import (
	"context"
	"fmt"
	"io"
	"net"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/siderolabs/go-retry/retry"
	"github.com/siderolabs/talos/pkg/cluster/check"
	"github.com/siderolabs/talos/pkg/conditions"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"github.com/siderolabs/talos/pkg/machinery/config/bundle"
	"github.com/siderolabs/talos/pkg/provision"
	"github.com/siderolabs/talos/pkg/provision/access"
)

// bootstrapHetznerCluster bootstraps the etcd cluster on the first control-plane node.
func (p *Provisioner) bootstrapHetznerCluster(
	ctx context.Context,
	bootstrapNode *hcloud.Server,
	configBundle *bundle.Bundle,
) error {
	nodeIP := bootstrapNode.PublicNet.IPv4.IP.String()
	talosConfig := configBundle.TalosConfig()

	_, _ = fmt.Fprintf(
		p.logWriter,
		"  Waiting for %s to be ready for bootstrap...\n",
		bootstrapNode.Name,
	)

	// Wait for node to become ready after installation
	if err := p.waitForNodeReady(ctx, nodeIP, talosConfig); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Node %s is ready\n", bootstrapNode.Name)

	// Create authenticated client for bootstrap
	talosClient, err := p.createAuthenticatedClient(ctx, nodeIP, talosConfig)
	if err != nil {
		return err
	}
	defer talosClient.Close() //nolint:errcheck

	_, _ = fmt.Fprintf(p.logWriter, "  Bootstrapping etcd on %s...\n", bootstrapNode.Name)

	// Bootstrap etcd cluster
	if err := p.bootstrapEtcdCluster(ctx, talosClient); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Etcd cluster bootstrapped\n")
	_, _ = fmt.Fprintf(p.logWriter, "  Waiting for Kubernetes to be ready...\n")

	// Wait for Kubernetes to become ready
	if err := p.waitForKubernetesReady(ctx, talosClient); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Kubernetes is ready\n")

	return nil
}

// waitForNodeReady waits for a Talos node to be ready after installation.
// After config is applied, nodes will reboot. We need to wait for the Talos API
// to come back up with the applied configuration (authenticated mode).
func (p *Provisioner) waitForNodeReady(
	ctx context.Context,
	nodeIP string,
	talosConfig *clientconfig.Config,
) error {
	timeout := clusterReadinessTimeout

	err := retry.Constant(timeout, retry.WithUnits(longRetryInterval)).
		RetryWithContext(ctx, func(ctx context.Context) error {
			// Create authenticated client using talosconfig
			retryClient, clientErr := talosclient.New(ctx,
				talosclient.WithEndpoints(nodeIP),
				talosclient.WithConfig(talosConfig),
			)
			if clientErr != nil {
				return retry.ExpectedError(clientErr)
			}

			defer retryClient.Close() //nolint:errcheck

			// Try to get version to verify the node is ready
			_, versionErr := retryClient.Version(ctx)
			if versionErr != nil {
				return retry.ExpectedError(versionErr)
			}

			return nil
		})
	if err != nil {
		return fmt.Errorf("timeout waiting for node to be ready after installation: %w", err)
	}

	return nil
}

// createAuthenticatedClient creates an authenticated Talos client for the given node.
func (p *Provisioner) createAuthenticatedClient(
	ctx context.Context,
	nodeIP string,
	talosConfig *clientconfig.Config,
) (*talosclient.Client, error) {
	talosClient, err := talosclient.New(ctx,
		talosclient.WithEndpoints(nodeIP),
		talosclient.WithConfig(talosConfig),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Talos client: %w", err)
	}

	return talosClient, nil
}

// bootstrapEtcdCluster bootstraps the etcd cluster on the given node.
func (p *Provisioner) bootstrapEtcdCluster(
	ctx context.Context,
	talosClient *talosclient.Client,
) error {
	err := retry.Constant(bootstrapTimeout, retry.WithUnits(retryInterval)).
		RetryWithContext(ctx, func(ctx context.Context) error {
			bootstrapErr := talosClient.Bootstrap(ctx, &machineapi.BootstrapRequest{})
			if bootstrapErr != nil {
				// FailedPrecondition means the node isn't ready yet
				if talosclient.StatusCode(bootstrapErr) == grpcFailedPrecondition {
					return retry.ExpectedError(bootstrapErr)
				}

				return fmt.Errorf("bootstrap failed: %w", bootstrapErr)
			}

			return nil
		})
	if err != nil {
		return fmt.Errorf("failed to bootstrap cluster: %w", err)
	}

	return nil
}

// waitForKubernetesReady waits for Kubernetes to become ready by attempting to fetch the kubeconfig.
func (p *Provisioner) waitForKubernetesReady(
	ctx context.Context,
	talosClient *talosclient.Client,
) error {
	timeout := clusterReadinessTimeout

	err := retry.Constant(timeout, retry.WithUnits(longRetryInterval)).
		RetryWithContext(ctx, func(ctx context.Context) error {
			// Try to fetch kubeconfig as an indicator that K8s is ready
			_, kubeconfigErr := talosClient.Kubeconfig(ctx)
			if kubeconfigErr != nil {
				return retry.ExpectedError(kubeconfigErr)
			}

			return nil
		})
	if err != nil {
		return fmt.Errorf("timeout waiting for Kubernetes to be ready: %w", err)
	}

	return nil
}

// saveHetznerKubeconfig fetches and saves the kubeconfig from a Hetzner control-plane node.
func (p *Provisioner) saveHetznerKubeconfig(
	ctx context.Context,
	controlPlaneNode *hcloud.Server,
	configBundle *bundle.Bundle,
) error {
	nodeIP := controlPlaneNode.PublicNet.IPv4.IP.String()
	talosConfig := configBundle.TalosConfig()

	// Create authenticated client
	talosClient, err := talosclient.New(ctx,
		talosclient.WithEndpoints(nodeIP),
		talosclient.WithConfig(talosConfig),
	)
	if err != nil {
		return fmt.Errorf("failed to create Talos client: %w", err)
	}

	defer talosClient.Close() //nolint:errcheck

	// Fetch kubeconfig
	kubeconfig, err := talosClient.Kubeconfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch kubeconfig: %w", err)
	}

	// The kubeconfig from Talos uses internal IPs. For Hetzner, we need to use the public IP.
	// Rewrite the server endpoint to use the public IP.
	kubeconfig, err = rewriteKubeconfigEndpoint(
		kubeconfig,
		"https://"+net.JoinHostPort(nodeIP, "6443"),
	)
	if err != nil {
		return fmt.Errorf("failed to rewrite kubeconfig endpoint: %w", err)
	}

	// Write kubeconfig to the configured path
	err = p.writeKubeconfig(kubeconfig)
	if err != nil {
		return err
	}

	return nil
}

// newHetznerClusterWithEndpoint constructs a HetznerClusterResult with the
// Kubernetes API endpoint derived from the first control-plane server's public IPv4.
func newHetznerClusterWithEndpoint(
	clusterName string,
	controlPlaneServers []*hcloud.Server,
	workerServers []*hcloud.Server,
) (*HetznerClusterResult, error) {
	kubeEndpoint := "https://" + net.JoinHostPort(
		controlPlaneServers[0].PublicNet.IPv4.IP.String(),
		"6443",
	)

	return NewHetznerClusterResult(clusterName, controlPlaneServers, workerServers, kubeEndpoint)
}

// waitForHetznerClusterReady waits for the Hetzner cluster to be fully ready.
// This uses the upstream Talos SDK's access.NewAdapter() and check.Wait() patterns
// to perform the same readiness checks as Docker-based clusters.
//
// The checks performed depend on whether CNI is disabled:
//   - With CNI: Full checks including node Ready status
//   - Without CNI: PreBootSequence + K8sComponentsReadiness checks only
//
// This ensures the cluster is actually usable when creation returns.
func (p *Provisioner) waitForHetznerClusterReady(
	ctx context.Context,
	clusterName string,
	controlPlaneServers []*hcloud.Server,
	workerServers []*hcloud.Server,
	configBundle *bundle.Bundle,
) error {
	hetznerCluster, err := newHetznerClusterWithEndpoint(
		clusterName,
		controlPlaneServers,
		workerServers,
	)
	if err != nil {
		return fmt.Errorf("failed to create cluster result: %w", err)
	}

	// Get Talos config for authenticated client access
	talosConfig := configBundle.TalosConfig()

	// Create ClusterAccess adapter using upstream SDK pattern
	// This provides the full ClusterInfo interface (ClientProvider, K8sProvider, Info)
	clusterAccess := access.NewAdapter(
		hetznerCluster,
		provision.WithTalosConfig(talosConfig),
	)

	defer clusterAccess.Close() //nolint:errcheck

	// Determine which checks to run based on CNI configuration
	// When CNI is disabled, nodes won't become Ready until CNI is installed
	return p.runHetznerClusterChecks(ctx, clusterAccess)
}

// waitForHetznerClusterReadyAfterStart waits for a Hetzner cluster to be ready after starting.
// This is similar to waitForHetznerClusterReady but loads the TalosConfig from disk
// instead of from the config bundle (which is not available during start operations).
func (p *Provisioner) waitForHetznerClusterReadyAfterStart(
	ctx context.Context,
	clusterName string,
) error {
	_, _ = fmt.Fprintf(p.logWriter, "Waiting for cluster to be ready...\n")

	// Discover and classify servers by role
	controlPlaneServers, workerServers, err := p.discoverHetznerServers(ctx, clusterName)
	if err != nil {
		return err
	}

	// Build the cluster result from the discovered servers
	hetznerCluster, err := newHetznerClusterWithEndpoint(
		clusterName,
		controlPlaneServers,
		workerServers,
	)
	if err != nil {
		return fmt.Errorf("failed to create cluster result: %w", err)
	}

	// Load TalosConfig from disk (since we don't have the config bundle during start)
	talosConfig, err := clientconfig.Open("")
	if err != nil {
		return fmt.Errorf("failed to load talosconfig: %w", err)
	}

	// Create ClusterAccess adapter using upstream SDK pattern
	clusterAccess := access.NewAdapter(
		hetznerCluster,
		provision.WithTalosConfig(talosConfig),
	)

	defer clusterAccess.Close() //nolint:errcheck

	err = p.runHetznerClusterChecks(ctx, clusterAccess)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Cluster is ready\n")

	return nil
}

// discoverHetznerServers lists nodes for a cluster and classifies them by role,
// returning separate slices of control-plane and worker servers.
func (p *Provisioner) discoverHetznerServers(
	ctx context.Context,
	clusterName string,
) ([]*hcloud.Server, []*hcloud.Server, error) {
	nodes, err := p.infraProvider.ListNodes(ctx, clusterName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	if len(nodes) == 0 {
		return nil, nil, fmt.Errorf("%w: %s", clustererr.ErrNoNodesFound, clusterName)
	}

	hetznerProvider, ok := p.infraProvider.(*hetzner.Provider)
	if !ok {
		return nil, nil, clustererr.ErrNotHetznerProvider
	}

	var controlPlaneServers, workerServers []*hcloud.Server

	for _, node := range nodes {
		server, serverErr := hetznerProvider.GetServerByName(ctx, node.Name)
		if serverErr != nil {
			return nil, nil, fmt.Errorf("failed to get server %s: %w", node.Name, serverErr)
		}

		if server == nil {
			continue
		}

		if node.Role == RoleControlPlane {
			controlPlaneServers = append(controlPlaneServers, server)
		} else {
			workerServers = append(workerServers, server)
		}
	}

	if len(controlPlaneServers) == 0 {
		return nil, nil, fmt.Errorf("%w: %s", clustererr.ErrNoControlPlaneNodes, clusterName)
	}

	return controlPlaneServers, workerServers, nil
}

// runHetznerClusterChecks runs CNI-aware readiness checks on a Hetzner cluster.
// It selects the appropriate checks based on CNI configuration, logs progress,
// and waits for all checks to pass.
func (p *Provisioner) runHetznerClusterChecks(
	ctx context.Context,
	clusterAccess *access.Adapter,
) error {
	checks := p.clusterReadinessChecks()

	if (p.talosConfigs != nil && p.talosConfigs.IsCNIDisabled()) || p.options.SkipCNIChecks {
		_, _ = fmt.Fprintf(
			p.logWriter,
			"  Running pre-boot and K8s component checks (CNI not installed yet)...\n",
		)
	} else {
		_, _ = fmt.Fprintf(p.logWriter, "  Running full cluster readiness checks...\n")
	}

	reporter := &hetznerCheckReporter{writer: p.logWriter}

	checkCtx, cancel := context.WithTimeout(ctx, clusterReadinessTimeout)
	defer cancel()

	err := check.Wait(checkCtx, clusterAccess, checks, reporter)
	if err != nil {
		return fmt.Errorf("cluster readiness checks failed: %w", err)
	}

	return nil
}

// hetznerCheckReporter implements check.Reporter to log check progress.
type hetznerCheckReporter struct {
	writer   io.Writer
	lastLine string
}

func (r *hetznerCheckReporter) Update(condition conditions.Condition) {
	line := fmt.Sprintf("    %s", condition)
	if line != r.lastLine {
		_, _ = fmt.Fprintf(r.writer, "%s\n", line)
		r.lastLine = line
	}
}
