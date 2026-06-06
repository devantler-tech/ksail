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
// control-plane node, returning a single in-place change when it does.
//
// Talos patch files (everything under talos/, e.g. sysctls, kubelet config,
// user namespaces, registries, API-server flags) are NOT part of ksail's
// ClusterSpec, so the spec-level diff engine cannot see them. This compares the
// fully *regenerated* desired config against the running node config, which
// catches any patch change generally — additions, edits, AND removals (a key
// dropped from a patch file is simply absent from the regenerated config).
//
// The desired config is realigned with the running cluster's PKI and endpoint,
// and the node-managed sections that ksail injects post-generation at create
// (registry-mirror endpoints, cert SANs — see buildDesiredNodeConfig) are grafted
// from the running config, so none of those read as drift. The diff itself uses
// configdiff.DiffConfigs — the canonical, comments-stripped diff that
// `talosctl apply-config --dry-run` uses — with secrets redacted on both sides.
//
// applyInPlaceConfigChanges applies the very same desired config, so detection
// and apply are consistent (including removals).
//
// Caveats:
//   - Only a control-plane node is inspected (matching detectDisruptiveConfigChanges);
//     cluster-wide patches under talos/cluster/ apply to every node. Worker-only
//     patch drift is a future enhancement.
//   - Omni-managed clusters are skipped (Omni owns node configuration).
func (p *Provisioner) detectInPlaceMachineConfigDrift(
	ctx context.Context,
	clusterName string,
) ([]clusterupdate.Change, error) {
	if p.talosConfigs == nil || p.omniOpts != nil {
		return nil, nil
	}

	running, found, err := p.fetchRunningControlPlaneConfig(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	// No reachable control-plane node (e.g. cluster not yet up): nothing to compare.
	if !found {
		return nil, nil
	}

	desired, err := p.buildDesiredNodeConfig(running, running, RoleControlPlane)
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
			Reason:   "Talos machine config (patches) differs from running nodes; will be re-applied without reboot",
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
	// The cluster PKI used to realign the regenerated config must come from a
	// control-plane node: worker configs carry only certificates (no CA private
	// keys, no etcd/aggregator/service-account material), so seeding the rebuild
	// from a worker config fails inside Talos with an opaque "failed to parse PEM
	// block" (#4963). Fall back to running only when no explicit source is given —
	// valid when running is itself a control-plane config (e.g. drift detection).
	if secretsSource == nil {
		secretsSource = running
	}

	if !hasControlPlanePKI(secretsSource) {
		return nil, errMissingControlPlanePKI
	}

	bundle := secrets.NewBundleFromConfig(secrets.NewFixedClock(time.Now()), secretsSource)

	aligned, err := p.talosConfigs.WithSecrets(bundle)
	if err != nil {
		return nil, fmt.Errorf("align secrets for config comparison: %w", err)
	}

	endpointIP := running.Cluster().Endpoint().Hostname()
	if endpointIP != "" {
		aligned, err = aligned.WithEndpoint(endpointIP)
		if err != nil {
			return nil, fmt.Errorf("align endpoint for config comparison: %w", err)
		}
	}

	aligned, err = p.alignKubernetesVersion(aligned, running)
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
func (p *Provisioner) alignKubernetesVersion(
	aligned *talosconfigmanager.Configs,
	running talosconfig.Provider,
) (*talosconfigmanager.Configs, error) {
	if p.options != nil && strings.TrimSpace(p.options.KubernetesVersion) != "" {
		return aligned, nil
	}

	runningVersion := talosconfigmanager.KubernetesVersionFromProvider(running)
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
// post-generation at create — registry mirrors/auth and cert SANs — from the
// running config into the desired config, so they don't read as drift. These are
// node/setup-managed (not user patch content); the apply re-pushes them verbatim.
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

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("graft node-managed config sections: %w", err)
	}

	return grafted, nil
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

	var cpIP string

	for _, node := range nodes {
		if node.Role == RoleControlPlane {
			cpIP = node.IP

			break
		}
	}

	if cpIP == "" {
		return nil, false, nil
	}

	config, err := p.fetchNodeConfig(ctx, cpIP)
	if err != nil {
		return nil, false, err
	}

	return config, true, nil
}

// fetchNodeConfig fetches the running Talos machine config provider from a single
// node by IP.
func (p *Provisioner) fetchNodeConfig(
	ctx context.Context,
	nodeIP string,
) (talosconfig.Provider, error) {
	talosClient, err := p.createTalosClient(ctx, nodeIP)
	if err != nil {
		return nil, fmt.Errorf("failed to create Talos client for %s: %w", nodeIP, err)
	}

	defer talosClient.Close() //nolint:errcheck

	machineConfig, err := safe.StateGet[*talosresconfig.MachineConfig](
		ctx,
		talosClient.COSI,
		talosresconfig.NewMachineConfig(nil).Metadata(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch running machine config from %s: %w", nodeIP, err)
	}

	return machineConfig.Provider(), nil
}
