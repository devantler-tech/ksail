package talosprovisioner

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	iopath "github.com/devantler-tech/ksail/v5/pkg/io"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/hetzner"
	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/siderolabs/go-retry/retry"
	"github.com/siderolabs/talos/pkg/cluster/check"
	"github.com/siderolabs/talos/pkg/conditions"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/bundle"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/provision"
	"github.com/siderolabs/talos/pkg/provision/access"
	"github.com/siderolabs/talos/pkg/provision/providers"
	"k8s.io/client-go/tools/clientcmd"
)

// TalosProviderName is the name used by the Talos SDK for Docker provisioner.
const TalosProviderName = "docker"

// Docker label keys used by Talos provisioner.
const (
	// LabelTalosOwned marks a container as owned by Talos provisioner.
	LabelTalosOwned = "talos.owned"
	// LabelTalosClusterName identifies which cluster a container belongs to.
	LabelTalosClusterName = "talos.cluster.name"
)

// Default resource values for nodes.
const (
	defaultNodeMemory = 2 * 1024 * 1024 * 1024 // 2GB
	defaultNodeCPUs   = 2 * 1000 * 1000 * 1000 // 2 CPU cores
	defaultMTU        = 1500
	// ipv4Offset is the offset from gateway for node IPs (gateway is .1, nodes start at .2).
	ipv4Offset = 2
	// stateDirectoryPermissions is the permissions for the state directory.
	stateDirectoryPermissions = 0o750
	// kubeconfigFileMode is the file mode for kubeconfig files.
	kubeconfigFileMode = 0o600
	// clusterReadinessTimeout is the timeout for waiting for the cluster to become ready.
	// This matches the upstream talosctl default of 10 minutes.
	clusterReadinessTimeout = 10 * time.Minute
)

// IP byte shift constants for IPv4 address manipulation.
const (
	byte0Shift = 24
	byte1Shift = 16
	byte2Shift = 8
)

// Common errors for the Talos provisioner.
var (
	// ErrDockerNotAvailable is returned when Docker is not available.
	ErrDockerNotAvailable = errors.New("docker is not available: ensure Docker is running")
	// ErrClusterAlreadyExists is returned when attempting to create a cluster that already exists.
	ErrClusterAlreadyExists = errors.New("cluster already exists")
	// ErrInvalidPatch is returned when a patch file is invalid.
	ErrInvalidPatch = errors.New("invalid patch file")
	// ErrNotImplemented is returned when a method is not yet implemented.
	ErrNotImplemented = errors.New("not implemented")
	// ErrIPv6NotSupported is returned when IPv6 addresses are used but not supported.
	ErrIPv6NotSupported = errors.New("IPv6 not supported")
	// ErrNegativeOffset is returned when a negative offset is provided for IP calculation.
	ErrNegativeOffset = errors.New("negative offset not allowed")
	// ErrNoControlPlane is returned when no control plane container is found.
	ErrNoControlPlane = errors.New("no control plane container found")
	// ErrNoPortMapping is returned when no port mapping is found for a required port.
	ErrNoPortMapping = errors.New("no port mapping found")
	// ErrHetznerProviderRequired is returned when the Hetzner provider is expected but not available.
	ErrHetznerProviderRequired = errors.New("hetzner provider required for this operation")
	// ErrMissingKubernetesEndpoint is returned when the cluster info is missing the Kubernetes endpoint.
	ErrMissingKubernetesEndpoint = errors.New("cluster info missing KubernetesEndpoint")
	// ErrKernelModuleLoadFailed is returned when loading a required kernel module fails.
	ErrKernelModuleLoadFailed = errors.New("failed to load kernel module")
)

// TalosProvisioner implements ClusterProvisioner for Talos-in-Docker clusters.
type TalosProvisioner struct {
	// talosConfigs holds the loaded Talos machine configurations with all patches applied.
	talosConfigs *talosconfigmanager.Configs
	// options holds runtime configuration for provisioning.
	options *Options
	// dockerClient is used for Docker-specific operations (volume cleanup, port inspection).
	dockerClient dockerclient.APIClient
	// infraProvider is the infrastructure provider for node operations (start/stop).
	// If nil, falls back to dockerClient for backwards compatibility.
	infraProvider provider.Provider
	// talosOpts holds Talos-specific options (node counts, cloud ISO, etc.).
	talosOpts *v1alpha1.OptionsTalos
	// hetznerOpts holds Hetzner-specific options when using the Hetzner provider.
	hetznerOpts        *v1alpha1.OptionsHetzner
	provisionerFactory func(ctx context.Context) (provision.Provisioner, error)
	logWriter          io.Writer
}

// NewTalosProvisioner creates a new TalosProvisioner.
// The talosConfigs parameter contains the pre-loaded Talos machine configurations
// with all patches (file-based and runtime) already applied.
// The options parameter contains runtime settings like node counts and output paths.
func NewTalosProvisioner(
	talosConfigs *talosconfigmanager.Configs,
	options *Options,
) *TalosProvisioner {
	if options == nil {
		options = NewOptions()
	}

	return &TalosProvisioner{
		talosConfigs: talosConfigs,
		options:      options,
		provisionerFactory: func(ctx context.Context) (provision.Provisioner, error) {
			return providers.Factory(ctx, TalosProviderName)
		},
		logWriter: os.Stdout,
	}
}

// WithDockerClient sets the Docker client for container operations.
func (p *TalosProvisioner) WithDockerClient(c dockerclient.APIClient) *TalosProvisioner {
	p.dockerClient = c

	return p
}

// WithProvisionerFactory sets a custom provisioner factory for testing.
func (p *TalosProvisioner) WithProvisionerFactory(
	f func(ctx context.Context) (provision.Provisioner, error),
) *TalosProvisioner {
	p.provisionerFactory = f

	return p
}

// WithLogWriter sets the log writer for provisioning output.
func (p *TalosProvisioner) WithLogWriter(w io.Writer) *TalosProvisioner {
	p.logWriter = w

	return p
}

// WithInfraProvider sets the infrastructure provider for node operations.
func (p *TalosProvisioner) WithInfraProvider(prov provider.Provider) *TalosProvisioner {
	p.infraProvider = prov

	return p
}

// WithHetznerOptions sets the Hetzner-specific options for cloud provisioning.
func (p *TalosProvisioner) WithHetznerOptions(opts v1alpha1.OptionsHetzner) *TalosProvisioner {
	p.hetznerOpts = &opts

	return p
}

// WithTalosOptions sets the Talos-specific options (node counts, cloud ISO, etc.).
func (p *TalosProvisioner) WithTalosOptions(opts v1alpha1.OptionsTalos) *TalosProvisioner {
	p.talosOpts = &opts

	return p
}

// SetProvider sets the infrastructure provider for node operations.
// This implements the ProviderAware interface.
func (p *TalosProvisioner) SetProvider(prov provider.Provider) {
	p.infraProvider = prov
}

// Options returns the current runtime options.
func (p *TalosProvisioner) Options() *Options {
	return p.options
}

// TalosConfigs returns the loaded Talos machine configurations.
func (p *TalosProvisioner) TalosConfigs() *talosconfigmanager.Configs {
	return p.talosConfigs
}

// Create creates a Talos cluster.
// If name is non-empty, it overrides the cluster name from talosConfigs.
// Routes to Docker-based or Hetzner-based provisioning based on configuration.
func (p *TalosProvisioner) Create(ctx context.Context, name string) error {
	clusterName := p.resolveClusterName(name)

	// Route to Hetzner-based provisioning if Hetzner options are set
	if p.hetznerOpts != nil {
		return p.createHetznerCluster(ctx, clusterName)
	}

	// Docker-based provisioning (default)
	return p.createDockerCluster(ctx, clusterName)
}

// createDockerCluster creates a Talos-in-Docker cluster using the Talos SDK.
func (p *TalosProvisioner) createDockerCluster(ctx context.Context, clusterName string) error {
	// Ensure required kernel modules are loaded (Linux only)
	err := p.ensureKernelModules(ctx)
	if err != nil {
		return err
	}

	// Verify Docker is available and running
	err = p.checkDockerAvailable(ctx)
	if err != nil {
		return err
	}

	// Check if cluster already exists
	exists, err := p.Exists(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to check if cluster exists: %w", err)
	}

	if exists {
		return fmt.Errorf("%w: %s", ErrClusterAlreadyExists, clusterName)
	}

	// Use the pre-loaded configs (already have all patches applied)
	configBundle := p.talosConfigs.Bundle()

	// Provision cluster and save configurations
	cluster, err := p.provisionCluster(ctx, clusterName, configBundle)
	if err != nil {
		return err
	}

	return p.saveClusterConfigs(ctx, cluster, configBundle)
}

// createHetznerCluster creates a Talos cluster on Hetzner Cloud infrastructure.
//
//nolint:cyclop,funlen // Complex function with sequential steps for cloud provisioning
func (p *TalosProvisioner) createHetznerCluster(ctx context.Context, clusterName string) error {
	// Type assert to get Hetzner-specific provider
	hetznerProv, ok := p.infraProvider.(*hetzner.Provider)
	if !ok {
		return fmt.Errorf("%w: got %T", ErrHetznerProviderRequired, p.infraProvider)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Creating Talos cluster %q on Hetzner Cloud...\n", clusterName)

	// Check if cluster already exists
	exists, err := hetznerProv.NodesExist(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to check if cluster exists: %w", err)
	}

	if exists {
		return fmt.Errorf("%w: %s", ErrClusterAlreadyExists, clusterName)
	}

	// Create infrastructure resources
	_, _ = fmt.Fprintf(p.logWriter, "Creating infrastructure resources...\n")

	network, err := hetznerProv.EnsureNetwork(ctx, clusterName, p.hetznerOpts.NetworkCIDR)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Network %s created\n", network.Name)

	firewall, err := hetznerProv.EnsureFirewall(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to create firewall: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Firewall %s created\n", firewall.Name)

	placementGroup, err := hetznerProv.EnsurePlacementGroup(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to create placement group: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Placement group %s created\n", placementGroup.Name)

	// Get SSH key if configured
	var sshKeyID int64

	if p.hetznerOpts.SSHKeyName != "" {
		sshKey, keyErr := hetznerProv.GetSSHKey(ctx, p.hetznerOpts.SSHKeyName)
		if keyErr != nil {
			return fmt.Errorf("failed to get SSH key: %w", keyErr)
		}

		if sshKey != nil {
			sshKeyID = sshKey.ID
		}
	}

	// Track created servers for bootstrap
	var controlPlaneServers []*hcloud.Server

	var workerServers []*hcloud.Server

	// Create control plane nodes
	_, _ = fmt.Fprintf(
		p.logWriter,
		"Creating %d control-plane node(s)...\n",
		p.options.ControlPlaneNodes,
	)

	for i := range p.options.ControlPlaneNodes {
		nodeName := fmt.Sprintf("%s-control-plane-%d", clusterName, i+1)

		server, serverErr := hetznerProv.CreateServer(ctx, hetzner.CreateServerOpts{
			Name:             nodeName,
			ServerType:       p.hetznerOpts.ControlPlaneServerType,
			ISOID:            p.talosOpts.ISO,
			Location:         p.hetznerOpts.Location,
			Labels:           hetzner.NodeLabels(clusterName, "control-plane", i+1),
			NetworkID:        network.ID,
			PlacementGroupID: placementGroup.ID,
			SSHKeyID:         sshKeyID,
			FirewallIDs:      []int64{firewall.ID},
		})
		if serverErr != nil {
			return fmt.Errorf("failed to create control-plane node %s: %w", nodeName, serverErr)
		}

		controlPlaneServers = append(controlPlaneServers, server)

		_, _ = fmt.Fprintf(p.logWriter, "  ✓ Control-plane node %s created (IP: %s)\n",
			server.Name, server.PublicNet.IPv4.IP.String())
	}

	// Create worker nodes
	if p.options.WorkerNodes > 0 {
		_, _ = fmt.Fprintf(p.logWriter, "Creating %d worker node(s)...\n", p.options.WorkerNodes)

		for i := range p.options.WorkerNodes {
			nodeName := fmt.Sprintf("%s-worker-%d", clusterName, i+1)

			server, serverErr := hetznerProv.CreateServer(ctx, hetzner.CreateServerOpts{
				Name:             nodeName,
				ServerType:       p.hetznerOpts.WorkerServerType,
				ISOID:            p.talosOpts.ISO,
				Location:         p.hetznerOpts.Location,
				Labels:           hetzner.NodeLabels(clusterName, "worker", i+1),
				NetworkID:        network.ID,
				PlacementGroupID: placementGroup.ID,
				SSHKeyID:         sshKeyID,
				FirewallIDs:      []int64{firewall.ID},
			})
			if serverErr != nil {
				return fmt.Errorf("failed to create worker node %s: %w", nodeName, serverErr)
			}

			workerServers = append(workerServers, server)

			_, _ = fmt.Fprintf(p.logWriter, "  ✓ Worker node %s created (IP: %s)\n",
				server.Name, server.PublicNet.IPv4.IP.String())
		}
	}

	_, _ = fmt.Fprintf(p.logWriter, "\nInfrastructure created. Bootstrapping Talos cluster...\n")

	// Regenerate configs with the first control-plane node's public IP as the endpoint.
	// This is necessary because:
	// 1. The original configs were generated with internal network IPs
	// 2. Hetzner nodes are accessed via their public IPs
	// 3. The endpoint IP is embedded in certificates and must match
	firstCPIP := controlPlaneServers[0].PublicNet.IPv4.IP.String()

	_, _ = fmt.Fprintf(p.logWriter, "Regenerating configs with endpoint IP %s...\n", firstCPIP)

	updatedConfigs, err := p.talosConfigs.WithEndpoint(firstCPIP)
	if err != nil {
		return fmt.Errorf("failed to regenerate configs with endpoint: %w", err)
	}

	// Update the stored configs and get the bundle
	p.talosConfigs = updatedConfigs
	configBundle := updatedConfigs.Bundle()

	// Build list of all node IPs for waiting
	allServers := append(controlPlaneServers, workerServers...)

	// Wait for Talos API to be reachable on all nodes (maintenance mode)
	_, _ = fmt.Fprintf(p.logWriter, "Waiting for Talos API on %d nodes...\n", len(allServers))

	if err = p.waitForHetznerTalosAPI(ctx, allServers); err != nil {
		return fmt.Errorf("failed waiting for Talos API: %w", err)
	}

	// Apply machine configuration to all nodes
	_, _ = fmt.Fprintf(p.logWriter, "Applying machine configuration to nodes...\n")

	if err = p.applyHetznerConfigs(
		ctx,
		clusterName,
		controlPlaneServers,
		workerServers,
		configBundle,
	); err != nil {
		return fmt.Errorf("failed to apply machine configuration: %w", err)
	}

	// Detach ISOs from all servers so they boot from disk instead of ISO
	_, _ = fmt.Fprintf(p.logWriter, "Detaching ISOs and rebooting nodes...\n")

	if err = p.detachISOsAndReboot(ctx, hetznerProv, allServers); err != nil {
		return fmt.Errorf("failed to detach ISOs: %w", err)
	}

	// Bootstrap the cluster on the first control-plane node
	_, _ = fmt.Fprintf(p.logWriter, "Bootstrapping etcd cluster...\n")

	if err = p.bootstrapHetznerCluster(ctx, controlPlaneServers[0], configBundle); err != nil {
		return fmt.Errorf("failed to bootstrap cluster: %w", err)
	}

	// Save kubeconfig
	if p.options.KubeconfigPath != "" {
		_, _ = fmt.Fprintf(p.logWriter, "Fetching and saving kubeconfig...\n")

		err = p.saveHetznerKubeconfig(ctx, controlPlaneServers[0], configBundle)
		if err != nil {
			return fmt.Errorf("failed to save kubeconfig: %w", err)
		}

		// Wait for cluster to be fully ready before reporting success
		// This uses upstream Talos SDK check.Wait() pattern
		_, _ = fmt.Fprintf(p.logWriter, "Waiting for cluster to be ready...\n")

		if waitErr := p.waitForHetznerClusterReady(
			ctx,
			clusterName,
			controlPlaneServers,
			workerServers,
			configBundle,
		); waitErr != nil {
			return fmt.Errorf("cluster readiness check failed: %w", waitErr)
		}

		_, _ = fmt.Fprintf(p.logWriter, "  ✓ Cluster is ready\n")
	}

	_, _ = fmt.Fprintf(
		p.logWriter,
		"\nSuccessfully created Talos cluster %q on Hetzner Cloud\n",
		clusterName,
	)

	return nil
}

// waitForHetznerTalosAPI waits for the Talos API to be reachable on all Hetzner servers.
// Nodes booted from ISO are in maintenance mode and expose the Talos API on port 50000.
func (p *TalosProvisioner) waitForHetznerTalosAPI(
	ctx context.Context,
	servers []*hcloud.Server,
) error {
	timeout := 5 * time.Minute

	for _, server := range servers {
		ip := server.PublicNet.IPv4.IP.String()
		endpoint := fmt.Sprintf("%s:%d", ip, talosAPIPort)

		_, _ = fmt.Fprintf(
			p.logWriter,
			"  Waiting for Talos API on %s (%s)...\n",
			server.Name,
			endpoint,
		)

		err := retry.Constant(timeout, retry.WithUnits(5*time.Second)).
			RetryWithContext(ctx, func(ctx context.Context) error {
				// Try to establish a TLS connection to verify the Talos API is responding
				// In maintenance mode, we can only verify the connection works - most APIs
				// return "not implemented in maintenance mode" which is expected
				c, connErr := talosclient.New(ctx,
					talosclient.WithEndpoints(ip),
					talosclient.WithTLSConfig(&tls.Config{
						InsecureSkipVerify: true, //nolint:gosec // Maintenance mode requires insecure connection
					}),
				)
				if connErr != nil {
					return retry.ExpectedError(connErr)
				}

				defer c.Close() //nolint:errcheck

				// Try to get version - in maintenance mode this may return "not implemented"
				// but that error indicates the API is reachable and responding
				_, versionErr := c.Version(ctx)
				if versionErr != nil {
					// "Unimplemented" means the API is reachable but in maintenance mode
					// This is actually a success - the node is ready for config application
					if strings.Contains(versionErr.Error(), "Unimplemented") {
						return nil
					}

					return retry.ExpectedError(versionErr)
				}

				return nil
			})
		if err != nil {
			return fmt.Errorf("timeout waiting for Talos API on %s: %w", server.Name, err)
		}

		_, _ = fmt.Fprintf(p.logWriter, "  ✓ Talos API reachable on %s\n", server.Name)
	}

	return nil
}

// applyHetznerConfigs applies machine configuration to all Hetzner nodes.
// It uses the insecure Talos client to connect to nodes in maintenance mode.
func (p *TalosProvisioner) applyHetznerConfigs(
	ctx context.Context,
	clusterName string,
	controlPlaneServers []*hcloud.Server,
	workerServers []*hcloud.Server,
	configBundle *bundle.Bundle,
) error {
	// Get control-plane and worker configs
	cpConfig := configBundle.ControlPlane()
	workerConfig := configBundle.Worker()

	// Apply control-plane config to all control-plane nodes
	for _, server := range controlPlaneServers {
		err := p.applyConfigToNode(ctx, server, cpConfig)
		if err != nil {
			return fmt.Errorf("failed to apply config to %s: %w", server.Name, err)
		}
	}

	// Apply worker config to all worker nodes
	for _, server := range workerServers {
		err := p.applyConfigToNode(ctx, server, workerConfig)
		if err != nil {
			return fmt.Errorf("failed to apply config to %s: %w", server.Name, err)
		}
	}

	return nil
}

// detachISOsAndReboot handles the post-config-apply phase of Hetzner Talos installation.
//
// After ApplyConfiguration, Talos runs the install sequence which:
// 1. Installs Talos to disk (creates STATE, EPHEMERAL partitions)
// 2. Automatically reboots the server
//
// On Hetzner, after reboot with an installed disk, the server typically boots from disk
// even with ISO still attached (disk gets higher boot priority after install).
//
// This function:
// 1. Waits for the installation + automatic reboot to complete
// 2. Waits for servers to become reachable (connection refused during reboot)
// 3. Detaches ISOs for cleanliness (not strictly required but good practice)
//
// Note: We cannot reliably poll STATE partition because the server reboots automatically
// during install, which breaks our insecure TLS connection.
func (p *TalosProvisioner) detachISOsAndReboot(
	ctx context.Context,
	hetznerProv *hetzner.Provider,
	servers []*hcloud.Server,
) error {
	_, _ = fmt.Fprintf(p.logWriter, "  Waiting for installation and automatic reboot to complete...\n")
	_, _ = fmt.Fprintf(p.logWriter, "  (Talos will install to disk and reboot automatically - this takes 3-5 minutes)\n")

	// Wait for all servers to complete installation and reboot
	// During this time:
	// - Nodes install Talos to disk (1-2 minutes)
	// - Nodes automatically reboot (from install sequence)
	// - Nodes boot from disk and come up with authenticated TLS
	//
	// We detect completion by waiting for a TCP connection to succeed on port 50000
	// (the server will be unreachable during reboot, then come back)
	for _, server := range servers {
		ip := server.PublicNet.IPv4.IP.String()

		_, _ = fmt.Fprintf(p.logWriter, "  Waiting for %s to install, reboot, and become reachable...\n", server.Name)

		// Wait for server to become reachable after installation + reboot
		// This waits through the entire install cycle:
		// - Initial "connection refused" during install
		// - Then more "connection refused" during reboot
		// - Finally success when booted from disk
		err := retry.Constant(10*time.Minute, retry.WithUnits(10*time.Second)).
			RetryWithContext(ctx, func(ctx context.Context) error {
				// Just check if we can establish a TCP connection
				// We don't care about TLS here, just network reachability
				conn, dialErr := net.DialTimeout("tcp", ip+":50000", 5*time.Second)
				if dialErr != nil {
					return retry.ExpectedError(fmt.Errorf("waiting for server to become reachable: %w", dialErr))
				}
				conn.Close()

				return nil
			})
		if err != nil {
			return fmt.Errorf("timeout waiting for %s to become reachable after install: %w", server.Name, err)
		}

		_, _ = fmt.Fprintf(p.logWriter, "  ✓ %s is reachable after install\n", server.Name)
	}

	// Now detach ISOs for cleanliness
	// This isn't strictly required (disk has boot priority after install)
	// but it's good practice to clean up
	for _, server := range servers {
		_, _ = fmt.Fprintf(p.logWriter, "  Detaching ISO from %s...\n", server.Name)

		err := hetznerProv.DetachISO(ctx, server)
		if err != nil {
			// Log but don't fail - ISO detachment is not critical
			_, _ = fmt.Fprintf(p.logWriter, "  Warning: Failed to detach ISO from %s: %v\n", server.Name, err)
		} else {
			_, _ = fmt.Fprintf(p.logWriter, "  ✓ ISO detached from %s\n", server.Name)
		}
	}

	return nil
}

// applyConfigToNode applies machine configuration to a single Hetzner node.
//

func (p *TalosProvisioner) applyConfigToNode(
	ctx context.Context,
	server *hcloud.Server,
	config talosconfig.Provider,
) error {
	ip := server.PublicNet.IPv4.IP.String()

	_, _ = fmt.Fprintf(p.logWriter, "  Applying config to %s (%s)...\n", server.Name, ip)

	// Get config bytes
	cfgBytes, err := config.Bytes()
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Create insecure client for maintenance mode
	c, err := talosclient.New(ctx,
		talosclient.WithEndpoints(ip),
		talosclient.WithTLSConfig(&tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // Maintenance mode requires insecure connection
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to create Talos client: %w", err)
	}

	defer c.Close() //nolint:errcheck

	// Apply configuration - the node will install and reboot
	_, err = c.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{
		Data: cfgBytes,
	})
	if err != nil {
		return fmt.Errorf("failed to apply configuration: %w", err)
	}

	_, _ = fmt.Fprintf(
		p.logWriter,
		"  ✓ Config applied to %s (node will install and reboot)\n",
		server.Name,
	)

	return nil
}

// bootstrapHetznerCluster bootstraps the etcd cluster on the first control-plane node.
func (p *TalosProvisioner) bootstrapHetznerCluster(
	ctx context.Context,
	bootstrapNode *hcloud.Server,
	configBundle *bundle.Bundle,
) error {
	ip := bootstrapNode.PublicNet.IPv4.IP.String()

	_, _ = fmt.Fprintf(
		p.logWriter,
		"  Waiting for %s to be ready for bootstrap...\n",
		bootstrapNode.Name,
	)

	// After config is applied, nodes will reboot. We need to wait for the Talos API
	// to come back up with the applied configuration (authenticated mode).
	talosConfig := configBundle.TalosConfig()

	// Wait for the node to come back after installation
	timeout := clusterReadinessTimeout

	err := retry.Constant(timeout, retry.WithUnits(10*time.Second)).
		RetryWithContext(ctx, func(ctx context.Context) error {
			// Create authenticated client using talosconfig
			c, clientErr := talosclient.New(ctx,
				talosclient.WithEndpoints(ip),
				talosclient.WithConfig(talosConfig),
			)
			if clientErr != nil {
				return retry.ExpectedError(clientErr)
			}

			defer c.Close() //nolint:errcheck

			// Try to get version to verify the node is ready
			_, versionErr := c.Version(ctx)
			if versionErr != nil {
				return retry.ExpectedError(versionErr)
			}

			return nil
		})
	if err != nil {
		return fmt.Errorf("timeout waiting for node to be ready after installation: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Node %s is ready\n", bootstrapNode.Name)

	// Create authenticated client for bootstrap
	c, err := talosclient.New(ctx,
		talosclient.WithEndpoints(ip),
		talosclient.WithConfig(talosConfig),
	)
	if err != nil {
		return fmt.Errorf("failed to create Talos client: %w", err)
	}

	defer c.Close() //nolint:errcheck

	_, _ = fmt.Fprintf(p.logWriter, "  Bootstrapping etcd on %s...\n", bootstrapNode.Name)

	// Bootstrap the cluster
	err = retry.Constant(2*time.Minute, retry.WithUnits(5*time.Second)).
		RetryWithContext(ctx, func(ctx context.Context) error {
			bootstrapErr := c.Bootstrap(ctx, &machineapi.BootstrapRequest{})
			if bootstrapErr != nil {
				// FailedPrecondition means the node isn't ready yet
				if talosclient.StatusCode(bootstrapErr) == 9 { // FailedPrecondition
					return retry.ExpectedError(bootstrapErr)
				}

				return bootstrapErr
			}

			return nil
		})
	if err != nil {
		return fmt.Errorf("failed to bootstrap cluster: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Etcd cluster bootstrapped\n")

	// Wait for cluster to be ready
	_, _ = fmt.Fprintf(p.logWriter, "  Waiting for Kubernetes to be ready...\n")

	err = retry.Constant(timeout, retry.WithUnits(10*time.Second)).
		RetryWithContext(ctx, func(ctx context.Context) error {
			// Try to fetch kubeconfig as an indicator that K8s is ready
			_, kubeconfigErr := c.Kubeconfig(ctx)
			if kubeconfigErr != nil {
				return retry.ExpectedError(kubeconfigErr)
			}

			return nil
		})
	if err != nil {
		return fmt.Errorf("timeout waiting for Kubernetes to be ready: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Kubernetes is ready\n")

	return nil
}

// saveHetznerKubeconfig fetches and saves the kubeconfig from a Hetzner control-plane node.
func (p *TalosProvisioner) saveHetznerKubeconfig(
	ctx context.Context,
	controlPlaneNode *hcloud.Server,
	configBundle *bundle.Bundle,
) error {
	ip := controlPlaneNode.PublicNet.IPv4.IP.String()
	talosConfig := configBundle.TalosConfig()

	// Create authenticated client
	c, err := talosclient.New(ctx,
		talosclient.WithEndpoints(ip),
		talosclient.WithConfig(talosConfig),
	)
	if err != nil {
		return fmt.Errorf("failed to create Talos client: %w", err)
	}

	defer c.Close() //nolint:errcheck

	// Fetch kubeconfig
	kubeconfig, err := c.Kubeconfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch kubeconfig: %w", err)
	}

	// The kubeconfig from Talos uses internal IPs. For Hetzner, we need to use the public IP.
	// Rewrite the server endpoint to use the public IP.
	kubeconfig, err = rewriteKubeconfigEndpoint(kubeconfig, fmt.Sprintf("https://%s:6443", ip))
	if err != nil {
		return fmt.Errorf("failed to rewrite kubeconfig endpoint: %w", err)
	}

	// Expand tilde in kubeconfig path
	kubeconfigPath, err := iopath.ExpandHomePath(p.options.KubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to expand kubeconfig path: %w", err)
	}

	// Ensure kubeconfig directory exists
	kubeconfigDir := filepath.Dir(kubeconfigPath)
	if kubeconfigDir != "" && kubeconfigDir != "." {
		mkdirErr := os.MkdirAll(kubeconfigDir, stateDirectoryPermissions)
		if mkdirErr != nil {
			return fmt.Errorf("failed to create kubeconfig directory: %w", mkdirErr)
		}
	}

	// Write kubeconfig to file
	err = os.WriteFile(kubeconfigPath, kubeconfig, kubeconfigFileMode)
	if err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Kubeconfig saved to %s\n", kubeconfigPath)

	return nil
}

// Delete deletes a Talos cluster.
// If name is non-empty, it overrides the configured cluster name.
// Routes to Docker-based or Hetzner-based deletion based on configuration.
func (p *TalosProvisioner) Delete(ctx context.Context, name string) error {
	clusterName := p.resolveClusterName(name)

	// Route to Hetzner-based deletion if Hetzner options are set
	if p.hetznerOpts != nil {
		return p.deleteHetznerCluster(ctx, clusterName)
	}

	// Docker-based deletion (default)
	return p.deleteDockerCluster(ctx, clusterName)
}

// deleteDockerCluster deletes a Talos-in-Docker cluster using the Talos SDK.
func (p *TalosProvisioner) deleteDockerCluster(ctx context.Context, clusterName string) error {
	_, err := p.validateClusterOperation(ctx, clusterName)
	if err != nil {
		return err
	}

	// Collect volumes used by Talos containers BEFORE destroying the cluster
	// These are anonymous volumes that the Talos SDK doesn't clean up
	containers, err := p.listTalosContainers(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to list Talos containers: %w", err)
	}

	volumes := p.collectContainerVolumes(ctx, containers)

	// Get state directory for cluster state
	stateDir, err := getStateDirectory()
	if err != nil {
		return fmt.Errorf("failed to get state directory: %w", err)
	}

	// Create Talos provisioner
	talosProvisioner, err := p.provisionerFactory(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Talos provisioner: %w", err)
	}

	defer func() { _ = talosProvisioner.Close() }()

	// Reflect to get cluster object from existing state
	cluster, err := talosProvisioner.Reflect(ctx, clusterName, stateDir)
	if err != nil {
		return fmt.Errorf("failed to reflect cluster state: %w", err)
	}

	// Destroy the cluster
	_, _ = fmt.Fprintf(p.logWriter, "Deleting Talos cluster %q...\n", clusterName)

	err = talosProvisioner.Destroy(ctx, cluster, provision.WithLogWriter(p.logWriter))
	if err != nil {
		return fmt.Errorf("failed to destroy cluster: %w", err)
	}

	// Clean up the anonymous volumes used by Talos containers
	// These are created by Docker for Talos node data and are not cleaned up by the Talos SDK
	if len(volumes) > 0 {
		_, _ = fmt.Fprintf(p.logWriter, "Cleaning up %d Talos node volumes...\n", len(volumes))
		p.removeVolumes(ctx, volumes)
	}

	// Clean up kubeconfig - remove only the context for this cluster
	if p.options.KubeconfigPath != "" {
		cleanupErr := p.cleanupKubeconfig(clusterName)
		if cleanupErr != nil {
			// Log warning but don't fail the delete operation
			_, _ = fmt.Fprintf(
				p.logWriter,
				"Warning: failed to clean up kubeconfig: %v\n",
				cleanupErr,
			)
		}
	}

	_, _ = fmt.Fprintf(p.logWriter, "Successfully deleted Talos cluster %q\n", clusterName)

	return nil
}

// deleteHetznerCluster deletes a Talos cluster on Hetzner Cloud infrastructure.
func (p *TalosProvisioner) deleteHetznerCluster(ctx context.Context, clusterName string) error {
	// Type assert to get Hetzner-specific provider
	hetznerProv, ok := p.infraProvider.(*hetzner.Provider)
	if !ok {
		return fmt.Errorf("%w: got %T", ErrHetznerProviderRequired, p.infraProvider)
	}

	// Check if cluster exists
	exists, err := hetznerProv.NodesExist(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to check if cluster exists: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: %s", clustererrors.ErrClusterNotFound, clusterName)
	}

	// Delete all nodes and infrastructure
	_, _ = fmt.Fprintf(p.logWriter, "Deleting Talos cluster %q on Hetzner...\n", clusterName)

	err = hetznerProv.DeleteNodes(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to delete Hetzner nodes: %w", err)
	}

	// Clean up kubeconfig
	if p.options.KubeconfigPath != "" {
		cleanupErr := p.cleanupKubeconfig(clusterName)
		if cleanupErr != nil {
			_, _ = fmt.Fprintf(
				p.logWriter,
				"Warning: failed to clean up kubeconfig: %v\n",
				cleanupErr,
			)
		}
	}

	_, _ = fmt.Fprintf(p.logWriter, "Successfully deleted Talos cluster %q\n", clusterName)

	return nil
}

// Exists checks if a Talos cluster exists.
// If name is non-empty, it overrides the configured cluster name.
// Routes to Docker-based or Hetzner-based existence check based on configuration.
func (p *TalosProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	clusterName := p.resolveClusterName(name)

	// Route to Hetzner-based check if Hetzner options are set
	if p.hetznerOpts != nil {
		hetznerProv, ok := p.infraProvider.(*hetzner.Provider)
		if !ok {
			return false, fmt.Errorf("%w: got %T", ErrHetznerProviderRequired, p.infraProvider)
		}

		exists, err := hetznerProv.NodesExist(ctx, clusterName)
		if err != nil {
			return false, fmt.Errorf("failed to check if cluster exists: %w", err)
		}

		return exists, nil
	}

	// Docker-based check (default)
	if p.dockerClient == nil {
		return false, ErrDockerNotAvailable
	}

	containers, err := p.listTalosContainers(ctx, clusterName)
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}

	return len(containers) > 0, nil
}

// List lists all Talos-in-Docker clusters.
// Returns unique cluster names from containers with Talos labels.
func (p *TalosProvisioner) List(ctx context.Context) ([]string, error) {
	if p.dockerClient == nil {
		return nil, ErrDockerNotAvailable
	}

	// Find all containers owned by Talos provisioner
	containers, err := p.dockerClient.ContainerList(ctx, container.ListOptions{
		All: true, // Include stopped containers
		Filters: filters.NewArgs(
			filters.Arg("label", LabelTalosOwned+"=true"),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	// Extract unique cluster names
	clusterSet := make(map[string]struct{})

	for _, c := range containers {
		if name, ok := c.Labels[LabelTalosClusterName]; ok && name != "" {
			clusterSet[name] = struct{}{}
		}
	}

	// Convert set to slice
	clusters := make([]string, 0, len(clusterSet))
	for name := range clusterSet {
		clusters = append(clusters, name)
	}

	return clusters, nil
}

// Start starts a stopped Talos-in-Docker cluster.
// If name is non-empty, it overrides the configured cluster name.
// Uses the infrastructure provider if set, otherwise falls back to Docker client.
func (p *TalosProvisioner) Start(ctx context.Context, name string) error {
	clusterName := p.resolveClusterName(name)

	// Use infrastructure provider if available
	if p.infraProvider != nil {
		_, _ = fmt.Fprintf(p.logWriter, "Starting Talos cluster %q...\n", clusterName)

		err := p.infraProvider.StartNodes(ctx, clusterName)
		if err != nil {
			return fmt.Errorf("failed to start cluster %q: %w", clusterName, err)
		}

		// Wait for cluster to be ready (same checks as during creation)
		err = p.waitForHetznerClusterReadyAfterStart(ctx, clusterName)
		if err != nil {
			return fmt.Errorf("cluster started but not ready: %w", err)
		}

		_, _ = fmt.Fprintf(p.logWriter, "Successfully started Talos cluster %q\n", clusterName)

		return nil
	}

	// Fall back to Docker client for backwards compatibility
	clusterName, containers, err := p.getClusterContainers(ctx, name)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "Starting Talos cluster %q...\n", clusterName)

	// Start each container
	for _, c := range containers {
		err = p.dockerClient.ContainerStart(ctx, c.ID, container.StartOptions{})
		if err != nil {
			return fmt.Errorf("failed to start container %s: %w", c.Names[0], err)
		}
	}

	_, _ = fmt.Fprintf(p.logWriter, "Successfully started Talos cluster %q\n", clusterName)

	return nil
}

// containerStopTimeout is the timeout for stopping a container gracefully.
const containerStopTimeout = 30

// Stop stops a running Talos-in-Docker cluster.
// If name is non-empty, it overrides the configured cluster name.
// Uses the infrastructure provider if set, otherwise falls back to Docker client.
func (p *TalosProvisioner) Stop(ctx context.Context, name string) error {
	clusterName := p.resolveClusterName(name)

	// Use infrastructure provider if available
	if p.infraProvider != nil {
		_, _ = fmt.Fprintf(p.logWriter, "Stopping Talos cluster %q...\n", clusterName)

		err := p.infraProvider.StopNodes(ctx, clusterName)
		if err != nil {
			return fmt.Errorf("failed to stop cluster %q: %w", clusterName, err)
		}

		_, _ = fmt.Fprintf(p.logWriter, "Successfully stopped Talos cluster %q\n", clusterName)

		return nil
	}

	// Fall back to Docker client for backwards compatibility
	clusterName, containers, err := p.getClusterContainers(ctx, name)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "Stopping Talos cluster %q...\n", clusterName)

	// Stop each container with a graceful timeout
	timeout := containerStopTimeout
	for _, c := range containers {
		err = p.dockerClient.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout})
		if err != nil {
			return fmt.Errorf("failed to stop container %s: %w", c.Names[0], err)
		}
	}

	_, _ = fmt.Fprintf(p.logWriter, "Successfully stopped Talos cluster %q\n", clusterName)

	return nil
}

// listTalosContainers lists all containers for a specific Talos cluster.
func (p *TalosProvisioner) listTalosContainers(
	ctx context.Context,
	clusterName string,
) ([]container.Summary, error) {
	containers, err := p.dockerClient.ContainerList(ctx, container.ListOptions{
		All: true, // Include stopped containers
		Filters: filters.NewArgs(
			filters.Arg("label", LabelTalosOwned+"=true"),
			filters.Arg("label", LabelTalosClusterName+"="+clusterName),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list Talos containers: %w", err)
	}

	return containers, nil
}

// collectContainerVolumes collects all volume names used by the given containers.
// It inspects each container to find mounted volumes (anonymous volumes used by Talos).
func (p *TalosProvisioner) collectContainerVolumes(
	ctx context.Context,
	containers []container.Summary,
) []string {
	volumeSet := make(map[string]struct{})

	for _, containerSummary := range containers {
		inspect, err := p.dockerClient.ContainerInspect(ctx, containerSummary.ID)
		if err != nil {
			// Log warning but continue with other containers
			_, _ = fmt.Fprintf(
				p.logWriter,
				"Warning: failed to inspect container %s: %v\n",
				containerSummary.ID[:12],
				err,
			)

			continue
		}

		for _, mount := range inspect.Mounts {
			// Only collect volume mounts (not bind mounts or tmpfs)
			if mount.Type == "volume" && mount.Name != "" {
				volumeSet[mount.Name] = struct{}{}
			}
		}
	}

	volumes := make([]string, 0, len(volumeSet))
	for vol := range volumeSet {
		volumes = append(volumes, vol)
	}

	return volumes
}

// removeVolumes removes the specified volumes.
// Errors are logged but do not cause the operation to fail.
func (p *TalosProvisioner) removeVolumes(ctx context.Context, volumes []string) {
	for _, vol := range volumes {
		err := p.dockerClient.VolumeRemove(ctx, vol, true) // force=true
		if err != nil {
			_, _ = fmt.Fprintf(
				p.logWriter,
				"Warning: failed to remove volume %s: %v\n",
				vol,
				err,
			)
		}
	}
}

// validateClusterOperation validates that Docker is available and the cluster exists.
// Returns the resolved cluster name or an error.
func (p *TalosProvisioner) validateClusterOperation(
	ctx context.Context,
	name string,
) (string, error) {
	// Verify Docker is available and running
	err := p.checkDockerAvailable(ctx)
	if err != nil {
		return "", err
	}

	clusterName := p.resolveClusterName(name)

	// Check if cluster exists
	exists, err := p.Exists(ctx, clusterName)
	if err != nil {
		return "", fmt.Errorf("failed to check if cluster exists: %w", err)
	}

	if !exists {
		return "", fmt.Errorf("%w: %s", clustererrors.ErrClusterNotFound, clusterName)
	}

	return clusterName, nil
}

// getClusterContainers validates the operation and returns the cluster's containers.
// This combines validation with container listing for Start/Stop operations.
func (p *TalosProvisioner) getClusterContainers(
	ctx context.Context,
	name string,
) (string, []container.Summary, error) {
	clusterName, err := p.validateClusterOperation(ctx, name)
	if err != nil {
		return "", nil, err
	}

	containers, err := p.listTalosContainers(ctx, clusterName)
	if err != nil {
		return "", nil, fmt.Errorf("failed to list containers: %w", err)
	}

	return clusterName, containers, nil
}

// bootstrapAndSaveKubeconfig bootstraps the cluster and saves the kubeconfig.
//
//nolint:cyclop,funlen // Bootstrap sequence is inherently complex but logically coherent
func (p *TalosProvisioner) bootstrapAndSaveKubeconfig(
	ctx context.Context,
	cluster provision.Cluster,
	configBundle *bundle.Bundle,
) error {
	// Get the mapped Talos API endpoint for Docker-in-VM environments (macOS, Windows).
	// On these platforms, the container's internal IP is not accessible from the host,
	// so we need to use 127.0.0.1 with the mapped port.
	mappedEndpoint, err := p.getMappedTalosAPIEndpoint(ctx, cluster.Info().ClusterName)
	if err != nil {
		return fmt.Errorf("failed to get mapped Talos API endpoint: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Using Talos API endpoint: %s\n", mappedEndpoint)

	// Create a modified talosconfig with the mapped endpoint
	talosConfig := configBundle.TalosConfig()
	if talosConfig != nil && talosConfig.Context != "" {
		if context, ok := talosConfig.Contexts[talosConfig.Context]; ok {
			context.Endpoints = []string{mappedEndpoint}
		}
	}

	// Get the Kubernetes API endpoint from the cluster info.
	// The Docker provisioner automatically sets this to the external endpoint
	// (https://127.0.0.1:<mapped-port>) when the cluster is created.
	kubernetesEndpoint := cluster.Info().KubernetesEndpoint
	if kubernetesEndpoint == "" {
		return ErrMissingKubernetesEndpoint
	}

	_, _ = fmt.Fprintf(p.logWriter, "Using Kubernetes API endpoint: %s\n", kubernetesEndpoint)

	// Create access adapter for cluster operations.
	// WithKubernetesEndpoint sets ForceEndpoint which is used to rewrite the kubeconfig.
	clusterAccess := access.NewAdapter(
		cluster,
		provision.WithTalosConfig(talosConfig),
		provision.WithKubernetesEndpoint(kubernetesEndpoint),
	)

	defer func() { _ = clusterAccess.Close() }()

	// Bootstrap the cluster
	_, _ = fmt.Fprintf(p.logWriter, "Bootstrapping cluster...\n")

	err = clusterAccess.Bootstrap(ctx, p.logWriter)
	if err != nil {
		return fmt.Errorf("bootstrap failed: %w", err)
	}

	// Wait for cluster to be ready (Talos API, etcd, Kubernetes API via external endpoint).
	// Since clusterAccess has ForceEndpoint set to the mapped localhost port,
	// K8s checks in DefaultClusterChecks() validate host connectivity.
	_, _ = fmt.Fprintf(p.logWriter, "Waiting for cluster to be ready...\n")

	checkCtx, checkCancel := context.WithTimeout(ctx, clusterReadinessTimeout)
	defer checkCancel()

	// Select appropriate cluster checks based on CNI configuration.
	// When using a custom CNI (e.g., Cilium), skip CNI-dependent checks (CoreDNS, kube-proxy)
	// because pods cannot start until the CNI is installed.
	// However, we still need K8sComponentsReadinessChecks to ensure K8s API is ready.
	// See: https://pkg.go.dev/github.com/siderolabs/talos/pkg/cluster/check#K8sComponentsReadinessChecks
	clusterChecks := check.DefaultClusterChecks()

	// Skip CNI-dependent checks if:
	// 1. The Talos config has CNI disabled (scaffolded project with disable-default-cni.yaml patch), OR
	// 2. KSail will install a custom CNI after cluster creation (options.SkipCNIChecks)
	//
	// We use PreBootSequenceChecks + K8sComponentsReadinessChecks (without the node ready checks)
	// to ensure:
	// - Talos services are healthy (etcd, apid, kubelet)
	// - K8s nodes are reported (registered with API server)
	// - Control plane static pods are running
	// This matches talosctl's SkipK8sNodeReadinessCheck behavior.
	if (p.talosConfigs != nil && p.talosConfigs.IsCNIDisabled()) || p.options.SkipCNIChecks {
		clusterChecks = slices.Concat(
			check.PreBootSequenceChecks(),
			check.K8sComponentsReadinessChecks(),
		)
	}

	err = check.Wait(checkCtx, clusterAccess, clusterChecks, check.StderrReporter())
	if err != nil {
		return fmt.Errorf("cluster readiness check failed: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Cluster is ready\n")

	// Fetch kubeconfig from cluster
	_, _ = fmt.Fprintf(p.logWriter, "Fetching kubeconfig...\n")

	kubeconfig, err := clusterAccess.Kubeconfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch kubeconfig: %w", err)
	}

	// Rewrite kubeconfig server endpoint using ForceEndpoint (set via WithKubernetesEndpoint).
	// The kubeconfig from Talos uses internal IPs, but we need the mapped localhost endpoint.
	kubeconfig, err = rewriteKubeconfigEndpoint(kubeconfig, clusterAccess.ForceEndpoint)
	if err != nil {
		return fmt.Errorf("failed to rewrite kubeconfig endpoint: %w", err)
	}

	// Expand tilde in kubeconfig path (e.g., ~/.kube/config -> /home/user/.kube/config)
	kubeconfigPath, err := iopath.ExpandHomePath(p.options.KubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to expand kubeconfig path: %w", err)
	}

	// Ensure kubeconfig directory exists
	kubeconfigDir := filepath.Dir(kubeconfigPath)
	if kubeconfigDir != "" && kubeconfigDir != "." {
		mkdirErr := os.MkdirAll(kubeconfigDir, stateDirectoryPermissions)
		if mkdirErr != nil {
			return fmt.Errorf("failed to create kubeconfig directory: %w", mkdirErr)
		}
	}

	// Write kubeconfig to file
	err = os.WriteFile(kubeconfigPath, kubeconfig, kubeconfigFileMode)
	if err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Saved kubeconfig to %s\n", kubeconfigPath)

	return nil
}

// provisionCluster creates the Talos cluster using the SDK.
//
//nolint:ireturn // provision.Cluster is the SDK's interface
func (p *TalosProvisioner) provisionCluster(
	ctx context.Context,
	clusterName string,
	configBundle *bundle.Bundle,
) (provision.Cluster, error) {
	// Create Talos provisioner
	talosProvisioner, err := p.provisionerFactory(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create Talos provisioner: %w", err)
	}

	defer func() { _ = talosProvisioner.Close() }()

	// Build cluster request with node configs from bundle
	clusterRequest, err := p.buildClusterRequest(clusterName, configBundle)
	if err != nil {
		return nil, fmt.Errorf("failed to build cluster request: %w", err)
	}

	// Create the cluster using Talos provisioner
	cluster, err := talosProvisioner.Create(
		ctx,
		clusterRequest,
		provision.WithLogWriter(p.logWriter),
		provision.WithTalosConfig(configBundle.TalosConfig()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster: %w", err)
	}

	// Log cluster info
	_, _ = fmt.Fprintf(p.logWriter, "Created Talos cluster %q with %d nodes\n",
		cluster.Info().ClusterName,
		len(cluster.Info().Nodes))

	return cluster, nil
}

// saveClusterConfigs saves talosconfig and kubeconfig if paths are configured.
func (p *TalosProvisioner) saveClusterConfigs(
	ctx context.Context,
	cluster provision.Cluster,
	configBundle *bundle.Bundle,
) error {
	// Save talosconfig if path is configured
	if p.options.TalosconfigPath != "" {
		saveErr := configBundle.TalosConfig().Save(p.options.TalosconfigPath)
		if saveErr != nil {
			return fmt.Errorf("failed to save talosconfig: %w", saveErr)
		}

		_, _ = fmt.Fprintf(p.logWriter, "Saved talosconfig to %s\n", p.options.TalosconfigPath)
	}

	// Bootstrap the cluster and retrieve kubeconfig
	if p.options.KubeconfigPath != "" {
		saveErr := p.bootstrapAndSaveKubeconfig(ctx, cluster, configBundle)
		if saveErr != nil {
			return fmt.Errorf("failed to save kubeconfig: %w", saveErr)
		}
	}

	return nil
}

// checkDockerAvailable verifies that Docker is configured and running.
func (p *TalosProvisioner) checkDockerAvailable(ctx context.Context) error {
	if p.dockerClient == nil {
		return ErrDockerNotAvailable
	}

	// Ping Docker to verify it's actually running
	_, err := p.dockerClient.Ping(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDockerNotAvailable, err)
	}

	return nil
}

// buildClusterRequest creates a provision.ClusterRequest from our config.
func (p *TalosProvisioner) buildClusterRequest(
	clusterName string,
	configBundle *bundle.Bundle,
) (provision.ClusterRequest, error) {
	// Parse the network CIDR
	cidr, err := netip.ParsePrefix(p.options.NetworkCIDR)
	if err != nil {
		return provision.ClusterRequest{}, fmt.Errorf("invalid network CIDR: %w", err)
	}

	// Calculate gateway (first usable IP)
	gatewayIP, err := nthIPInNetwork(cidr, 1)
	if err != nil {
		return provision.ClusterRequest{}, fmt.Errorf("failed to calculate gateway IP: %w", err)
	}

	// State directory for cluster
	stateDir, err := getStateDirectory()
	if err != nil {
		return provision.ClusterRequest{}, fmt.Errorf("failed to get state directory: %w", err)
	}

	// Build node requests with configs from bundle
	nodes, err := p.buildNodeRequests(clusterName, cidr, configBundle)
	if err != nil {
		return provision.ClusterRequest{}, err
	}

	return provision.ClusterRequest{
		Name:           clusterName,
		Image:          p.options.TalosImage,
		StateDirectory: stateDir,
		Network: provision.NetworkRequest{
			Name:         clusterName,
			CIDRs:        []netip.Prefix{cidr},
			GatewayAddrs: []netip.Addr{gatewayIP},
			MTU:          defaultMTU,
		},
		Nodes: nodes,
	}, nil
}

// buildNodeRequests creates node request configurations for control plane and worker nodes.
func (p *TalosProvisioner) buildNodeRequests(
	clusterName string,
	cidr netip.Prefix,
	configBundle *bundle.Bundle,
) ([]provision.NodeRequest, error) {
	nodes := make([]provision.NodeRequest, 0, p.options.ControlPlaneNodes+p.options.WorkerNodes)

	// Control plane nodes - use ControlPlane config from bundle
	for nodeIndex := range p.options.ControlPlaneNodes {
		nodeIP, ipErr := nthIPInNetwork(cidr, nodeIndex+ipv4Offset)
		if ipErr != nil {
			return nil, fmt.Errorf(
				"failed to calculate IP for control-plane-%d: %w",
				nodeIndex+1,
				ipErr,
			)
		}

		nodes = append(nodes, provision.NodeRequest{
			Name:     fmt.Sprintf("%s-control-plane-%d", clusterName, nodeIndex+1),
			Type:     machine.TypeControlPlane,
			IPs:      []netip.Addr{nodeIP},
			Memory:   defaultNodeMemory,
			NanoCPUs: defaultNodeCPUs,
			Config:   configBundle.ControlPlane(),
		})
	}

	// Worker nodes - use Worker config from bundle
	for nodeIndex := range p.options.WorkerNodes {
		nodeIP, ipErr := nthIPInNetwork(cidr, p.options.ControlPlaneNodes+nodeIndex+ipv4Offset)
		if ipErr != nil {
			return nil, fmt.Errorf(
				"failed to calculate IP for worker-%d: %w",
				nodeIndex+1,
				ipErr,
			)
		}

		nodes = append(nodes, provision.NodeRequest{
			Name:     fmt.Sprintf("%s-worker-%d", clusterName, nodeIndex+1),
			Type:     machine.TypeWorker,
			IPs:      []netip.Addr{nodeIP},
			Memory:   defaultNodeMemory,
			NanoCPUs: defaultNodeCPUs,
			Config:   configBundle.Worker(),
		})
	}

	return nodes, nil
}

// resolveClusterName returns the provided name if non-empty, otherwise the cluster name from configs.
func (p *TalosProvisioner) resolveClusterName(name string) string {
	if name != "" {
		return name
	}

	if p.talosConfigs != nil {
		return p.talosConfigs.Name
	}

	return talosconfigmanager.DefaultClusterName
}

// nthIPInNetwork returns the nth IP in the network (1-indexed).
// The offset parameter specifies how many addresses to skip from the network base address.
func nthIPInNetwork(prefix netip.Prefix, offset int) (netip.Addr, error) {
	addr := prefix.Addr()

	// Convert to byte slice for manipulation
	if addr.Is4() {
		ipBytes := addr.As4()
		ipValue := uint32(ipBytes[0])<<byte0Shift |
			uint32(ipBytes[1])<<byte1Shift |
			uint32(ipBytes[2])<<byte2Shift |
			uint32(ipBytes[3])

		// Safe conversion: offset should always be small and positive for valid cluster sizes
		if offset < 0 {
			return netip.Addr{}, ErrNegativeOffset
		}

		//nolint:gosec // G115: offset validated above and bounded by cluster size
		ipValue += uint32(offset)

		return netip.AddrFrom4([4]byte{
			byte(ipValue >> byte0Shift),
			byte(ipValue >> byte1Shift),
			byte(ipValue >> byte2Shift),
			byte(ipValue),
		}), nil
	}

	return netip.Addr{}, ErrIPv6NotSupported
}

// getStateDirectory returns the state directory for Talos clusters.
func getStateDirectory() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	stateDir := filepath.Join(homeDir, ".talos", "clusters")

	mkdirErr := os.MkdirAll(stateDir, stateDirectoryPermissions)
	if mkdirErr != nil {
		return "", fmt.Errorf("failed to create state directory: %w", mkdirErr)
	}

	return stateDir, nil
}

// talosAPIPort is the Talos apid service port.
const talosAPIPort = 50000

// getMappedTalosAPIEndpoint finds the control plane container and returns the mapped Talos API endpoint.
// On macOS and other non-Linux systems, Docker runs in a VM, so we need to use the mapped port
// via 127.0.0.1 instead of the container's internal IP.
func (p *TalosProvisioner) getMappedTalosAPIEndpoint(
	ctx context.Context,
	clusterName string,
) (string, error) {
	if p.dockerClient == nil {
		return "", ErrDockerNotAvailable
	}

	// Find the control plane container for this cluster
	containers, err := p.dockerClient.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", LabelTalosOwned+"=true"),
			filters.Arg("label", LabelTalosClusterName+"="+clusterName),
			filters.Arg("label", "talos.type=controlplane"),
		),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		return "", fmt.Errorf("%w for cluster %s", ErrNoControlPlane, clusterName)
	}

	// Get the first control plane container (they all have the same port mapping)
	containerID := containers[0].ID

	// Inspect the container to get port mappings
	inspect, err := p.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}

	// Find the mapped port for Talos API (50000/tcp)
	portKey := nat.Port(fmt.Sprintf("%d/tcp", talosAPIPort))

	bindings, ok := inspect.NetworkSettings.Ports[portKey]
	if !ok || len(bindings) == 0 {
		return "", fmt.Errorf("%w for Talos API port %d", ErrNoPortMapping, talosAPIPort)
	}

	// Use the first binding's host port
	hostPort := bindings[0].HostPort

	return net.JoinHostPort("127.0.0.1", hostPort), nil
}

// rewriteKubeconfigEndpoint rewrites all cluster server endpoints in the kubeconfig
// to use the specified endpoint. This is used for Docker-in-VM environments where
// the internal container IPs are not accessible from the host.
// This follows the same pattern as the Talos SDK's mergeKubeconfig function.
func rewriteKubeconfigEndpoint(kubeconfigBytes []byte, endpoint string) ([]byte, error) {
	if endpoint == "" {
		return kubeconfigBytes, nil
	}

	kubeConfig, err := clientcmd.Load(kubeconfigBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	// Rewrite server endpoint for all clusters
	for name := range kubeConfig.Clusters {
		kubeConfig.Clusters[name].Server = endpoint
	}

	// Serialize back to YAML
	result, err := clientcmd.Write(*kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize kubeconfig: %w", err)
	}

	return result, nil
}

// cleanupKubeconfig removes the cluster, context, and user entries for the deleted cluster
// from the kubeconfig file. This only removes entries matching the cluster name,
// leaving other cluster configurations intact.
func (p *TalosProvisioner) cleanupKubeconfig(clusterName string) error {
	// Expand tilde in kubeconfig path
	kubeconfigPath, err := iopath.ExpandHomePath(p.options.KubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to expand kubeconfig path: %w", err)
	}

	// Talos uses "admin@<cluster-name>" format for context and user names
	contextName := "admin@" + clusterName
	userName := contextName

	err = k8s.CleanupKubeconfig(
		kubeconfigPath,
		clusterName,
		contextName,
		userName,
		p.logWriter,
	)
	if err != nil {
		return fmt.Errorf("failed to cleanup kubeconfig: %w", err)
	}

	return nil
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
func (p *TalosProvisioner) waitForHetznerClusterReady(
	ctx context.Context,
	clusterName string,
	controlPlaneServers []*hcloud.Server,
	workerServers []*hcloud.Server,
	configBundle *bundle.Bundle,
) error {
	// Build the first control-plane endpoint for Kubernetes API access
	kubeEndpoint := fmt.Sprintf("https://%s:6443", controlPlaneServers[0].PublicNet.IPv4.IP.String())

	// Create HetznerClusterResult which implements provision.Cluster
	hetznerCluster, err := NewHetznerClusterResult(
		clusterName,
		controlPlaneServers,
		workerServers,
		kubeEndpoint,
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
	skipNodeReadiness := (p.talosConfigs != nil && p.talosConfigs.IsCNIDisabled()) || p.options.SkipCNIChecks

	var checks []check.ClusterCheck

	if skipNodeReadiness {
		_, _ = fmt.Fprintf(p.logWriter, "  Running pre-boot and K8s component checks (CNI not installed yet)...\n")
		// Use PreBootSequence + K8sComponentsReadiness checks (skips K8sAllNodesReady)
		checks = slices.Concat(check.PreBootSequenceChecks(), check.K8sComponentsReadinessChecks())
	} else {
		_, _ = fmt.Fprintf(p.logWriter, "  Running full cluster readiness checks...\n")
		// Use full DefaultClusterChecks which includes all readiness checks
		checks = check.DefaultClusterChecks()
	}

	// Create a reporter that logs to our log writer
	reporter := &hetznerCheckReporter{writer: p.logWriter}

	// Run the checks using upstream check.Wait()
	// This is the same pattern used by talosctl and Docker provisioner
	checkCtx, cancel := context.WithTimeout(ctx, clusterReadinessTimeout)
	defer cancel()

	if err := check.Wait(checkCtx, clusterAccess, checks, reporter); err != nil {
		return fmt.Errorf("cluster readiness checks failed: %w", err)
	}

	return nil
}

// waitForHetznerClusterReadyAfterStart waits for a Hetzner cluster to be ready after starting.
// This is similar to waitForHetznerClusterReady but loads the TalosConfig from disk
// instead of from the config bundle (which is not available during start operations).
func (p *TalosProvisioner) waitForHetznerClusterReadyAfterStart(
	ctx context.Context,
	clusterName string,
) error {
	_, _ = fmt.Fprintf(p.logWriter, "Waiting for cluster to be ready...\n")

	// Get nodes from the infrastructure provider
	nodes, err := p.infraProvider.ListNodes(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	if len(nodes) == 0 {
		return fmt.Errorf("no nodes found for cluster %q", clusterName)
	}

	// Cast to Hetzner provider to get server details
	hetznerProvider, ok := p.infraProvider.(*hetzner.Provider)
	if !ok {
		return fmt.Errorf("infrastructure provider is not a Hetzner provider")
	}

	// Get control-plane and worker servers
	var controlPlaneServers, workerServers []*hcloud.Server

	for _, node := range nodes {
		server, err := hetznerProvider.GetServerByName(ctx, node.Name)
		if err != nil {
			return fmt.Errorf("failed to get server %s: %w", node.Name, err)
		}

		if server == nil {
			continue
		}

		if node.Role == "control-plane" {
			controlPlaneServers = append(controlPlaneServers, server)
		} else {
			workerServers = append(workerServers, server)
		}
	}

	if len(controlPlaneServers) == 0 {
		return fmt.Errorf("no control-plane nodes found for cluster %q", clusterName)
	}

	// Build the kubernetes endpoint from the first control-plane server
	kubeEndpoint := fmt.Sprintf("https://%s:6443", controlPlaneServers[0].PublicNet.IPv4.IP.String())

	// Create HetznerClusterResult which implements provision.Cluster
	hetznerCluster, err := NewHetznerClusterResult(
		clusterName,
		controlPlaneServers,
		workerServers,
		kubeEndpoint,
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

	// Determine which checks to run based on CNI configuration
	skipNodeReadiness := (p.talosConfigs != nil && p.talosConfigs.IsCNIDisabled()) || p.options.SkipCNIChecks

	var checks []check.ClusterCheck

	if skipNodeReadiness {
		_, _ = fmt.Fprintf(p.logWriter, "  Running pre-boot and K8s component checks (CNI not installed yet)...\n")
		checks = slices.Concat(check.PreBootSequenceChecks(), check.K8sComponentsReadinessChecks())
	} else {
		_, _ = fmt.Fprintf(p.logWriter, "  Running full cluster readiness checks...\n")
		checks = check.DefaultClusterChecks()
	}

	reporter := &hetznerCheckReporter{writer: p.logWriter}

	checkCtx, cancel := context.WithTimeout(ctx, clusterReadinessTimeout)
	defer cancel()

	if err := check.Wait(checkCtx, clusterAccess, checks, reporter); err != nil {
		return fmt.Errorf("cluster readiness checks failed: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Cluster is ready\n")

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

// ensureKernelModules loads required kernel modules for Talos networking.
// On Linux, this loads the br_netfilter module which is required for bridge networking.
// On macOS and Windows, Docker Desktop handles this automatically via its Linux VM.
func (p *TalosProvisioner) ensureKernelModules(ctx context.Context) error {
	// Only needed on Linux - Docker Desktop on macOS/Windows handles this in its VM
	if runtime.GOOS != "linux" {
		return nil
	}

	// Check if br_netfilter is already loaded by reading /proc/modules
	data, err := os.ReadFile("/proc/modules")
	if err == nil {
		// Check if br_netfilter is in the loaded modules list
		if containsModule(string(data), "br_netfilter") {
			return nil // Already loaded
		}
	}

	// Try to load the module using modprobe
	_, _ = fmt.Fprintf(p.logWriter, "Loading br_netfilter kernel module...\n")

	cmd := exec.CommandContext(ctx, "modprobe", "br_netfilter")

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try with sudo if direct modprobe fails (user may not have CAP_SYS_MODULE)
		sudoCmd := exec.CommandContext(ctx, "sudo", "modprobe", "br_netfilter")

		sudoOutput, sudoErr := sudoCmd.CombinedOutput()
		if sudoErr != nil {
			return fmt.Errorf(
				"%w: br_netfilter (modprobe failed: %w, sudo modprobe failed: %w, output: %s)",
				ErrKernelModuleLoadFailed,
				err,
				sudoErr,
				string(append(output, sudoOutput...)),
			)
		}
	}

	_, _ = fmt.Fprintf(p.logWriter, "Successfully loaded br_netfilter kernel module\n")

	return nil
}

// containsModule checks if a module name appears in /proc/modules output.
func containsModule(modulesContent, moduleName string) bool {
	// /proc/modules format: "module_name size refcount deps state offset"
	// Each module is on its own line, and the name is the first field
	for line := range strings.SplitSeq(modulesContent, "\n") {
		if len(line) == 0 {
			continue
		}
		// Get the first field (module name)
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == moduleName {
			return true
		}
	}

	return false
}
