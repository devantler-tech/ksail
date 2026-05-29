package talosprovisioner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/cosi-project/runtime/pkg/safe"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configdiff"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
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
// config patches differ from what is running on the nodes.
const MachineConfigField = "machine.config"

// detectInPlaceMachineConfigDrift reports whether applying ksail's user patches
// to the running control-plane config would change it, returning a single
// in-place change when it would.
//
// Talos patch files (everything under talos/, e.g. sysctls, kubelet config,
// user namespaces, registries, API-server flags) are NOT part of ksail's
// ClusterSpec, so the spec-level diff engine cannot see them. This overlays the
// user patches onto the node's *running* config and diffs the result against it,
// which catches any patch change (additions and edits) generally — rather than a
// hand-picked set of fields.
//
// Comparing against the running config (rather than a freshly regenerated one)
// is what makes this reliable: the running config already carries every
// create-time/runtime-injected setting (registry-mirror endpoints, PKI, the
// real cluster endpoint), so those never read as drift. Only the user patch
// content surfaces. The diff itself uses configdiff.DiffConfigs — the canonical,
// comments-stripped diff that `talosctl apply-config --dry-run` uses — and both
// sides are secret-redacted as defence-in-depth and to keep the diff safe to log.
//
// applyInPlaceConfigChanges applies the very same running+patches config, so
// detection and apply are consistent.
//
// Caveats:
//   - Patch *removals* are not detected: strategic-merge patches add/override but
//     do not delete keys from the running config, so dropping a key from a patch
//     file is a no-op here (a future enhancement could diff against a full render).
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

	patched, err := applyUserPatches(running, p.talosConfigs.Patches(), RoleControlPlane)
	if err != nil {
		return nil, err
	}

	diff, err := machineConfigDiff(running, patched)
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
			NewValue: configFingerprint(patched),
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "Talos machine config (patches) differs from running nodes; will be re-applied without reboot",
		},
	}, nil
}

// applyUserPatches overlays ksail's user-authored Talos patches (scoped to the
// node role) onto the running config and returns the result. Because the base is
// the running node config, all create-time/runtime-injected settings (mirror
// endpoints, PKI, endpoint) are preserved — only the user patch content is
// applied. Returns the running config unchanged when no patches target the role.
func applyUserPatches(
	running talosconfig.Provider,
	patches []talosconfigmanager.Patch,
	role string,
) (talosconfig.Provider, error) {
	configPatches := make([]configpatcher.Patch, 0, len(patches))

	for _, patch := range patches {
		if !patchAppliesToRole(patch.Scope, role) {
			continue
		}

		loaded, loadErr := configpatcher.LoadPatch(patch.Content)
		if loadErr != nil {
			return nil, fmt.Errorf("load patch %s: %w", patch.Path, loadErr)
		}

		configPatches = append(configPatches, loaded)
	}

	if len(configPatches) == 0 {
		return running, nil
	}

	out, err := configpatcher.Apply(configpatcher.WithConfig(running), configPatches)
	if err != nil {
		return nil, fmt.Errorf("apply patches to running config: %w", err)
	}

	patched, err := out.Config()
	if err != nil {
		return nil, fmt.Errorf("read patched config: %w", err)
	}

	return patched, nil
}

// patchAppliesToRole reports whether a patch's scope targets the given node role.
func patchAppliesToRole(scope talosconfigmanager.PatchScope, role string) bool {
	switch scope {
	case talosconfigmanager.PatchScopeCluster:
		return true
	case talosconfigmanager.PatchScopeControlPlane:
		return role == RoleControlPlane
	case talosconfigmanager.PatchScopeWorker:
		return role == RoleWorker
	default:
		return false
	}
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
