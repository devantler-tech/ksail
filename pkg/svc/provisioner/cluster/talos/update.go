package talosprovisioner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	svcprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	talosresconfig "github.com/siderolabs/talos/pkg/machinery/resources/config"
)

// Update applies configuration changes to all nodes in a running Talos cluster.
// It implements the ClusterUpdater interface.
func (p *Provisioner) Update(
	ctx context.Context,
	name string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	opts clusterupdate.UpdateOptions,
) (*clusterupdate.UpdateResult, error) {
	// Compute diff to determine what changed
	diff, diffErr := p.DiffConfig(ctx, name, oldSpec, newSpec)

	// Detect disruptive config changes (encryption, CNI, disk quota) and merge
	// into diff before PrepareUpdate, so --dry-run includes wipe-required changes.
	// This runs before secrets sync because it uses the saved talosconfig on disk
	// (not the in-memory configs that need secrets sync).
	if diffErr == nil && diff != nil {
		clusterName := p.resolveClusterName(name)

		wipeChanges, detectErr := p.detectDisruptiveConfigChanges(ctx, clusterName)
		if detectErr != nil {
			_, _ = fmt.Fprintf(p.logWriter, "  ⚠ Failed to detect disruptive config changes: %v\n", detectErr)
		} else {
			for _, change := range wipeChanges {
				switch change.Category {
				case clusterupdate.ChangeCategoryWipeRequired:
					diff.WipeRequired = append(diff.WipeRequired, change)
				case clusterupdate.ChangeCategoryRebootRequired:
					diff.RebootRequired = append(diff.RebootRequired, change)
				default:
					diff.InPlaceChanges = append(diff.InPlaceChanges, change)
				}
			}
		}
	}

	result, proceed, prepErr := clusterupdate.PrepareUpdate(
		diff, diffErr, opts, clustererr.ErrRecreationRequired,
	)
	if !proceed {
		return result, prepErr //nolint:wrapcheck // error context added in PrepareUpdate
	}

	clusterName := p.resolveClusterName(name)

	// Sync Hetzner Cloud Firewall rules to the hardened set.
	// This migrates existing clusters that were created with insecure rules
	// (etcd, kubelet, trustd exposed to 0.0.0.0/0) to the secure configuration.
	syncErr := p.syncHetznerFirewallRules(ctx, clusterName)
	if syncErr != nil {
		return result, syncErr
	}

	// For Omni-managed clusters, refresh kubeconfig and talosconfig before any
	// Helm/K8s operations. cluster create always calls saveOmniConfigs, but
	// cluster update did not, leaving the on-disk kubeconfig stale after token
	// rotation or Omni-side reissuance (Fixes #3922).
	configErr := p.refreshOmniConfigsIfNeeded(ctx, clusterName)
	if configErr != nil {
		return result, fmt.Errorf("failed to refresh Omni configs before update: %w", configErr)
	}

	// Sync in-memory machine configs with the running cluster's PKI secrets.
	// ConfigManager.Load() generates fresh CA/tokens on every call, but scale-up
	// and in-place config changes must use the same secrets as the running cluster
	// to avoid "certificate signed by unknown authority" errors on new nodes.
	secretErr := p.syncSecretsFromCluster(ctx, clusterName, oldSpec, newSpec, result)
	if secretErr != nil {
		return result, fmt.Errorf("failed to sync cluster secrets: %w", secretErr)
	}

	// Wipe-required changes were already detected and merged into the result
	// before PrepareUpdate. Gate on --force before executing the wipe migration.
	if result.HasWipeRequired() && !opts.Force {
		wipeChanges := result.WipeRequired
		_, _ = fmt.Fprintf(p.logWriter, "\n  ⚠ Detected %d change(s) requiring partition wipe:\n", len(wipeChanges))

		for _, change := range wipeChanges {
			_, _ = fmt.Fprintf(p.logWriter, "    • %s: %s → %s (%s)\n",
				change.Field, change.OldValue, change.NewValue, change.Reason)
		}

		_, _ = fmt.Fprintf(p.logWriter, "\n  Use --force to proceed with partition wipe migration.\n")
		_, _ = fmt.Fprintf(p.logWriter, "  Manual procedure: https://docs.siderolabs.com/talos/v1.13/configure-your-talos-cluster/storage-and-disk-management/disk-encryption#going-from-unencrypted-to-encrypted-and-vice-versa\n")

		return result, fmt.Errorf("%w: %d changes require partition wipe (use --force to proceed)",
			clusterupdate.ErrWipeRequired, len(wipeChanges))
	}

	if result.HasWipeRequired() {
		// Force is set — execute wipe migration
		wipeErr := p.applyWipeRequiredChanges(ctx, clusterName, result)
		if wipeErr != nil {
			return result, fmt.Errorf("failed to apply wipe-required changes: %w", wipeErr)
		}
	}

	// Handle node scaling changes
	scaleErr := p.applyNodeScalingChanges(ctx, clusterName, oldSpec, newSpec, result)
	if scaleErr != nil {
		return result, fmt.Errorf("failed to apply node scaling changes: %w", scaleErr)
	}

	// Handle in-place config changes (NO_REBOOT mode).
	// Only re-apply machine configs when the provisioner detected actual changes;
	// component-level changes (e.g. loadBalancer) are handled by the reconciler.
	// Omni manages node configuration through its own API; the diff for Omni clusters
	// only ever contains node-count fields (controlPlanes/workers) which are already
	// handled (and skipped) above, so direct Talos machine config pushes are not needed.
	if p.shouldApplyInPlaceChanges(diff) {
		cfgErr := p.applyInPlaceConfigChanges(ctx, clusterName, result)
		if cfgErr != nil {
			return result, fmt.Errorf("failed to apply in-place config changes: %w", cfgErr)
		}
	}

	// Handle reboot-required changes (STAGED mode with rolling reboot)
	rebootErr := p.applyRebootChangesIfNeeded(ctx, clusterName, result, diff, opts)
	if rebootErr != nil {
		return result, fmt.Errorf("failed to apply reboot-required changes: %w", rebootErr)
	}

	// Talos OS version upgrades are NOT performed here. They are only triggered
	// explicitly via `ksail cluster update --update-distribution`, which goes
	// through the UpgradeDistribution() path. Running applyTalosVersionUpgrade()
	// unconditionally would silently attempt to change the Talos version to
	// KSail's baked-in default, which may differ from what the cluster is
	// actually running (e.g., booted from a Hetzner ISO at a different version).
	// See: https://github.com/devantler-tech/ksail/issues/4260

	// Ensure the cluster-autoscaler-config Secret exists when the node autoscaler
	// is enabled. During cluster create, this secret is created by
	// bootstrapAndFinalize. During update the component reconciler installs the
	// Helm chart but did not previously create the prerequisite secret, causing
	// the autoscaler pod to enter CreateContainerConfigError.
	// See: https://github.com/devantler-tech/ksail/issues/4606
	secretErr = p.ensureAutoscalerSecretIfNeeded(ctx, clusterName)
	if secretErr != nil {
		return result, fmt.Errorf("failed to ensure autoscaler config secret: %w", secretErr)
	}

	return result, nil
}

// DiffConfig computes the differences between current and desired configurations.
func (p *Provisioner) DiffConfig(
	_ context.Context,
	_ string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
) (*clusterupdate.UpdateResult, error) {
	// Talos clusters support in-place changes for most config paths.
	result, ok := clusterupdate.NewDiffResult(oldSpec, newSpec)
	if !ok {
		return result, nil
	}

	// Guard: control-plane count must remain >= 1 regardless of autoscaling.
	if newSpec.ControlPlanes < 1 {
		return nil, ErrMinimumControlPlanes
	}

	// Compare control plane count
	if oldSpec.ControlPlanes != newSpec.ControlPlanes {
		result.InPlaceChanges = append(result.InPlaceChanges, clusterupdate.Change{
			Field:    "controlPlanes",
			OldValue: strconv.Itoa(int(oldSpec.ControlPlanes)),
			NewValue: strconv.Itoa(int(newSpec.ControlPlanes)),
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "control-plane nodes can be added/removed via provider",
		})
	}

	// Compare worker count
	if oldSpec.Workers != newSpec.Workers {
		result.InPlaceChanges = append(result.InPlaceChanges, clusterupdate.Change{
			Field:    "workers",
			OldValue: strconv.Itoa(int(oldSpec.Workers)),
			NewValue: strconv.Itoa(int(newSpec.Workers)),
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "worker nodes can be added/removed via provider",
		})
	}

	return result, nil
}

// applyNodeScalingChanges handles adding or removing Talos nodes.
// For Docker: creates or removes containers with static IPs and Talos config.
// For Hetzner: creates or deletes servers via the Hetzner API.
func (p *Provisioner) applyNodeScalingChanges(
	ctx context.Context,
	clusterName string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *clusterupdate.UpdateResult,
) error {
	if oldSpec == nil || newSpec == nil {
		return nil
	}

	cpDelta := int(newSpec.ControlPlanes - oldSpec.ControlPlanes)
	workerDelta := int(newSpec.Workers - oldSpec.Workers)

	if cpDelta == 0 && workerDelta == 0 {
		return nil
	}

	// Prevent scaling control-plane nodes below 1
	if newSpec.ControlPlanes < 1 {
		return ErrMinimumControlPlanes
	}

	_, _ = fmt.Fprintf(p.logWriter, "  Node scaling for Talos cluster %q: CP %+d, Workers %+d\n",
		clusterName, cpDelta, workerDelta)

	if p.omniOpts != nil {
		return p.scaleOmniByRole(
			ctx, clusterName,
			int(oldSpec.ControlPlanes), int(oldSpec.Workers),
			int(newSpec.ControlPlanes), int(newSpec.Workers),
			result,
		)
	}

	return p.scaleByProvider(ctx, clusterName, cpDelta, workerDelta, result)
}

// scaleByProvider applies node scaling changes using the Docker or Hetzner provider backend.
// Omni scaling is handled separately by scaleOmniByRole before this method is called.
func (p *Provisioner) scaleByProvider(
	ctx context.Context,
	clusterName string,
	cpDelta, workerDelta int,
	result *clusterupdate.UpdateResult,
) error {
	scaleRole := p.scaleDockerByRole
	if p.hetznerOpts != nil {
		scaleRole = p.scaleHetznerByRole
	}

	if cpDelta != 0 {
		err := scaleRole(ctx, clusterName, RoleControlPlane, cpDelta, result)
		if err != nil {
			return err
		}
	}

	if workerDelta != 0 {
		err := scaleRole(ctx, clusterName, RoleWorker, workerDelta, result)
		if err != nil {
			return err
		}
	}

	return nil
}

// applyInPlaceConfigChanges applies configuration changes that don't require reboots.
// Uses ApplyConfiguration with NO_REBOOT mode for Talos-supported fields.
// Control-plane nodes receive the ControlPlane() config and worker nodes receive the Worker() config.
func (p *Provisioner) applyInPlaceConfigChanges(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
) error {
	if p.talosConfigs == nil {
		return nil
	}

	// Get nodes with role information from the cluster
	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to get nodes: %w", err)
	}

	if len(nodes) == 0 {
		_, _ = fmt.Fprintf(p.logWriter, "  No nodes found for cluster %s\n", clusterName)

		return nil
	}

	// Apply the appropriate config to each node based on its role
	for _, node := range nodes {
		config := p.talosConfigs.ControlPlane()
		if node.Role == RoleWorker {
			config = p.talosConfigs.Worker()
		}

		if config == nil {
			_, _ = fmt.Fprintf(
				p.logWriter, "  ⚠ No config available for %s node %s\n",
				node.Role, node.IP,
			)

			continue
		}

		p.applyNodeConfig(ctx, node, config, result)
	}

	return nil
}

// applyNodeConfig applies the appropriate config to a single node and records the result.
func (p *Provisioner) applyNodeConfig(
	ctx context.Context,
	node nodeWithRole,
	config talosconfig.Provider,
	result *clusterupdate.UpdateResult,
) {
	err := p.applyConfigWithMode(
		ctx,
		node.IP,
		config,
		machineapi.ApplyConfigurationRequest_NO_REBOOT,
	)
	if err != nil {
		_, _ = fmt.Fprintf(
			p.logWriter, "  ⚠ Failed to apply config to %s (%s): %v\n",
			node.IP, node.Role, err,
		)

		result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
			Field:    "talos.config",
			NewValue: node.IP,
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   fmt.Sprintf("failed to apply %s config: %v", node.Role, err),
		})
	} else {
		_, _ = fmt.Fprintf(
			p.logWriter, "  ✓ Config applied to %s (%s, no reboot)\n",
			node.IP, node.Role,
		)

		result.AppliedChanges = append(result.AppliedChanges, clusterupdate.Change{
			Field:    "talos.config",
			NewValue: node.IP,
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   node.Role + " config applied successfully",
		})
	}
}

// applyRebootRequiredChanges applies changes that require node reboots.
// Uses rolling reboot strategy: for each node, cordon → drain → apply config
// with STAGED mode → reboot → wait for Ready → uncordon. Workers are processed
// first to minimize control-plane disruption.
func (p *Provisioner) applyRebootRequiredChanges(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
	opts clusterupdate.UpdateOptions,
) error {
	_, _ = fmt.Fprintf(p.logWriter,
		"  %d changes require reboot (rolling=%v)\n",
		len(result.RebootRequired), opts.RollingReboot)

	return p.rollingApplyRebootChanges(ctx, clusterName, result)
}

// applyConfigWithMode applies configuration to a single node with the specified mode.
func (p *Provisioner) applyConfigWithMode(
	ctx context.Context,
	nodeIP string,
	config talosconfig.Provider,
	mode machineapi.ApplyConfigurationRequest_Mode,
) error {
	if config == nil {
		return clustererr.ErrConfigNil
	}

	cfgBytes, err := config.Bytes()
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	talosClient, err := p.createTalosClient(ctx, nodeIP)
	if err != nil {
		return err
	}

	defer talosClient.Close() //nolint:errcheck

	_, err = talosClient.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{
		Data: cfgBytes,
		Mode: mode,
	})
	if err != nil {
		return fmt.Errorf("failed to apply configuration: %w", err)
	}

	return nil
}

// errSavedTalosconfigUnavailable signals that the on-disk talosconfig
// was absent or unreadable. Callers can use [errors.Is] to distinguish
// this expected case from a real client-creation failure and fall
// through to the in-memory PKI bundle.
var errSavedTalosconfigUnavailable = errors.New("saved talosconfig unavailable")

// createTalosClient creates a Talos client for the given node.
// It prefers the saved talosconfig on disk (written during cluster creation)
// because it contains the CA and client certificates the running cluster trusts.
// The in-memory talosConfigs bundle may hold freshly generated PKI that the
// cluster has never seen, so it is used only as a fallback.
func (p *Provisioner) createTalosClient(
	ctx context.Context,
	nodeIP string,
) (*talosclient.Client, error) {
	client, fromSavedConfig, err := p.createTalosClientFromSavedConfig(ctx, nodeIP)
	if err != nil {
		return nil, err
	}

	if fromSavedConfig {
		return client, nil
	}

	return p.createTalosClientFromBundle(ctx, nodeIP)
}

func (p *Provisioner) createTalosClientFromSavedConfig(
	ctx context.Context,
	nodeIP string,
) (*talosclient.Client, bool, error) {
	// Prefer the saved talosconfig (written during cluster creation).
	talosconfigPath, pathErr := canonicalSavedTalosconfigPath(p.options.TalosconfigPath)
	if pathErr != nil {
		if !errors.Is(pathErr, errSavedTalosconfigUnavailable) {
			return nil, false, pathErr
		}

		return nil, false, nil
	}

	if talosconfigPath == "" {
		return nil, false, nil
	}

	client, err := p.tryClientFromSavedConfig(ctx, talosconfigPath, nodeIP)
	if err == nil {
		return client, true, nil
	}

	if errors.Is(err, errSavedTalosconfigUnavailable) {
		return nil, false, nil
	}

	return nil, false, err
}

func (p *Provisioner) createTalosClientFromBundle(
	ctx context.Context,
	nodeIP string,
) (*talosclient.Client, error) {
	// Fallback: use the in-memory bundle's TalosConfig (works for first-time creation).
	if p.talosConfigs != nil && p.talosConfigs.Bundle() != nil {
		if talosConf := p.talosConfigs.Bundle().TalosConfig(); talosConf != nil {
			client, err := talosclient.New(ctx,
				talosclient.WithEndpoints(nodeIP),
				talosclient.WithConfig(talosConf),
			)
			if err != nil {
				return nil, fmt.Errorf("failed to create Talos client with config: %w", err)
			}

			return client, nil
		}
	}

	return nil, clustererr.ErrTalosConfigRequired
}

// canonicalSavedTalosconfigPath returns the canonical on-disk talosconfig path
// when available. A missing path is treated as unavailable so callers can
// fall back to in-memory configuration.
func canonicalSavedTalosconfigPath(rawPath string) (string, error) {
	expandedPath, expandErr := fsutil.ExpandHomePath(rawPath)
	if expandErr != nil {
		return "", fmt.Errorf("%w: %w", errSavedTalosconfigUnavailable, expandErr)
	}

	canonicalPath, canonErr := fsutil.EvalCanonicalPath(expandedPath)
	if canonErr != nil {
		if errors.Is(canonErr, os.ErrNotExist) {
			return "", fmt.Errorf("%w: %w", errSavedTalosconfigUnavailable, canonErr)
		}

		return "", fmt.Errorf(
			"failed to canonicalize talosconfig path %q: %w",
			expandedPath,
			canonErr,
		)
	}

	return canonicalPath, nil
}

// tryClientFromSavedConfig attempts to construct a Talos client from a
// saved talosconfig at talosconfigPath. It returns
// [errSavedTalosconfigUnavailable] when the file cannot be opened so
// the caller can fall through to the in-memory bundle.
func (p *Provisioner) tryClientFromSavedConfig(
	ctx context.Context,
	talosconfigPath, nodeIP string,
) (*talosclient.Client, error) {
	savedCfg, openErr := clientconfig.Open(talosconfigPath)
	if openErr != nil {
		return nil, fmt.Errorf("%w: %w", errSavedTalosconfigUnavailable, openErr)
	}

	caErr := validateCurrentContextCA(savedCfg, talosconfigPath)
	if caErr != nil {
		return nil, caErr
	}

	client, err := talosclient.New(ctx,
		talosclient.WithEndpoints(nodeIP),
		talosclient.WithConfig(savedCfg),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Talos client from saved config: %w", err)
	}

	return client, nil
}

// applyRebootChangesIfNeeded applies reboot-required config changes with a
// rolling reboot when both conditions are met. Returns nil when no reboot
// changes are needed or rolling reboot is disabled.
func (p *Provisioner) applyRebootChangesIfNeeded(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
	diff *clusterupdate.UpdateResult,
	opts clusterupdate.UpdateOptions,
) error {
	if !diff.HasRebootRequired() || !opts.RollingReboot {
		return nil
	}

	return p.applyRebootRequiredChanges(ctx, clusterName, result, opts)
}

// needsSecretSync returns true when the update requires the in-memory configs
// to match the running cluster's PKI. This is needed when pushing machine
// configs to nodes (scale-up, in-place, reboot) or when generating the
// autoscaler config secret (which embeds a worker config derived from the
// bundle). This avoids unnecessary Talos API calls for no-op updates or
// operations that don't touch machine configs (e.g., pure scale-down).
func (p *Provisioner) needsSecretSync(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	diff *clusterupdate.UpdateResult,
) bool {
	if p.talosConfigs == nil || p.omniOpts != nil {
		return false
	}

	// Scale-up: new nodes need the existing cluster's PKI.
	if oldSpec != nil && newSpec != nil &&
		(newSpec.ControlPlanes > oldSpec.ControlPlanes || newSpec.Workers > oldSpec.Workers) {
		return true
	}

	// Node autoscaler: the autoscaler config secret embeds a worker config
	// derived from the bundle. Without syncing, it would contain freshly-generated
	// PKI that doesn't match the running cluster — autoscaler-provisioned nodes
	// would fail to join with "certificate signed by unknown authority".
	if p.hetznerOpts != nil && p.hetznerOpts.NodeAutoscalerEnabled {
		return true
	}

	// In-place or reboot-required config changes push configs to existing nodes.
	return p.shouldApplyInPlaceChanges(diff) || diff.HasRebootRequired()
}

// syncSecretsFromCluster connects to a running control-plane node, fetches its
// machine configuration, extracts the PKI secrets and cluster endpoint, and
// rebuilds the in-memory talosConfigs. This ensures that configs applied to new
// nodes during scale-up use the same CA, tokens, bootstrap secrets, and cluster
// endpoint as the running cluster.
//
// Without secrets sync, ConfigManager.Load() generates fresh PKI on every call,
// causing certificate mismatch errors when new nodes try to join.
// Without endpoint sync, the configs default to a CIDR-derived private IP which
// is unreachable on cloud providers like Hetzner (where the endpoint must be the
// control-plane's public IP).
//
// This is a no-op when no machine configs will be pushed (no scale-up, no in-place
// changes, no reboot-required changes). When secrets ARE needed but no control-plane
// node is available, it fails closed to prevent PKI mismatch.
func (p *Provisioner) syncSecretsFromCluster(
	ctx context.Context,
	clusterName string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	diff *clusterupdate.UpdateResult,
) error {
	if !p.needsSecretSync(oldSpec, newSpec, diff) {
		return nil
	}

	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to discover nodes for secret sync: %w", err)
	}

	var cpIP string

	for _, node := range nodes {
		if node.Role == RoleControlPlane {
			cpIP = node.IP

			break
		}
	}

	if cpIP == "" {
		return fmt.Errorf("%w: cluster %q", ErrNoControlPlaneForSecretSync, clusterName)
	}

	existingSecrets, endpointIP, err := p.fetchClusterSecretsAndEndpoint(ctx, cpIP)
	if err != nil {
		return err
	}

	rebuilt, err := p.talosConfigs.WithSecrets(existingSecrets)
	if err != nil {
		return fmt.Errorf("failed to rebuild configs with cluster secrets: %w", err)
	}

	rebuilt, err = rebuilt.WithEndpoint(endpointIP)
	if err != nil {
		return fmt.Errorf("failed to update configs with cluster endpoint: %w", err)
	}

	p.talosConfigs = rebuilt

	_, _ = fmt.Fprintf(
		p.logWriter,
		"  ✓ Synced cluster secrets and endpoint (%s) from %s\n",
		endpointIP,
		cpIP,
	)

	return nil
}

// fetchClusterSecretsAndEndpoint connects to a control-plane node via the Talos
// API, fetches its running MachineConfig, and extracts the PKI secrets bundle
// and cluster endpoint. The endpoint is read from the running config (not
// derived from node IPs) so that HA clusters with multiple control-plane nodes
// always produce a deterministic endpoint.
func (p *Provisioner) fetchClusterSecretsAndEndpoint(
	ctx context.Context,
	cpIP string,
) (*secrets.Bundle, string, error) {
	talosClient, err := p.createTalosClient(ctx, cpIP)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create Talos client for secret sync: %w", err)
	}

	defer talosClient.Close() //nolint:errcheck

	machineConfig, err := safe.StateGet[*talosresconfig.MachineConfig](
		ctx,
		talosClient.COSI,
		talosresconfig.NewMachineConfig(nil).Metadata(),
	)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch machine config from %s: %w", cpIP, err)
	}

	runningConfig := machineConfig.Config()

	existingSecrets := secrets.NewBundleFromConfig(
		secrets.NewFixedClock(time.Now()),
		runningConfig,
	)

	// Read the endpoint from the running cluster's config rather than deriving
	// it from node IPs. This avoids non-deterministic ordering in HA clusters
	// where getNodesByRole may return a different first CP node between updates.
	// Falls back to cpIP if the running config has no endpoint set.
	endpointIP := runningConfig.Cluster().Endpoint().Hostname()
	if endpointIP == "" {
		endpointIP = cpIP
	}

	return existingSecrets, endpointIP, nil
}

// nodeWithRole holds an IP address and its role for role-aware config application.
type nodeWithRole struct {
	IP   string
	Role string // "control-plane" or "worker"
}

// getNodesByRole returns nodes with their roles for the cluster.
func (p *Provisioner) getNodesByRole(
	ctx context.Context,
	clusterName string,
) ([]nodeWithRole, error) {
	if p.dockerClient != nil {
		return p.getDockerNodesByRole(ctx, clusterName)
	}

	if p.hetznerOpts != nil {
		return p.getHetznerNodesByRole(ctx, clusterName)
	}

	if p.omniOpts != nil {
		return p.getOmniNodesByRole(ctx, clusterName)
	}

	return nil, fmt.Errorf("%w: no provider configured for node listing", ErrDockerNotAvailable)
}

// getHetznerNodesByRole gets node IPs and roles from Hetzner servers.
func (p *Provisioner) getHetznerNodesByRole(
	ctx context.Context,
	clusterName string,
) ([]nodeWithRole, error) {
	if p.infraProvider == nil {
		return nil, nil
	}

	hzProvider, err := p.hetznerProvider()
	if err != nil {
		return nil, err
	}

	listed, err := p.infraProvider.ListNodes(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to list Hetzner nodes: %w", err)
	}

	nodes := make([]nodeWithRole, 0, len(listed))

	for _, node := range listed {
		server, serverErr := hzProvider.GetServerByName(ctx, node.Name)
		if serverErr != nil || server == nil {
			continue
		}

		ip := server.PublicNet.IPv4.IP.String()

		nodes = append(nodes, nodeWithRole{IP: ip, Role: node.Role})
	}

	return nodes, nil
}

// getDockerNodesByRole gets node IPs and roles from Docker containers.
// Role is inferred from container names: names containing "controlplane" are control-plane nodes,
// all others are workers.
func (p *Provisioner) getDockerNodesByRole(
	ctx context.Context,
	clusterName string,
) ([]nodeWithRole, error) {
	if p.dockerClient == nil {
		return nil, clustererr.ErrDockerClientNotConfigured
	}

	containers, err := p.dockerClient.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", LabelTalosClusterName+"="+clusterName),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	nodes := make([]nodeWithRole, 0, len(containers))

	for _, ctr := range containers {
		role := RoleWorker

		for _, name := range ctr.Names {
			// Match both "controlplane" (KSail-scaled nodes) and "control-plane"
			// (Talos SDK-created nodes) naming conventions.
			if strings.Contains(name, "controlplane") || strings.Contains(name, "control-plane") {
				role = RoleControlPlane

				break
			}
		}

		for _, network := range ctr.NetworkSettings.Networks {
			if network.IPAddress != "" {
				nodes = append(nodes, nodeWithRole{
					IP:   network.IPAddress,
					Role: role,
				})

				break
			}
		}
	}

	return nodes, nil
}

// GetCurrentConfig retrieves the current cluster configuration by probing the
// running cluster through the Kubernetes API and Docker/Hetzner/Omni providers.
func (p *Provisioner) GetCurrentConfig(
	ctx context.Context,
) (*v1alpha1.ClusterSpec, *v1alpha1.ProviderSpec, error) {
	var provider v1alpha1.Provider

	switch {
	case p.dockerClient != nil:
		provider = v1alpha1.ProviderDocker
	case p.hetznerOpts != nil:
		provider = v1alpha1.ProviderHetzner
	case p.omniOpts != nil:
		provider = v1alpha1.ProviderOmni
	}

	spec := clusterupdate.DefaultCurrentSpec(v1alpha1.DistributionTalos, provider)

	// Detect installed components from the live cluster when the detector is available.
	if p.componentDetector != nil {
		detected, err := p.componentDetector.DetectComponents(
			ctx,
			v1alpha1.DistributionTalos,
			provider,
		)
		if err == nil {
			spec.CNI = detected.CNI
			spec.CSI = detected.CSI
			spec.MetricsServer = detected.MetricsServer
			spec.LoadBalancer = detected.LoadBalancer
			spec.CertManager = detected.CertManager
			spec.PolicyEngine = detected.PolicyEngine
			spec.GitOpsEngine = detected.GitOpsEngine
			spec.Autoscaler.Node = detected.Autoscaler.Node
		}
	}

	// Introspect actual node counts from the running cluster
	// to avoid false-positive diffs from hardcoded defaults.
	controlPlanes, workers := p.introspectNodeCounts(ctx)
	spec.ControlPlanes = controlPlanes
	spec.Workers = workers

	// Detect the running Talos version from the cluster to avoid
	// false-positive diffs when the user pins a version in config.
	spec.Talos.Version = p.introspectTalosVersion(ctx)

	// Build provider spec if we have Hetzner options configured.
	// Server types are introspected from the running Hetzner servers so
	// that changes (e.g., cx22 -> cx33) appear in the diff. Other Hetzner
	// fields (location, network, SSH key) cannot be introspected, so we
	// echo the desired config as the baseline for those.
	var providerSpec *v1alpha1.ProviderSpec

	if p.hetznerOpts != nil {
		hetznerSpec := *p.hetznerOpts
		p.introspectHetznerServerTypes(ctx, &hetznerSpec)

		providerSpec = &v1alpha1.ProviderSpec{
			Hetzner: hetznerSpec,
		}
	}

	return spec, providerSpec, nil
}

// introspectNodeCounts determines the actual control-plane and worker node
// counts from the running cluster. Falls back to safe defaults (1 CP, 0 workers)
// when the cluster cannot be queried.
func (p *Provisioner) introspectNodeCounts(ctx context.Context) (int32, int32) {
	clusterName := p.resolveClusterName("")

	if p.dockerClient != nil {
		nodes, err := p.getDockerNodesByRole(ctx, clusterName)
		if err == nil {
			return countNodeRoles(nodes)
		}
	}

	if p.hetznerOpts != nil {
		nodes, err := p.getHetznerNodesByRole(ctx, clusterName)
		if err == nil {
			return countNodeRoles(nodes)
		}
	}

	if p.omniOpts != nil {
		nodes, err := p.getOmniNodesByRole(ctx, clusterName)
		if err == nil {
			return countNodeRoles(nodes)
		}
	}

	return 1, 0
}

// introspectTalosVersion queries a control-plane node for the running Talos
// version. Returns an empty string when the version cannot be determined
// (e.g., no Talos API access); in that case the diff engine will report a
// version change if the desired spec specifies a version.
func (p *Provisioner) introspectTalosVersion(ctx context.Context) string {
	clusterName := p.resolveClusterName("")

	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil || len(nodes) == 0 {
		return ""
	}

	// Prefer a control-plane node for the version query; fall back to the
	// first available node if no control-plane node is found.
	target := nodes[0]

	for _, node := range nodes {
		if node.Role == RoleControlPlane {
			target = node

			break
		}
	}

	version, err := p.getRunningTalosVersion(ctx, target.IP)
	if err != nil {
		return ""
	}

	return version
}

// countNodeRoles counts control-plane and worker nodes from a list of nodeWithRole.
func countNodeRoles(nodes []nodeWithRole) (int32, int32) {
	var controlPlanes, workers int32

	for _, n := range nodes {
		switch n.Role {
		case RoleControlPlane:
			controlPlanes++
		case RoleWorker:
			workers++
		}
	}

	if controlPlanes == 0 {
		controlPlanes = 1
	}

	return controlPlanes, workers
}

// introspectHetznerServerTypes populates the ControlPlaneServerType and
// WorkerServerType fields on hetznerSpec from the running Hetzner servers.
func (p *Provisioner) introspectHetznerServerTypes(
	ctx context.Context,
	hetznerSpec *v1alpha1.OptionsHetzner,
) {
	if p.infraProvider == nil {
		return
	}

	clusterName := p.resolveClusterName("")

	nodes, listErr := p.infraProvider.ListNodes(ctx, clusterName)
	if listErr != nil {
		return
	}

	cpType, workerType := detectHetznerServerTypes(nodes)
	if cpType != "" {
		hetznerSpec.ControlPlaneServerType = cpType
	}

	if workerType != "" {
		hetznerSpec.WorkerServerType = workerType
	}
}

// detectHetznerServerTypes determines the actual control-plane and worker
// server types from a node listing. Returns empty strings when no nodes of
// a given role are found.
func detectHetznerServerTypes(nodes []svcprovider.NodeInfo) (string, string) {
	var cpServerType, workerServerType string

	for _, node := range nodes {
		switch node.Role {
		case RoleControlPlane:
			if cpServerType == "" && node.ServerType != "" {
				cpServerType = node.ServerType
			}
		case RoleWorker:
			if workerServerType == "" && node.ServerType != "" {
				workerServerType = node.ServerType
			}
		}

		if cpServerType != "" && workerServerType != "" {
			break
		}
	}

	return cpServerType, workerServerType
}

// syncHetznerFirewallRules synchronizes the Hetzner Cloud Firewall rules to the
// hardened set, migrating clusters created with the old insecure rules.
// No-ops when the provisioner was not initialized with Hetzner opts.
func (p *Provisioner) syncHetznerFirewallRules(
	ctx context.Context,
	clusterName string,
) error {
	if p.hetznerOpts == nil {
		return nil
	}

	hzProvider, err := p.hetznerProvider()
	if err != nil {
		return err
	}

	syncErr := hzProvider.SyncFirewallRules(ctx, clusterName, p.hetznerOpts.AllowedCIDRs)
	if syncErr != nil {
		return fmt.Errorf("failed to sync Hetzner firewall rules for %s: %w", clusterName, syncErr)
	}

	return nil
}

// ensureAutoscalerSecretIfNeeded creates or updates the cluster-autoscaler-config
// Secret when the node autoscaler is enabled on Hetzner. It is a no-op when
// autoscaling is disabled, the provider is not Hetzner, or the config bundle
// is unavailable. Returns ErrAutoscalerRequiresSchematic early when no
// schematic is configured, before performing any side effects.
func (p *Provisioner) ensureAutoscalerSecretIfNeeded(
	ctx context.Context,
	clusterName string,
) error {
	if p.hetznerOpts == nil || !p.hetznerOpts.NodeAutoscalerEnabled || p.talosConfigs == nil {
		return nil
	}

	configBundle := p.talosConfigs.Bundle()
	if configBundle == nil {
		return nil
	}

	// Fail fast: check that a schematic is available before performing
	// side effects (creating secrets, uploading snapshots). The autoscaler
	// requires a snapshot image to provision new nodes.
	if !p.hasSchematicConfigured() {
		return ErrAutoscalerRequiresSchematic
	}

	// Ensure the hcloud secret (token + network) exists. The autoscaler Helm
	// chart references this secret for HCLOUD_TOKEN and HCLOUD_NETWORK.
	err := p.ensureHcloudSecret(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("ensuring hcloud secret for autoscaler: %w", err)
	}

	snapshotImageID, err := p.ensureSnapshotImage(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("looking up snapshot image for autoscaler secret: %w", err)
	}

	return p.ensureAutoscalerSecret(ctx, configBundle, snapshotImageID)
}

// hasSchematicConfigured reports whether a Talos schematic ID is available
// (either explicit via talosOpts.SchematicID or auto-computed from extensions
// via talosConfigs.SchematicID()).
func (p *Provisioner) hasSchematicConfigured() bool {
	if p.talosOpts != nil {
		if strings.TrimSpace(p.talosOpts.SchematicID) != "" {
			return true
		}
	}

	if p.talosConfigs != nil {
		if strings.TrimSpace(p.talosConfigs.SchematicID()) != "" {
			return true
		}
	}

	return false
}
