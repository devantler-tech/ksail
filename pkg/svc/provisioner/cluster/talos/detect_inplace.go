package talosprovisioner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configdiff"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	talosresconfig "github.com/siderolabs/talos/pkg/machinery/resources/config"
)

// fingerprintLength is the number of hex characters shown for a machine-config
// fingerprint in the change summary — enough to distinguish two renders at a
// glance without dumping the whole config into the table.
const fingerprintLength = 12

// redactedSecretPlaceholder replaces secret values before diffing/fingerprinting
// so the comparison never trips on PKI and any surfaced diff is safe to print.
const redactedSecretPlaceholder = "<redacted>"

// maxDriftDiffLines caps how much of the machine-config diff is echoed to the
// change summary so it stays readable; the full config is re-applied regardless.
const maxDriftDiffLines = 60

// MachineConfigField is the change field reported when the desired Talos machine
// config differs from what is running on the nodes.
const MachineConfigField = "machine.config"

// errNoRoleConfig is returned when the desired config has no machine config for a
// node's role (control-plane/worker) — an unexpected state for a valid cluster.
var errNoRoleConfig = errors.New("no Talos machine config available for node role")

// errMissingControlPlanePKI is returned when the secrets source used to realign a
// desired config lacks the cluster CA private key. That key lives only on
// control-plane nodes; worker configs carry certificates but no CA private keys.
// Seeding a full config-bundle regeneration from a worker config therefore fails
// deep inside Talos with an opaque "failed to parse PEM block" (#4963). Surfacing
// this named error instead makes the precondition actionable.
var errMissingControlPlanePKI = errors.New(
	"secrets source lacks control-plane PKI (cluster CA private key); " +
		"a control-plane node's config is required to realign worker configs",
)

// detectInPlaceMachineConfigDrift reports whether the desired Talos machine
// config (base config + current patch files) differs from what is running on the
// cluster, returning one in-place change per role (control-plane, worker) that
// has drifted.
//
// Talos patch files (everything under talos/, e.g. sysctls, kubelet config,
// user namespaces, registries, API-server flags) are NOT part of ksail's
// ClusterSpec, so the spec-level diff engine cannot see them. This compares the
// fully *regenerated* desired config against the running node config, which
// catches any patch change generally — additions, edits, AND removals (a key
// dropped from a patch file is simply absent from the regenerated config).
//
// Both a control-plane and a worker node are inspected so that role-scoped
// patches are detected on the role they actually target: worker-scoped patches
// (talos/workers/) never reach the control-plane config, so a control-plane-only
// check would silently miss them — and the apply path, which is already
// role-correct, would never run because nothing was detected. The control-plane
// node covers cluster-wide (talos/cluster/) and control-plane-scoped
// (talos/control-planes/) patches; the worker node covers cluster-wide and
// worker-scoped patches.
//
// The desired config is realigned with the running cluster's PKI and endpoint,
// and the node-managed sections that ksail injects post-generation at create
// (registry-mirror endpoints, cert SANs — see buildDesiredNodeConfig) are grafted
// from the running config, so none of those read as drift. The diff itself uses
// configdiff.DiffConfigs — the canonical, comments-stripped diff that
// `talosctl apply-config --dry-run` uses — with secrets redacted on both sides.
//
// applyInPlaceConfigChanges applies the very same desired config to every node,
// so detection and apply are consistent (including removals).
//
// Caveats:
//   - Omni-managed clusters are skipped (Omni owns node configuration).
func (p *Provisioner) detectInPlaceMachineConfigDrift(
	ctx context.Context,
	clusterName string,
) ([]clusterupdate.Change, error) {
	if p.talosConfigs == nil || p.omniOpts != nil {
		return nil, nil
	}

	// List the cluster's nodes once; both the control-plane and worker drift checks
	// resolve their representative node from this single listing.
	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to discover nodes for config comparison: %w", err)
	}

	// The cluster PKI (CA private key) needed to realign any regenerated config
	// lives only on control-plane nodes; resolve one control-plane config up front
	// and reuse it as the secrets source for both drift checks (worker configs
	// carry no CA private key — see #4963).
	cpRunning, found, err := p.fetchNodeConfigForRole(ctx, nodes, RoleControlPlane)
	if err != nil {
		return nil, err
	}

	// No reachable control-plane node (e.g. cluster not yet up): nothing to compare.
	if !found {
		return nil, nil
	}

	// Control-plane node: catches cluster-wide and control-plane-scoped patch drift.
	changes, err := p.detectRoleMachineConfigDrift(cpRunning, cpRunning, RoleControlPlane)
	if err != nil {
		return nil, err
	}

	// Worker node: catches worker-scoped patch drift, invisible to the
	// control-plane check above. The control-plane config supplies the PKI.
	//
	// Best-effort: a worker being unreachable must not suppress the control-plane
	// drift already detected (and pushed), so a worker-detection failure is logged
	// and skipped rather than aborting the whole detection.
	workerChanges, workerErr := p.detectWorkerMachineConfigDrift(ctx, nodes, cpRunning)
	if workerErr != nil {
		_, _ = fmt.Fprintf(
			p.logWriter,
			"  ⚠ Failed to detect worker machine config drift: %v\n",
			workerErr,
		)

		return changes, nil
	}

	return append(changes, workerChanges...), nil
}

// detectWorkerMachineConfigDrift compares the desired worker config (base +
// cluster/worker patch files) against a running worker node, returning a change
// when they differ. It is a no-op (no changes) when the cluster has no worker
// nodes. secretsSource must be a control-plane config: worker configs carry no
// cluster CA private key, so the desired-config rebuild is seeded from the
// control-plane PKI (#4963), exactly as the apply path threads it.
func (p *Provisioner) detectWorkerMachineConfigDrift(
	ctx context.Context,
	nodes []nodeWithRole,
	secretsSource talosconfig.Provider,
) ([]clusterupdate.Change, error) {
	running, found, err := p.fetchNodeConfigForRole(ctx, nodes, RoleWorker)
	if err != nil {
		return nil, err
	}

	if !found {
		return nil, nil
	}

	return p.detectRoleMachineConfigDrift(running, secretsSource, RoleWorker)
}

// detectRoleMachineConfigDrift reports whether the desired Talos machine config
// for the given role (base + the role's patch files) differs from the running
// config of a node in that role, returning a single in-place change when it does
// and no changes when the configs match. secretsSource supplies the cluster PKI
// for the desired-config rebuild and must be a control-plane config (see
// buildDesiredNodeConfig); pass running itself for the control-plane role.
func (p *Provisioner) detectRoleMachineConfigDrift(
	running, secretsSource talosconfig.Provider,
	role string,
) ([]clusterupdate.Change, error) {
	desired, err := p.buildDesiredNodeConfig(running, secretsSource, role)
	if err != nil {
		return nil, err
	}

	diff, err := machineConfigDiff(running, desired)
	if err != nil {
		return nil, err
	}

	if diff == "" {
		return nil, nil
	}

	p.logMachineConfigDrift(diff)

	return []clusterupdate.Change{
		{
			Field:    MachineConfigField,
			OldValue: configFingerprint(running),
			NewValue: configFingerprint(desired),
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason: fmt.Sprintf(
				"Talos machine config (patches) differs from running %s nodes; "+
					"will be re-applied without reboot",
				role,
			),
		},
	}, nil
}

// buildDesiredNodeConfig produces the config ksail wants on a node: the freshly
// regenerated config (base + current patch files) for the node's role, realigned
// with the running cluster's PKI and endpoint, then with the node-managed sections
// grafted from the running config.
//
// Regenerating (rather than patching the running config) is what makes patch
// *removals* detectable: a key dropped from a patch file is simply absent from
// the regenerated config. The graft then restores the settings ksail injects
// post-generation at create — which are not user patches and must not read as
// drift. secretsSource supplies the cluster PKI for realignment and must be a
// control-plane node's config (see the body); pass nil to use running. It returns
// an error if the secrets source lacks control-plane PKI, if secret/endpoint
// alignment fails, or if the desired config has no machine config for the role.
func (p *Provisioner) buildDesiredNodeConfig(
	running talosconfig.Provider,
	secretsSource talosconfig.Provider,
	role string,
) (talosconfig.Provider, error) {
	aligned, err := p.alignSecretsFromSource(running, secretsSource)
	if err != nil {
		return nil, err
	}

	endpointIP := running.Cluster().Endpoint().Hostname()
	if endpointIP != "" {
		aligned, err = aligned.WithEndpoint(endpointIP)
		if err != nil {
			return nil, fmt.Errorf("align endpoint for config comparison: %w", err)
		}
	}

	// Align the Kubernetes version from secretsSource (a control-plane config),
	// not running: a parsed worker config has no explicit kube-apiserver image, so
	// its KubernetesVersionFromProvider resolves to the Talos machinery's bundled
	// default version rather than the cluster's actual running version — which
	// would read as (and apply) a phantom worker kubelet upgrade on every update.
	// The control-plane config carries the authoritative apiserver version. For the
	// control-plane drift path secretsSource == running, so this is a no-op there.
	aligned, err = p.alignKubernetesVersion(aligned, secretsSource)
	if err != nil {
		return nil, err
	}

	desired := aligned.ControlPlane()
	if role == RoleWorker {
		desired = aligned.Worker()
	}

	if desired == nil {
		return nil, fmt.Errorf("%w: %s", errNoRoleConfig, role)
	}

	grafted, err := graftNodeManagedSections(desired, running)
	if err != nil {
		return nil, err
	}

	return graftNodeHostname(grafted, running)
}

// alignSecretsFromSource regenerates the desired config bundle realigned with the
// running cluster's PKI. That PKI must come from a control-plane node: worker
// configs carry only certificates (no CA private keys, no etcd/aggregator/
// service-account material), so seeding the rebuild from a worker config fails
// inside Talos with an opaque "failed to parse PEM block" (#4963). secretsSource
// falls back to running only when no explicit source is given — valid when running
// is itself a control-plane config (e.g. drift detection).
func (p *Provisioner) alignSecretsFromSource(
	running, secretsSource talosconfig.Provider,
) (*talosconfigmanager.Configs, error) {
	if secretsSource == nil {
		secretsSource = running
	}

	if !hasControlPlanePKI(secretsSource) {
		return nil, errMissingControlPlanePKI
	}

	bundle, err := secrets.NewBundleFromConfig(secrets.NewFixedClock(time.Now()), secretsSource)
	if err != nil {
		return nil, fmt.Errorf("derive secrets bundle for config comparison: %w", err)
	}

	aligned, err := p.talosConfigs.WithSecrets(bundle)
	if err != nil {
		return nil, fmt.Errorf("align secrets for config comparison: %w", err)
	}

	return aligned, nil
}

// graftNodeHostname preserves the per-node static hostname
// (machine.network.hostname) that ksail injects post-generation on Hetzner nodes
// via patchTalosHostname at create/scale time — so the Hetzner CCM can match the
// Kubernetes Node to its server. That hostname is neither in the base config nor
// in any user patch, so a freshly regenerated desired config omits it and instead
// carries the SDK's default standalone HostnameConfig document (auto: stable).
// Grafting it here keeps the hostname out of the drift diff and, on apply,
// prevents the node from re-registering under a generated talos-xxxxx name on its
// next reboot (e.g. during a Talos OS upgrade via `ksail cluster update`).
//
// When the running node has no static hostname (e.g. Docker nodes, which derive
// their hostname from the container), desired is returned unchanged so its own
// HostnameConfig document — which already matches running — is preserved.
func graftNodeHostname(
	desired, running talosconfig.Provider,
) (talosconfig.Provider, error) {
	runningRaw := running.RawV1Alpha1()
	if runningRaw == nil {
		return desired, nil
	}

	hostname := runningRaw.Hostname()
	if hostname == "" {
		return desired, nil
	}

	desiredBytes, err := desired.Bytes()
	if err != nil {
		return nil, fmt.Errorf("encode desired config for hostname graft: %w", err)
	}

	// patchTalosHostname sets machine.network.hostname and strips the conflicting
	// standalone HostnameConfig document, mirroring the create-time transform so
	// the grafted config matches what the node already runs.
	patched, err := patchTalosHostname(desiredBytes, hostname)
	if err != nil {
		return nil, fmt.Errorf("graft node hostname %q: %w", hostname, err)
	}

	provider, err := configloader.NewFromBytes(patched)
	if err != nil {
		return nil, fmt.Errorf("reload config after hostname graft: %w", err)
	}

	return provider, nil
}

// hasControlPlanePKI reports whether a config provider carries the cluster CA
// private key. That key is present only on control-plane nodes; worker configs
// include the cluster CA certificate but not its key. Without it, regenerating a
// full Talos config bundle (which re-issues certificates) cannot proceed and Talos
// fails with an opaque "failed to parse PEM block".
func hasControlPlanePKI(provider talosconfig.Provider) bool {
	ca := provider.Cluster().IssuingCA()

	return ca != nil && len(ca.Key) > 0
}

// alignKubernetesVersion renders the desired config at the Kubernetes version
// already running on the cluster when the user has not pinned one
// (spec.cluster.kubernetesVersion). Without this, an unrelated update would
// regenerate the desired config at KSail's built-in default — which, after KSail
// bumps that default, reads as an unrequested (and possibly Talos-incompatible)
// Kubernetes upgrade. When a version is pinned, the pin is left in place so an
// intentional change is still detected and applied.
//
// versionSource is the running config the version is read from; callers pass a
// control-plane config because only it carries the authoritative kube-apiserver
// image tag (a parsed worker config defaults that image to the Talos machinery's
// bundled version).
func (p *Provisioner) alignKubernetesVersion(
	aligned *talosconfigmanager.Configs,
	versionSource talosconfig.Provider,
) (*talosconfigmanager.Configs, error) {
	if p.options != nil && strings.TrimSpace(p.options.KubernetesVersion) != "" {
		return aligned, nil
	}

	runningVersion := talosconfigmanager.KubernetesVersionFromProvider(versionSource)
	if runningVersion == "" {
		return aligned, nil
	}

	updated, err := aligned.WithKubernetesVersion(runningVersion)
	if err != nil {
		return nil, fmt.Errorf("align Kubernetes version for config comparison: %w", err)
	}

	return updated, nil
}

// graftNodeManagedSections copies the machine-config sections that ksail injects
// post-generation — registry mirrors/auth, cert SANs, and the Hetzner HCloud VIP
// endpoint — from the running config into the desired config, so they don't read
// as removable drift. These are node/setup-managed rather than user patch content.
//
// If ksail gains another post-generation machine-config transform, graft its
// section here too (otherwise it will surface as phantom drift). The per-node
// hostname is a separate-document transform, so it is grafted in graftNodeHostname
// rather than here.
//
//nolint:staticcheck // MachineRegistries is deprecated but still functional in Talos v1.x
func graftNodeManagedSections(
	desired, running talosconfig.Provider,
) (talosconfig.Provider, error) {
	runningRaw := running.RawV1Alpha1()
	if runningRaw == nil || runningRaw.MachineConfig == nil {
		return desired, nil
	}

	grafted, err := desired.PatchV1Alpha1(func(cfg *v1alpha1.Config) error {
		if cfg.MachineConfig == nil {
			return nil
		}

		// Registry mirrors + auth: injected by ApplyMirrorRegistries at create.
		cfg.MachineConfig.MachineRegistries = runningRaw.MachineConfig.MachineRegistries
		// Cert SANs: appended by WithCertSANs at create (e.g. DinD exposure address).
		cfg.MachineConfig.MachineCertSANs = runningRaw.MachineConfig.MachineCertSANs
		graftHCloudVIP(cfg.MachineConfig, runningRaw.MachineConfig)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("graft node-managed config sections: %w", err)
	}

	return grafted, nil
}

// graftHCloudVIP preserves the runtime-injected Hetzner VIP on its network
// interface. When the desired config already declares that interface, only the
// VIP and its DHCP prerequisite are grafted so unrelated user-owned interface
// changes remain visible as drift.
//
//nolint:staticcheck // Talos v1alpha1 machine networking remains the active config API
func graftHCloudVIP(desired, running *v1alpha1.MachineConfig) {
	if running.MachineNetwork == nil || machineNetworkHasHCloudVIP(desired.MachineNetwork) {
		return
	}

	for _, runningDevice := range running.MachineNetwork.NetworkInterfaces {
		if !deviceHasHCloudVIP(runningDevice) {
			continue
		}

		if desired.MachineNetwork == nil {
			desired.MachineNetwork = new(v1alpha1.NetworkConfig)
		}

		desiredDevice := networkDeviceByInterface(
			desired.MachineNetwork.NetworkInterfaces,
			runningDevice.DeviceInterface,
		)
		if desiredDevice == nil {
			runningIdentity := runningDevice.DeepCopy()

			graftedDevice := &v1alpha1.Device{
				DeviceInterface: runningIdentity.DeviceInterface,
				DeviceSelector:  runningIdentity.DeviceSelector,
				DeviceVIPConfig: runningDevice.DeviceVIPConfig.DeepCopy(),
			}
			if runningDevice.DeviceDHCP != nil {
				dhcp := *runningDevice.DeviceDHCP
				graftedDevice.DeviceDHCP = &dhcp
			}

			desired.MachineNetwork.NetworkInterfaces = append(
				desired.MachineNetwork.NetworkInterfaces,
				graftedDevice,
			)

			continue
		}

		desiredDevice.DeviceVIPConfig = runningDevice.DeviceVIPConfig.DeepCopy()
		if runningDevice.DeviceDHCP != nil {
			dhcp := *runningDevice.DeviceDHCP
			desiredDevice.DeviceDHCP = &dhcp
		}
	}
}

// deviceHasHCloudVIP reports whether a network device carries a runtime
// Hetzner VIP declaration.
func deviceHasHCloudVIP(device *v1alpha1.Device) bool {
	return device != nil && device.DeviceVIPConfig != nil &&
		device.DeviceVIPConfig.HCloudConfig != nil
}

// machineNetworkHasHCloudVIP reports whether the desired config declares an
// authoritative Hetzner VIP that must not be replaced by stale runtime state.
//
//nolint:staticcheck // Talos v1alpha1 machine networking remains the active config API
func machineNetworkHasHCloudVIP(network *v1alpha1.NetworkConfig) bool {
	if network == nil {
		return false
	}

	for _, device := range network.NetworkInterfaces {
		if device != nil && device.DeviceVIPConfig != nil &&
			device.DeviceVIPConfig.HCloudConfig != nil {
			return true
		}
	}

	return false
}

// networkDeviceByInterface returns the declared device with name interfaceName,
// or nil when the desired config does not declare it.
func networkDeviceByInterface(
	devices v1alpha1.NetworkDeviceList,
	interfaceName string,
) *v1alpha1.Device {
	if interfaceName == "" {
		return nil
	}

	for _, device := range devices {
		if device != nil && device.DeviceInterface == interfaceName {
			return device
		}
	}

	return nil
}

// machineConfigDiff returns the Talos-native textual diff between two configs,
// with secrets redacted. An empty string means no difference. It delegates to
// configdiff.DiffConfigs, which encodes both sides with the canonical,
// comments-stripped encoder before diffing.
func machineConfigDiff(oldConfig, newConfig talosconfig.Provider) (string, error) {
	diff, err := configdiff.DiffConfigs(
		oldConfig.RedactSecrets(redactedSecretPlaceholder),
		newConfig.RedactSecrets(redactedSecretPlaceholder),
	)
	if err != nil {
		return "", fmt.Errorf("compute machine config diff: %w", err)
	}

	return diff, nil
}

// logMachineConfigDrift prints the (already secret-redacted) diff so operators
// can see what changed, truncated to keep the change summary readable.
func (p *Provisioner) logMachineConfigDrift(diff string) {
	lines := strings.Split(strings.TrimRight(diff, "\n"), "\n")
	if len(lines) > maxDriftDiffLines {
		omitted := len(lines) - maxDriftDiffLines
		lines = append(
			lines[:maxDriftDiffLines],
			fmt.Sprintf("... (%d more diff lines)", omitted),
		)
	}

	_, _ = fmt.Fprintf(
		p.logWriter,
		"  Machine config drift (secrets redacted):\n%s\n",
		strings.Join(lines, "\n"),
	)
}

// configFingerprint returns a short, stable hex fingerprint of a provider's
// redacted, canonical, comments-stripped encoding — the same normalisation
// machineConfigDiff uses, so equal fingerprints imply an empty diff.
func configFingerprint(provider talosconfig.Provider) string {
	canonical, err := provider.
		RedactSecrets(redactedSecretPlaceholder).
		EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return "unknown"
	}

	sum := sha256.Sum256(canonical)

	return hex.EncodeToString(sum[:])[:fingerprintLength]
}

// fetchRunningControlPlaneConfig discovers a control-plane node and returns its
// running Talos machine config provider. It returns (nil, false, nil) when no
// control-plane node is reachable so callers can treat "cannot compare" as "no
// detected drift" rather than failing the update.
func (p *Provisioner) fetchRunningControlPlaneConfig(
	ctx context.Context,
	clusterName string,
) (talosconfig.Provider, bool, error) {
	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil {
		return nil, false, fmt.Errorf("failed to discover nodes for config comparison: %w", err)
	}

	return p.fetchNodeConfigForRole(ctx, nodes, RoleControlPlane)
}

// fetchNodeConfigForRole returns the running Talos machine config of the first
// node of the given role from an already-discovered node set, so callers that
// need more than one role's config list the cluster's nodes only once. It returns
// (nil, false, nil) when no node of that role is present, so callers can treat
// "cannot compare" as "no detected drift" rather than failing the update.
func (p *Provisioner) fetchNodeConfigForRole(
	ctx context.Context,
	nodes []nodeWithRole,
	role string,
) (talosconfig.Provider, bool, error) {
	var nodeIP string

	for _, node := range nodes {
		if node.Role == role {
			nodeIP = node.IP

			break
		}
	}

	if nodeIP == "" {
		return nil, false, nil
	}

	config, err := p.nodeConfigFetcher(ctx, nodeIP)
	if err != nil {
		return nil, false, err
	}

	return config, true, nil
}

// fetchNodeConfig fetches the running Talos machine config provider from a single
// node by IP. A fresh client is dialed per attempt and the fetch is retried for
// transient gRPC failures (e.g. a flaky TLS handshake to apid) so one dropped
// flow doesn't abort the operation.
func (p *Provisioner) fetchNodeConfig(
	ctx context.Context,
	nodeIP string,
) (talosconfig.Provider, error) {
	var provider talosconfig.Provider

	err := p.retryTransientTalosAPICall(ctx, nodeIP, "Machine config fetch",
		func(ctx context.Context) error {
			talosClient, clientErr := p.createTalosClient(ctx, nodeIP)
			if clientErr != nil {
				return fmt.Errorf("failed to create Talos client for %s: %w", nodeIP, clientErr)
			}

			defer talosClient.Close() //nolint:errcheck

			machineConfig, fetchErr := safe.StateGet[*talosresconfig.MachineConfig](
				ctx,
				talosClient.COSI,
				talosresconfig.NewMachineConfig(nil).Metadata(),
			)
			if fetchErr != nil {
				return fmt.Errorf(
					"failed to fetch running machine config from %s: %w",
					nodeIP,
					fetchErr,
				)
			}

			provider = machineConfig.Provider()

			return nil
		})
	if err != nil {
		return nil, err
	}

	return provider, nil
}
