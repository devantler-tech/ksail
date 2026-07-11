package talosprovisioner

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
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
	p.mergeDisruptiveChanges(ctx, name, diff, diffErr)

	// Detect Hetzner server-type changes (rolling node replacement) and merge into
	// diff before PrepareUpdate, so the --force gate and apply phase both see them.
	p.mergeServerTypeRollingChanges(ctx, name, diff, diffErr)

	result, proceed, prepErr := clusterupdate.PrepareUpdate(
		diff, diffErr, opts, clustererr.ErrRecreationRequired,
	)
	if !proceed {
		return result, prepErr //nolint:wrapcheck // error context added in PrepareUpdate
	}

	clusterName := p.resolveClusterName(name)

	return p.applyUpdateChanges(ctx, clusterName, oldSpec, newSpec, diff, result, opts)
}

// mergeDisruptiveChanges detects disruptive config changes (encryption, CNI, disk quota)
// and merges them into the diff before PrepareUpdate.
func (p *Provisioner) mergeDisruptiveChanges(
	ctx context.Context,
	name string,
	diff *clusterupdate.UpdateResult,
	diffErr error,
) {
	if diffErr != nil || diff == nil {
		return
	}

	clusterName := p.resolveClusterName(name)

	wipeChanges, detectErr := p.detectDisruptiveConfigChanges(ctx, clusterName)
	if detectErr != nil {
		_, _ = fmt.Fprintf(
			p.logWriter,
			"  ⚠ Failed to detect disruptive config changes: %v\n",
			detectErr,
		)

		return
	}

	for _, change := range wipeChanges {
		switch change.Category { //nolint:exhaustive // only actionable categories need routing
		case clusterupdate.ChangeCategoryWipeRequired:
			diff.WipeRequired = append(diff.WipeRequired, change)
		case clusterupdate.ChangeCategoryRebootRequired:
			diff.RebootRequired = append(diff.RebootRequired, change)
		case clusterupdate.ChangeCategoryRecreateRequired:
			diff.RecreateRequired = append(diff.RecreateRequired, change)
		default:
			diff.InPlaceChanges = append(diff.InPlaceChanges, change)
		}
	}
}

// mergeServerTypeRollingChanges detects Hetzner control-plane / worker
// server-type changes by comparing the running servers against the desired
// configuration, and merges them into diff so that PrepareUpdate can gate them
// (behind --force) and applyRollingRecreateChanges can apply them. It is a no-op
// for non-Hetzner providers, or when the desired and running types already match.
//
// Change impact is classified from the *current* node inventory (not the desired
// spec), so a control-plane roll is only offered when enough control planes are
// actually present to preserve etcd quorum. A runtime guard in rollingReplaceRole
// re-checks this immediately before any node is deleted.
func (p *Provisioner) mergeServerTypeRollingChanges(
	ctx context.Context,
	name string,
	diff *clusterupdate.UpdateResult,
	diffErr error,
) {
	if diffErr != nil || diff == nil {
		return
	}

	if p.hetznerOpts == nil || p.infraProvider == nil {
		return
	}

	clusterName := p.resolveClusterName(name)

	nodes, listErr := p.infraProvider.ListNodes(ctx, clusterName)
	if listErr != nil {
		_, _ = fmt.Fprintf(
			p.logWriter,
			"  ⚠ Failed to detect server types for rolling update: %v\n",
			listErr,
		)

		return
	}

	cpDesired := p.hetznerServerType(RoleControlPlane)
	workerDesired := p.hetznerServerType(RoleWorker)
	cpCount, workerCount := countServerNodesByRole(nodes)

	appendServerTypeChange(diff, RoleControlPlane,
		representativeServerType(nodes, RoleControlPlane, cpDesired), cpDesired,
		clusterupdate.ControlPlaneServerTypeChangeCategory(cpCount))
	appendServerTypeChange(diff, RoleWorker,
		representativeServerType(nodes, RoleWorker, workerDesired), workerDesired,
		clusterupdate.WorkerServerTypeChangeCategory(workerCount))
}

// appendServerTypeChange appends a server-type change to the appropriate diff
// slice based on its classified category. It is a no-op when the current type is
// unknown (no running nodes of the role), when the types match, or for the
// in-place category (no existing nodes to replace).
func appendServerTypeChange(
	diff *clusterupdate.UpdateResult,
	role, current, desired string,
	category clusterupdate.ChangeCategory,
) {
	if current == "" || desired == "" || strings.EqualFold(current, desired) {
		return
	}

	field := "provider.hetzner.workerServerType"
	if role == RoleControlPlane {
		field = "provider.hetzner.controlPlaneServerType"
	}

	change := clusterupdate.Change{
		Field:    field,
		OldValue: current,
		NewValue: desired,
		Category: category,
		Reason:   "existing " + role + " servers are replaced one at a time to apply the new VM type",
	}

	switch category { //nolint:exhaustive // only rolling/recreate categories are actionable here
	case clusterupdate.ChangeCategoryRollingRecreate:
		diff.RollingRecreate = append(diff.RollingRecreate, change)
	case clusterupdate.ChangeCategoryRecreateRequired:
		change.Reason = "control plane lacks etcd-quorum redundancy to roll; recreation required"
		diff.RecreateRequired = append(diff.RecreateRequired, change)
	default:
		// In-place: no existing nodes to replace; future nodes use the new type.
	}
}

// floatingIPEnabledField is the diff field name for the Hetzner floating-IP
// enablement change, shared by its detector and apply step.
const floatingIPEnabledField = "provider.hetzner.floatingIPEnabled"

// mergeFloatingIPChanges detects a `floatingIPEnabled: true` configuration
// whose ksail-owned floating IP is absent or whose stored control-plane config
// lacks the matching endpoint/VIP block. Both states merge an idempotent
// in-place reconcile change, so a retry can recover when a prior update created
// the address but failed before pushing Talos config (#5947). Cloud state is
// read live because introspection echoes the desired flag and cannot reveal
// address drift. The disable transition is warned about but remains deferred to
// #6032. Ownership collisions propagate as errors rather than being swallowed.
func (p *Provisioner) mergeFloatingIPChanges(
	ctx context.Context,
	name string,
	diff *clusterupdate.UpdateResult,
) error {
	if diff == nil {
		return nil
	}

	floatingIP, detected, err := p.detectOwnedFloatingIP(ctx, name)
	if err != nil {
		return err
	}

	if !detected {
		return nil
	}

	exists := floatingIP != nil

	configured, configDetected := p.detectHetznerFloatingIPConfig(
		ctx, name, floatingIP,
	)
	if !configDetected {
		return nil
	}

	p.mergeDetectedFloatingIPChanges(diff, exists, configured)

	return nil
}

// mergeDetectedFloatingIPChanges records the enabled-state repair or warns
// about the separately tracked disable transition after live detection.
func (p *Provisioner) mergeDetectedFloatingIPChanges(
	diff *clusterupdate.UpdateResult,
	exists, configured bool,
) {
	if p.hetznerOpts.FloatingIPEnabled {
		if exists && configured {
			return
		}

		diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
			Field:    floatingIPEnabledField,
			OldValue: strconv.FormatBool(exists && configured),
			NewValue: strconv.FormatBool(p.hetznerOpts.FloatingIPEnabled),
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   floatingIPReconcileReason(exists),
		})

		return
	}

	if exists {
		_, _ = fmt.Fprintf(
			p.logWriter,
			"  ⚠ floatingIPEnabled is false but the cluster's ksail-owned floating IP"+
				" still exists; cluster update does not reconcile the disable transition"+
				" yet (#6032) — detach and release it via the Hetzner console or CLI if"+
				" it is no longer wanted\n",
		)
	}
}

// detectHetznerFloatingIPConfig detects running endpoint/VIP state only when
// the feature is enabled and its cloud address already exists. Other states need
// no running-config lookup and are considered successfully detected.
func (p *Provisioner) detectHetznerFloatingIPConfig(
	ctx context.Context,
	name string,
	floatingIP *hcloud.FloatingIP,
) (bool, bool) {
	if !p.hetznerOpts.FloatingIPEnabled || floatingIP == nil {
		return false, true
	}

	return p.detectRunningHetznerFloatingIPConfig(
		ctx, p.resolveClusterName(name), floatingIP.IP.String(),
	)
}

// floatingIPReconcileReason describes whether reconciliation creates the cloud
// address or repairs stored Talos endpoint/VIP configuration around one that
// already exists.
func floatingIPReconcileReason(exists bool) string {
	if exists {
		return "the existing floating IP endpoint and missing VIP config are " +
			"regenerated and pushed to control planes without reboot"
	}

	return "the floating IP is created and attached, and control planes " +
		"receive the VIP config without reboot"
}

// detectRunningHetznerFloatingIPConfig reports whether every inventoried
// control plane carries the expected endpoint/VIP state. An individual config
// fetch failure is treated as needing idempotent reconciliation so a partial
// prior apply is retried; inability to list any control planes remains an
// unavailable detection and is skipped.
func (p *Provisioner) detectRunningHetznerFloatingIPConfig(
	ctx context.Context,
	clusterName, expectedIP string,
) (bool, bool) {
	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil {
		_, _ = fmt.Fprintf(
			p.logWriter,
			"  ⚠ Failed to list control planes for floating IP config detection: %v\n",
			err,
		)

		return false, false
	}

	configs := make([]talosconfig.Provider, 0, len(nodes))
	for _, node := range nodes {
		if node.Role != RoleControlPlane {
			continue
		}

		config, fetchErr := p.nodeConfigFetcher(ctx, node.IP)
		if fetchErr != nil {
			_, _ = fmt.Fprintf(
				p.logWriter,
				"  ⚠ Failed to fetch control-plane floating IP config from %s: %v\n",
				node.IP,
				fetchErr,
			)

			return false, true
		}

		configs = append(configs, config)
	}

	if len(configs) == 0 {
		return false, false
	}

	return allControlPlanesHaveHetznerFloatingIPConfig(configs, expectedIP), true
}

// allControlPlanesHaveHetznerFloatingIPConfig reports whether every supplied
// running control-plane config carries the expected endpoint and HCloud VIP.
func allControlPlanesHaveHetznerFloatingIPConfig(
	configs []talosconfig.Provider,
	expectedIP string,
) bool {
	if len(configs) == 0 {
		return false
	}

	for _, config := range configs {
		if !hasHetznerFloatingIPConfig(config, expectedIP) {
			return false
		}
	}

	return true
}

// hasHetznerFloatingIPConfig reports whether config carries a Hetzner-managed
// VIP matching both the expected cloud address and cluster endpoint.
func hasHetznerFloatingIPConfig(config talosconfig.Provider, expectedIP string) bool {
	if config == nil || expectedIP == "" {
		return false
	}

	endpoint := config.Cluster().Endpoint()
	if endpoint == nil || endpoint.Hostname() != expectedIP {
		return false
	}

	for _, device := range config.Machine().Network().Devices() {
		vip := device.VIPConfig()
		if vip != nil && vip.HCloud() != nil && vip.IP() == expectedIP {
			return true
		}
	}

	return false
}

// detectOwnedFloatingIP reads the cluster's ksail-owned floating IP, returning
// the address with a detected flag. detected is false when the answer is
// genuinely unavailable — not a Hetzner cluster, no infra provider, or a
// transient lookup failure — so callers skip rather than act on a guess. An
// ownership collision is definitive and returned as an error.
func (p *Provisioner) detectOwnedFloatingIP(
	ctx context.Context,
	name string,
) (*hcloud.FloatingIP, bool, error) {
	if p.hetznerOpts == nil {
		return nil, false, nil
	}

	hzProvider, isHetzner := p.infraProvider.(*hetzner.Provider)
	if !isHetzner {
		return nil, false, nil
	}

	floatingIP, err := hzProvider.GetOwnedFloatingIP(ctx, p.resolveClusterName(name))
	if err != nil {
		return nil, false, p.classifyFloatingIPDetectionErr(err)
	}

	return floatingIP, true, nil
}

// classifyFloatingIPDetectionErr splits floating-IP detection failures into
// definitive failures (ownership collisions and context termination, which
// must stop the update) and transient provider failures (logged and swallowed,
// so the caller skips rather than acts on a guess).
func (p *Provisioner) classifyFloatingIPDetectionErr(err error) error {
	if errors.Is(err, hetzner.ErrFloatingIPNotOwned) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("failed to detect floating IP state: %w", err)
	}

	_, _ = fmt.Fprintf(
		p.logWriter,
		"  ⚠ Failed to detect floating IP state: %v\n",
		err,
	)

	return nil
}

// hasFloatingIPChange reports whether diff carries the floatingIPEnabled
// in-place change mergeFloatingIPChanges produces.
func hasFloatingIPChange(diff *clusterupdate.UpdateResult) bool {
	if diff == nil {
		return false
	}

	for _, change := range diff.InPlaceChanges {
		if change.Field == floatingIPEnabledField {
			return true
		}
	}

	return false
}

// applyUpdateChanges applies all update changes after PrepareUpdate succeeds by
// running the ordered apply steps (see updateApplySteps) and stopping at the
// first failure.
func (p *Provisioner) applyUpdateChanges(
	ctx context.Context,
	clusterName string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	diff *clusterupdate.UpdateResult,
	result *clusterupdate.UpdateResult,
	opts clusterupdate.UpdateOptions,
) (*clusterupdate.UpdateResult, error) {
	// An explicit --force/--yes also authorizes node drains to delete pods directly,
	// bypassing PodDisruptionBudgets so a rolling reboot/recreate can complete even
	// when a budget would never permit graceful eviction (see drainNode). Scope it to
	// this update by setting it on the (per-invocation) provisioner here.
	p.drainForce = opts.Force

	for _, step := range p.updateApplySteps(clusterName, oldSpec, newSpec, diff, result, opts) {
		err := step.run(ctx)
		if err != nil {
			return result, err
		}
	}

	return result, nil
}

// updateStep is one named step in the post-PrepareUpdate apply sequence. The name
// carries error context and lets tests assert the step ordering.
type updateStep struct {
	name string
	run  func(ctx context.Context) error
}

// updateApplySteps returns the ordered apply steps run after PrepareUpdate
// succeeds. The order is significant and asserted by tests.
//
// The autoscaler tier baseline (ensureAutoscalerSecretIfNeeded) is refreshed
// BEFORE static-node scaling. Autoscaler nodes are a separate, independent tier,
// and on a capacity-constrained cluster a static scale-up can hard-fail at the
// Hetzner project server limit — the very limit the stale autoscaler nodes pin
// the project to. Running the autoscaler refresh first guarantees that a scaling
// (or any later reboot/in-place) failure can never strand the autoscaler on a
// stale machine-config template; otherwise it keeps minting broken nodes that
// hold the project at its server limit and re-wedge every subsequent update at
// the same failing step (#5219).
func (p *Provisioner) updateApplySteps(
	clusterName string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	diff, result *clusterupdate.UpdateResult,
	opts clusterupdate.UpdateOptions,
) []updateStep {
	return []updateStep{
		{"sync Hetzner firewall rules", func(ctx context.Context) error {
			// syncHetznerFirewallRules already wraps its own errors.
			return p.syncHetznerFirewallRules(ctx, clusterName)
		}},
		{"refresh Omni configs", func(ctx context.Context) error {
			return wrapStepErr(p.refreshOmniConfigsIfNeeded(ctx, clusterName),
				"failed to refresh Omni configs before update")
		}},
		{"sync cluster secrets", func(ctx context.Context) error {
			return wrapStepErr(p.syncSecretsFromCluster(ctx, clusterName, oldSpec, newSpec, result),
				"failed to sync cluster secrets")
		}},
		{"apply wipe-required changes", func(ctx context.Context) error {
			// PrepareUpdate already blocks wipe-required changes without --force.
			if !result.HasWipeRequired() {
				return nil
			}

			return wrapStepErr(p.applyWipeRequiredChanges(ctx, clusterName, result),
				"failed to apply wipe-required changes")
		}},
		{"reconcile floating IP endpoint", func(ctx context.Context) error {
			// The autoscaler Secret embeds p.talosConfigs.Worker(), so establish
			// the stable endpoint before rendering that template. Secret sync has
			// already aligned the bundle with the running cluster's PKI.
			return wrapStepErr(p.reconcileFloatingIPEndpoint(ctx, clusterName, diff),
				"failed to reconcile floating IP endpoint")
		}},
		{"ensure autoscaler config secret", func(ctx context.Context) error {
			return wrapStepErr(p.ensureAutoscalerSecretIfNeeded(ctx, clusterName, diff, result),
				"failed to ensure autoscaler config secret")
		}},
		{"apply node scaling changes", func(ctx context.Context) error {
			return wrapStepErr(
				p.applyNodeScalingChanges(ctx, clusterName, oldSpec, newSpec, result),
				"failed to apply node scaling changes",
			)
		}},
		{"apply rolling recreate changes", func(ctx context.Context) error {
			return wrapStepErr(p.applyRollingRecreateChanges(ctx, clusterName, result),
				"failed to apply rolling recreate changes")
		}},
		{"refresh floating IP endpoint after node changes", func(ctx context.Context) error {
			// Re-run the idempotent reconcile after scaling/rolling replacement so
			// the final control-plane set is reflected in certificate SANs and the
			// address is attached to a surviving server. The first reconcile above
			// remains load-bearing for the autoscaler template.
			return wrapStepErr(p.reconcileFloatingIPEndpoint(ctx, clusterName, diff),
				"failed to refresh floating IP endpoint after node changes")
		}},
		{"apply in-place config changes", func(ctx context.Context) error {
			if !p.shouldApplyInPlaceChanges(diff) {
				return nil
			}

			return wrapStepErr(p.applyInPlaceConfigChanges(ctx, clusterName, result),
				"failed to apply in-place config changes")
		}},
		{"apply reboot-required changes", func(ctx context.Context) error {
			return wrapStepErr(p.applyRebootChangesIfNeeded(ctx, clusterName, result, diff, opts),
				"failed to apply reboot-required changes")
		}},
	}
}

// wrapStepErr annotates a failed update step's error with the step's intent,
// returning nil when err is nil so step closures wrap-and-return in a single
// expression instead of repeating the err-check boilerplate.
func wrapStepErr(err error, msg string) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s: %w", msg, err)
}

// DiffConfig computes the differences between current and desired configurations.
//
// Besides comparing the spec-level node counts, it performs a live comparison of
// the regenerated desired machine config (base config + every Talos patch file
// under talos/, with create-time node-managed sections such as registry mirrors
// and cert SANs preserved from the running config) against the running
// control-plane node. Those patches are not part of the ClusterSpec, so this is
// the only place drift in them surfaces — including patch removals — both in the
// change summary and as the trigger for re-pushing config to existing nodes.
func (p *Provisioner) DiffConfig(
	ctx context.Context,
	name string,
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

	p.appendInPlaceMachineConfigDrift(ctx, name, result)

	err := p.appendAutoscalerTemplateDrift(ctx, result)
	if err != nil {
		return result, err
	}

	// Live floating-IP drift must be part of DiffConfig because the CLI uses this
	// result as its preflight gate. Detecting it only inside Update is unreachable
	// when floatingIPEnabled is the sole desired change: the CLI would otherwise
	// report "No changes detected" and never call Update (#5947).
	err = p.mergeFloatingIPChanges(ctx, name, result)
	if err != nil {
		return result, err
	}

	return result, nil
}

// appendAutoscalerTemplateDrift detects drift between the rendered worker
// bootstrap template and the cluster-autoscaler-config Secret, appending an
// in-place change when it differs or is absent so a worker-only talos/ patch
// change is not silently dropped from `cluster update` (#5194). Environmental
// detection failures are non-fatal (see detectAutoscalerTemplateDrift); a
// deterministic render failure (e.g. ErrAutoscalerUserDataTooLarge) is surfaced,
// aborting the update loudly rather than shipping a broken template to future
// autoscaler nodes.
func (p *Provisioner) appendAutoscalerTemplateDrift(
	ctx context.Context,
	result *clusterupdate.UpdateResult,
) error {
	changes, err := p.detectAutoscalerTemplateDrift(ctx)
	if err != nil {
		return err
	}

	result.InPlaceChanges = append(result.InPlaceChanges, changes...)

	return nil
}

// appendInPlaceMachineConfigDrift detects drift between the rendered Talos
// machine config (all patch files) and the running control-plane node, appending
// any change to the diff. Detection failures are non-fatal and logged: they must not
// turn DiffConfig into an error, or callers (computeUpdateDiff, the drift display)
// would drop the spec-level diff entirely. A clean unreachable cluster yields no
// changes (the guard short-circuits before any network call when configs are
// absent, e.g. in unit tests).
func (p *Provisioner) appendInPlaceMachineConfigDrift(
	ctx context.Context,
	name string,
	result *clusterupdate.UpdateResult,
) {
	clusterName := p.resolveClusterName(name)

	changes, err := p.detectInPlaceMachineConfigDrift(ctx, clusterName)
	if err != nil {
		_, _ = fmt.Fprintf(
			p.logWriter,
			"  ⚠ Failed to detect machine config drift for cluster %q: %v\n",
			clusterName,
			err,
		)

		return
	}

	result.InPlaceChanges = append(result.InPlaceChanges, changes...)
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

// applyInPlaceConfigChanges applies user patch changes to running nodes without a
// reboot. For each node it overlays the role-scoped user patches onto the node's
// running config and pushes the result via ApplyConfiguration (NO_REBOOT).
//
// Using running+patches (rather than a freshly regenerated config) preserves the
// create-time/runtime-injected settings the node already carries — registry-mirror
// endpoints, PKI, the real cluster endpoint — which a regenerated config would
// drop. This mirrors detectInPlaceMachineConfigDrift, so detection and apply stay
// consistent.
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

	// The desired-config rebuild needs the cluster PKI, which only a control-plane
	// node carries. Resolve one control-plane config up front and reuse it as the
	// secrets source for every node — seeding the rebuild from a worker's own config
	// fails with "failed to parse PEM block" (#4963). All control-planes share the
	// same PKI, so any one is a valid source.
	secretsSource := p.fetchSecretsSource(ctx, clusterName)

	// On Hetzner, buildDesiredNodeConfig strips any user HostnameConfig and imposes
	// the server-name static hostname for CCM compatibility. Surface that override
	// once per update so a user's talos/cluster/hostname.yaml isn't silently dropped.
	// ControlPlane() returns nil when the bundle is not fully loaded (the guard above
	// only checks p.talosConfigs itself), so nil-check before calling Bytes().
	if p.hetznerOpts != nil {
		cpConfig := p.talosConfigs.ControlPlane()
		if cpConfig != nil {
			cpBytes, bytesErr := cpConfig.Bytes()
			if bytesErr == nil {
				p.warnIfOverridingUserHostname(cpBytes)
			}
		}
	}

	for _, node := range nodes {
		p.applyNodeConfig(ctx, node, secretsSource, result)
	}

	return nil
}

// fetchSecretsSource returns a control-plane node's running config, used to seed
// the per-node desired-config rebuild with the cluster PKI. It returns nil when no
// control-plane is reachable; callers then fall back to each node's own config,
// which carries complete PKI only on control-plane nodes (see buildDesiredNodeConfig
// and #4963).
func (p *Provisioner) fetchSecretsSource(
	ctx context.Context,
	clusterName string,
) talosconfig.Provider {
	cpConfig, found, err := p.fetchRunningControlPlaneConfig(ctx, clusterName)
	if err != nil || !found {
		return nil
	}

	return cpConfig
}

// applyNodeConfig overlays the role-scoped user patches onto a node's running
// config and applies the result (NO_REBOOT), recording success or failure. The
// secretsSource (a control-plane config) supplies the cluster PKI for the rebuild;
// see buildDesiredNodeConfig.
func (p *Provisioner) applyNodeConfig(
	ctx context.Context,
	node nodeWithRole,
	secretsSource talosconfig.Provider,
	result *clusterupdate.UpdateResult,
) {
	running, err := p.fetchNodeConfig(ctx, node.IP)
	if err != nil {
		p.recordNodeConfigFailure(node, result, fmt.Sprintf("fetch running config: %v", err))

		return
	}

	desired, err := p.buildDesiredNodeConfig(running, secretsSource, node.Role)
	if err != nil {
		p.recordNodeConfigFailure(node, result, fmt.Sprintf("build desired config: %v", err))

		return
	}

	err = p.applyConfigWithMode(
		ctx,
		node.IP,
		desired,
		machineapi.ApplyConfigurationRequest_NO_REBOOT,
	)
	if err != nil {
		p.recordNodeConfigFailure(node, result, fmt.Sprintf("apply %s config: %v", node.Role, err))

		return
	}

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

// recordNodeConfigFailure logs and records a failed node config application.
func (p *Provisioner) recordNodeConfigFailure(
	node nodeWithRole,
	result *clusterupdate.UpdateResult,
	reason string,
) {
	_, _ = fmt.Fprintf(
		p.logWriter, "  ⚠ Config update failed on %s (%s): %s\n",
		node.IP, node.Role, reason,
	)

	result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
		Field:    "talos.config",
		NewValue: node.IP,
		Category: clusterupdate.ChangeCategoryInPlace,
		Reason:   reason,
	})
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
//
//nolint:cyclop // sequence of independent conditions that each warrant a secret sync
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

	// Rolling node replacement creates new servers that must join with the
	// existing cluster's PKI and endpoint.
	if diff.HasRollingRecreate() {
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
	// fetchNodeConfig retries transient gRPC failures (flaky TLS handshakes to
	// apid on public IPs). The returned Provider satisfies config.Config.
	runningConfig, err := p.fetchNodeConfig(ctx, cpIP)
	if err != nil {
		return nil, "", err
	}

	existingSecrets, err := secrets.NewBundleFromConfig(
		secrets.NewFixedClock(time.Now()),
		runningConfig,
	)
	if err != nil {
		return nil, "", fmt.Errorf("extracting secrets bundle from running config: %w", err)
	}

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
