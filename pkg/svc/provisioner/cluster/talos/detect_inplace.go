package talosprovisioner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configdiff"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
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

// MachineConfigField is the change field reported when the rendered Talos
// machine config drifts from what is running on the nodes.
const MachineConfigField = "machine.config"

// detectInPlaceMachineConfigDrift reports whether the desired Talos machine
// config differs from what is running on the control-plane node, returning a
// single in-place change when it does.
//
// Talos patch files (everything under talos/, e.g. sysctls, kubelet config,
// user namespaces, registries, API-server flags) are NOT part of ksail's
// ClusterSpec, so the spec-level diff engine cannot see them. This compares the
// desired config against the running node config, which catches any patch change
// — additions, edits, and removals alike — rather than a hand-picked set of
// fields. Surfacing it from DiffConfig both shows the drift in the change
// summary and triggers applyInPlaceConfigChanges, which re-pushes the full
// machine config to every node (NO_REBOOT).
//
// The comparison uses configdiff.DiffConfigs — the same canonical, sorted-key,
// comments-stripped diff that `talosctl apply-config --dry-run` uses — run
// in-process. That normalisation is essential: a naive Bytes() comparison trips
// on the doc/example comments and field ordering that a freshly-rendered config
// carries but a node's stored config does not, producing phantom drift on an
// unchanged cluster.
//
// The desired config is first realigned with the running cluster's PKI and
// endpoint (see alignedDesiredControlPlaneConfig); without that, the
// freshly-generated secrets and CIDR-derived endpoint that p.talosConfigs holds
// at diff time would read as drift on every run. Secrets are additionally
// redacted on both sides before diffing as defence-in-depth and to keep the
// surfaced diff safe to print.
//
// Caveats (accepted: ksail owns the node machine config):
//   - Config not managed by ksail's bundle reads as drift and is reconciled away
//     on the next apply, the same as any other config push.
//   - A ksail/Talos version bump can change the rendered defaults, causing one
//     benign, idempotent re-apply; it is self-limiting once applied.
//   - Only a control-plane node is inspected (matching detectDisruptiveConfigChanges);
//     this covers all cluster-wide patches under talos/cluster/. Worker-only
//     patch drift is a future enhancement.
func (p *Provisioner) detectInPlaceMachineConfigDrift(
	ctx context.Context,
	clusterName string,
) ([]clusterupdate.Change, error) {
	// Omni owns node configuration via its own API; skip when configs are absent.
	if p.talosConfigs == nil || p.omniOpts != nil {
		return nil, nil
	}

	runningConfig, found, err := p.fetchRunningControlPlaneConfig(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	// No reachable control-plane node (e.g. cluster not yet up): nothing to compare.
	if !found {
		return nil, nil
	}

	desiredConfig, err := p.alignedDesiredControlPlaneConfig(runningConfig)
	if err != nil {
		return nil, err
	}

	if desiredConfig == nil {
		return nil, nil
	}

	diff, err := machineConfigDiff(runningConfig, desiredConfig)
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
			OldValue: configFingerprint(runningConfig),
			NewValue: configFingerprint(desiredConfig),
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "Talos machine config (patches) differs from running nodes; will be re-applied without reboot",
		},
	}, nil
}

// machineConfigDiff returns the Talos-native textual diff between the running and
// desired configs, with secrets redacted. An empty string means no drift. It
// delegates to configdiff.DiffConfigs, which encodes both sides with the
// canonical, comments-stripped encoder before diffing.
func machineConfigDiff(runningConfig, desiredConfig talosconfig.Provider) (string, error) {
	diff, err := configdiff.DiffConfigs(
		runningConfig.RedactSecrets(redactedSecretPlaceholder),
		desiredConfig.RedactSecrets(redactedSecretPlaceholder),
	)
	if err != nil {
		return "", fmt.Errorf("compute machine config diff: %w", err)
	}

	return diff, nil
}

// alignedDesiredControlPlaneConfig rebuilds the desired control-plane config with
// the running cluster's PKI (extracted from the running config) and endpoint, so
// the diff reflects only patch/content drift rather than the freshly-generated
// secrets and default endpoint that p.talosConfigs carries at diff time. This
// mirrors syncSecretsFromCluster, which the apply phase relies on so that pushed
// configs match the running cluster's PKI.
func (p *Provisioner) alignedDesiredControlPlaneConfig(
	runningConfig talosconfig.Provider,
) (talosconfig.Provider, error) {
	bundle := secrets.NewBundleFromConfig(secrets.NewFixedClock(time.Now()), runningConfig)

	aligned, err := p.talosConfigs.WithSecrets(bundle)
	if err != nil {
		return nil, fmt.Errorf("align secrets for config drift comparison: %w", err)
	}

	endpointIP := runningConfig.Cluster().Endpoint().Hostname()
	if endpointIP != "" {
		aligned, err = aligned.WithEndpoint(endpointIP)
		if err != nil {
			return nil, fmt.Errorf("align endpoint for config drift comparison: %w", err)
		}
	}

	return aligned.ControlPlane(), nil
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
// detected drift" rather than failing the update. Shared by
// detectDisruptiveConfigChanges and detectInPlaceMachineConfigDrift.
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

	talosClient, err := p.createTalosClient(ctx, cpIP)
	if err != nil {
		return nil, false, fmt.Errorf(
			"failed to create Talos client for config comparison: %w",
			err,
		)
	}

	defer talosClient.Close() //nolint:errcheck

	machineConfig, err := safe.StateGet[*talosresconfig.MachineConfig](
		ctx,
		talosClient.COSI,
		talosresconfig.NewMachineConfig(nil).Metadata(),
	)
	if err != nil {
		return nil, false, fmt.Errorf(
			"failed to fetch running machine config from %s: %w",
			cpIP,
			err,
		)
	}

	return machineConfig.Provider(), true, nil
}
