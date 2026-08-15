package clusterupdate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

// ErrWipeRequired is returned when configuration changes require partition wiping
// (e.g., disk encryption migration) and --force was not provided.
var ErrWipeRequired = errors.New("partition wipe required")

// ErrRollingRecreateRequired is returned when configuration changes require a
// rolling node replacement (e.g., a Hetzner server-type change) and --force was
// not provided.
var ErrRollingRecreateRequired = errors.New("rolling node replacement required")

// MinControlPlanesForRollingReplace is the minimum number of control-plane nodes
// required to replace control planes one at a time while preserving etcd quorum.
// With fewer nodes, removing one before its replacement joins would drop the
// cluster below quorum, so a full recreation is required instead.
const MinControlPlanesForRollingReplace = 3

// ControlPlaneServerTypeChangeCategory classifies a control-plane server-type
// change based on the number of running control-plane nodes. Clusters with
// enough redundancy (>= MinControlPlanesForRollingReplace) can roll one node at
// a time; smaller clusters must be recreated to avoid losing etcd quorum.
func ControlPlaneServerTypeChangeCategory(controlPlanes int) ChangeCategory {
	if controlPlanes >= MinControlPlanesForRollingReplace {
		return ChangeCategoryRollingRecreate
	}

	return ChangeCategoryRecreateRequired
}

// WorkerServerTypeChangeCategory classifies a worker server-type change based on
// the number of running worker nodes. When workers exist they are replaced one
// at a time (rolling); with no existing workers the change only affects future
// nodes and is applied in-place.
func WorkerServerTypeChangeCategory(workers int) ChangeCategory {
	if workers >= 1 {
		return ChangeCategoryRollingRecreate
	}

	return ChangeCategoryInPlace
}

// DefaultLocalRegistryAddress is the default local registry address applied by
// the config system when a GitOps engine is configured but no explicit
// --local-registry flag was provided. Provisioners mirror this default in
// GetCurrentConfig to prevent false-positive diffs.
const DefaultLocalRegistryAddress = "localhost:5050"

// UnknownBaselineValue is the sentinel value assigned to a component field whose
// current cluster state could not be read (e.g. the Kubernetes API was
// unreachable). It is deliberately not a valid value for any component enum, so
// it never collides with a genuine configuration value. The diff engine detects
// this sentinel and surfaces the field as an "Unknown" baseline instead of
// fabricating a confident diff against the default value. See MarkComponentsUnknown.
const UnknownBaselineValue = "Unknown"

// ApplyGitOpsLocalRegistryDefault mirrors the config system's GitOps-aware
// default: when a GitOps engine is detected but no local registry is configured,
// default to "localhost:5050". This prevents false-positive diffs when the update
// command compares the detected current state against the desired state.
func ApplyGitOpsLocalRegistryDefault(spec *v1alpha1.ClusterSpec) {
	if spec.LocalRegistry.Registry != "" {
		return
	}

	if spec.GitOpsEngine.IsNone() {
		// No GitOps engine — no default registry needed.
		return
	}

	spec.LocalRegistry.Registry = DefaultLocalRegistryAddress
}

// DefaultCurrentSpec returns a ClusterSpec populated with the default values
// that the config system applies at creation time. Provisioners that cannot
// introspect their running config (Kind, K3d) return this to ensure
// DiffEngine does not report false-positive changes.
func DefaultCurrentSpec(
	distribution v1alpha1.Distribution,
	provider v1alpha1.Provider,
) *v1alpha1.ClusterSpec {
	return &v1alpha1.ClusterSpec{
		Distribution:  distribution,
		Provider:      provider,
		CNI:           v1alpha1.CNIDefault,
		CSI:           v1alpha1.CSIDefault,
		MetricsServer: v1alpha1.MetricsServerDefault,
		LoadBalancer:  v1alpha1.LoadBalancerDefault,
		CertManager:   v1alpha1.CertManagerDisabled,
		PolicyEngine:  v1alpha1.PolicyEngineNone,
		GitOpsEngine:  v1alpha1.GitOpsEngineNone,
	}
}

// MarkComponentsUnknown sets each scalar component-enum field that the detector
// probes from the live cluster (CNI, CSI, MetricsServer, LoadBalancer,
// CertManager, PolicyEngine, GitOpsEngine) to the UnknownBaselineValue sentinel.
// Callers invoke this when component detection fails (e.g. the Kubernetes API is
// unreachable) so that the diff engine surfaces these fields as "Unknown" rather
// than fabricating a confident diff from default values for components that may
// already be installed.
//
// The node autoscaler (Autoscaler.Node) is also detector-derived but is
// intentionally left unmodified here: it is a nested struct rather than a single
// enum, so the diff engine instead skips its diff entirely when the component
// baseline is unknown (see Engine.checkAutoscalerOptionsChange).
func MarkComponentsUnknown(spec *v1alpha1.ClusterSpec) {
	if spec == nil {
		return
	}

	spec.CNI = v1alpha1.CNI(UnknownBaselineValue)
	spec.CSI = v1alpha1.CSI(UnknownBaselineValue)
	spec.MetricsServer = v1alpha1.MetricsServer(UnknownBaselineValue)
	spec.LoadBalancer = v1alpha1.LoadBalancer(UnknownBaselineValue)
	spec.CertManager = v1alpha1.CertManager(UnknownBaselineValue)
	spec.PolicyEngine = v1alpha1.PolicyEngine(UnknownBaselineValue)
	spec.GitOpsEngine = v1alpha1.GitOpsEngine(UnknownBaselineValue)
}

// ApplyDetectedComponents copies the detector-derived component fields (CNI,
// CSI, MetricsServer, LoadBalancer, CertManager, PolicyEngine, GitOpsEngine,
// and the node autoscaler toggle) from a detected spec onto a baseline spec.
// The detector (detector.ComponentDetector.DetectComponents) returns its
// findings as a *v1alpha1.ClusterSpec; this helper centralizes the field list
// so every baseline-building call site copies the same set — adding a newly
// detected component means updating this function (and MarkComponentsUnknown)
// rather than each caller.
func ApplyDetectedComponents(dst, detected *v1alpha1.ClusterSpec) {
	if dst == nil || detected == nil {
		return
	}

	dst.CNI = detected.CNI
	dst.CSI = detected.CSI
	dst.MetricsServer = detected.MetricsServer
	dst.LoadBalancer = detected.LoadBalancer
	dst.CertManager = detected.CertManager
	dst.PolicyEngine = detected.PolicyEngine
	dst.GitOpsEngine = detected.GitOpsEngine
	dst.Autoscaler.Node = detected.Autoscaler.Node
}

// ChangeCategory classifies the impact of a configuration change.
type ChangeCategory int

const (
	// ChangeCategoryInPlace indicates the change can be applied without disruption.
	// Examples: component enable/disable via Helm, Talos config changes that support NO_REBOOT.
	ChangeCategoryInPlace ChangeCategory = iota

	// ChangeCategoryRebootRequired indicates the change requires node reboots.
	// Examples: Talos kernel parameters, CNI provider changes, machine feature toggles.
	ChangeCategoryRebootRequired

	// ChangeCategoryRecreateRequired indicates the cluster must be recreated.
	// Examples: distribution change, provider change, Kind node changes, network CIDR changes.
	ChangeCategoryRecreateRequired

	// ChangeCategoryWipeRequired indicates the change requires partition wiping.
	// Examples: disk encryption migration (LUKS2), changes requiring formatted partitions.
	// These changes are more disruptive than reboots but don't require full cluster recreation.
	ChangeCategoryWipeRequired

	// ChangeCategoryUnknown indicates the current value of a field could not be
	// read from the cluster, so its baseline is unknown. Such entries are
	// informational only: they are displayed in the change summary but never
	// drive an in-place apply or a cluster recreation, because the tool cannot
	// know whether a real change is required.
	ChangeCategoryUnknown

	// ChangeCategoryRollingRecreate indicates the change is applied by replacing
	// nodes one at a time (drain → delete → recreate with the new configuration),
	// without recreating the whole cluster. Example: a Hetzner server-type change
	// on a Talos cluster with enough redundancy to preserve etcd quorum.
	ChangeCategoryRollingRecreate
)

// String returns a human-readable name for the change category.
func (c ChangeCategory) String() string {
	switch c {
	case ChangeCategoryInPlace:
		return "in-place"
	case ChangeCategoryRebootRequired:
		return "reboot-required"
	case ChangeCategoryRecreateRequired:
		return "recreate-required"
	case ChangeCategoryWipeRequired:
		return "wipe-required"
	case ChangeCategoryRollingRecreate:
		return "rolling-recreate"
	case ChangeCategoryUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// Change describes a single detected configuration change.
type Change struct {
	// Field is the configuration field path that changed (e.g., "cluster.cni", "cluster.workers").
	Field string
	// OldValue is the previous value (may be empty for additions).
	OldValue string
	// NewValue is the new value (may be empty for removals).
	NewValue string
	// Category classifies the impact of this change.
	Category ChangeCategory
	// Reason explains why this change has its category.
	Reason string
}

// UpdateResult describes the outcome of a cluster update operation.
type UpdateResult struct {
	// InPlaceChanges lists changes that were applied without disruption.
	InPlaceChanges []Change
	// RebootRequired lists changes that require node reboots.
	RebootRequired []Change
	// RecreateRequired lists changes that require cluster recreation.
	RecreateRequired []Change
	// WipeRequired lists changes that require partition wiping.
	WipeRequired []Change
	// RollingRecreate lists changes applied by replacing nodes one at a time
	// (drain → delete → recreate) without recreating the whole cluster.
	RollingRecreate []Change
	// UnknownBaseline lists fields whose current value could not be read from the
	// cluster. These are informational only and never drive an apply or recreate.
	UnknownBaseline []Change
	// AppliedChanges lists changes that were successfully applied.
	AppliedChanges []Change
	// FailedChanges lists changes that failed to apply.
	FailedChanges []Change
	// RebootsPerformed indicates how many nodes were rebooted.
	RebootsPerformed int
	// ClusterRecreated indicates if the cluster was recreated.
	ClusterRecreated bool
}

// NewEmptyUpdateResult returns a new UpdateResult with all slices initialized.
// Use this factory instead of manually constructing UpdateResult to avoid
// code duplication across provisioner implementations.
func NewEmptyUpdateResult() *UpdateResult {
	return &UpdateResult{
		InPlaceChanges:   make([]Change, 0),
		RebootRequired:   make([]Change, 0),
		RecreateRequired: make([]Change, 0),
		WipeRequired:     make([]Change, 0),
		RollingRecreate:  make([]Change, 0),
		UnknownBaseline:  make([]Change, 0),
		AppliedChanges:   make([]Change, 0),
		FailedChanges:    make([]Change, 0),
	}
}

// NewDiffResult creates an initialized UpdateResult for DiffConfig methods and
// reports whether both specs are non-nil. Callers should return early with the
// result when ok is false.
func NewDiffResult(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
) (*UpdateResult, bool) {
	return NewEmptyUpdateResult(), oldSpec != nil && newSpec != nil
}

// NewUpdateResultFromDiff creates an UpdateResult seeded with diff classification data
// and initialized applied/failed slices for tracking execution outcomes.
func NewUpdateResultFromDiff(diff *UpdateResult) *UpdateResult {
	return &UpdateResult{
		InPlaceChanges:   diff.InPlaceChanges,
		RebootRequired:   diff.RebootRequired,
		RecreateRequired: diff.RecreateRequired,
		WipeRequired:     diff.WipeRequired,
		RollingRecreate:  diff.RollingRecreate,
		UnknownBaseline:  diff.UnknownBaseline,
		AppliedChanges:   make([]Change, 0),
		FailedChanges:    make([]Change, 0),
	}
}

// HasInPlaceChanges returns true if there are any in-place changes.
func (r *UpdateResult) HasInPlaceChanges() bool {
	return len(r.InPlaceChanges) > 0
}

// HasRebootRequired returns true if there are changes requiring reboots.
func (r *UpdateResult) HasRebootRequired() bool {
	return len(r.RebootRequired) > 0
}

// HasRecreateRequired returns true if there are changes requiring recreation.
func (r *UpdateResult) HasRecreateRequired() bool {
	return len(r.RecreateRequired) > 0
}

// HasWipeRequired returns true if there are changes requiring partition wipes.
func (r *UpdateResult) HasWipeRequired() bool {
	return len(r.WipeRequired) > 0
}

// HasRollingRecreate returns true if there are changes that require rolling
// node replacement.
func (r *UpdateResult) HasRollingRecreate() bool {
	return len(r.RollingRecreate) > 0
}

// HasUnknownBaseline returns true if any field's current value could not be read
// from the cluster. Such entries are informational and never drive an apply.
func (r *UpdateResult) HasUnknownBaseline() bool {
	return len(r.UnknownBaseline) > 0
}

// HasFailedChanges returns true if one or more changes failed to apply. A
// non-empty FailedChanges set means the live cluster only partially matches the
// desired spec, so callers must treat the update as failed (non-zero exit)
// rather than reporting success.
func (r *UpdateResult) HasFailedChanges() bool {
	return len(r.FailedChanges) > 0
}

// NeedsUserConfirmation returns true if any changes require user confirmation.
// In-place changes can be applied silently; reboot, recreate, wipe, or rolling
// node replacement require confirmation.
func (r *UpdateResult) NeedsUserConfirmation() bool {
	return r.HasRebootRequired() || r.HasRecreateRequired() ||
		r.HasWipeRequired() || r.HasRollingRecreate()
}

// TotalChanges returns the total number of detected changes.
func (r *UpdateResult) TotalChanges() int {
	return len(r.InPlaceChanges) + len(r.RebootRequired) +
		len(r.RecreateRequired) + len(r.WipeRequired) + len(r.RollingRecreate)
}

// AllChanges returns all detected changes in a single slice.
func (r *UpdateResult) AllChanges() []Change {
	all := make([]Change, 0, r.TotalChanges())
	all = append(all, r.InPlaceChanges...)
	all = append(all, r.RebootRequired...)
	all = append(all, r.RecreateRequired...)
	all = append(all, r.WipeRequired...)
	all = append(all, r.RollingRecreate...)

	return all
}

// UpdateOptions provides configuration for the update operation.
type UpdateOptions struct {
	// Force skips user confirmation for destructive changes. It also authorizes
	// partition-wipe changes (e.g. disk encryption migration) that are detected
	// during apply, so it must reflect an explicit --force, not merely an
	// interactive confirmation of an unrelated change.
	Force bool
	// DryRun shows what would change without applying.
	DryRun bool
	// RollingReboot enables rolling reboots (one node at a time) for reboot-required changes.
	RollingReboot bool
	// AllowRollingRecreate authorizes rolling node replacement (e.g. a Hetzner
	// server-type change). It is gated separately from Force so that confirming a
	// rolling replacement never implicitly authorizes a partition wipe.
	AllowRollingRecreate bool
}

// BeginUpdate runs the shared Update entry sequence: nil-guard the specs,
// compute the provisioner diff, and prepare the mutable result via
// PrepareUpdate. Returns (result, true, nil) when the caller should proceed
// to apply in-place changes.
func BeginUpdate(
	ctx context.Context,
	name string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	opts UpdateOptions,
	recreateErr error,
	diffConfig DiffConfigFunc,
) (*UpdateResult, bool, error) {
	if oldSpec == nil || newSpec == nil {
		return NewEmptyUpdateResult(), false, nil
	}

	diff, diffErr := diffConfig(ctx, name, oldSpec, newSpec)

	return PrepareUpdate(diff, diffErr, opts, recreateErr)
}

// DiffConfigFunc computes a provisioner's configuration diff for an update.
type DiffConfigFunc func(
	ctx context.Context,
	name string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
) (*UpdateResult, error)

// RunUpdate executes a provisioner's whole Update flow: BeginUpdate, then —
// when the preamble says to proceed — the provisioner's apply step, wrapping
// its error with applyWrapMsg. Provisioners whose Update needs no extra
// steps can delegate to this in a single return.
func RunUpdate(
	ctx context.Context,
	name string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	opts UpdateOptions,
	recreateErr error,
	diffConfig DiffConfigFunc,
	apply func(ctx context.Context, name string, result *UpdateResult) error,
	applyWrapMsg string,
) (*UpdateResult, error) {
	result, proceed, prepErr := BeginUpdate(
		ctx, name, oldSpec, newSpec, opts, recreateErr, diffConfig,
	)
	if !proceed {
		return result, prepErr
	}

	err := apply(ctx, name, result)
	if err != nil {
		return result, fmt.Errorf("%s: %w", applyWrapMsg, err)
	}

	return result, nil
}

// PrepareUpdate handles the common update preamble shared by provisioners:
//   - If diffErr is non-nil, return it immediately.
//   - If dry-run, return the diff immediately.
//   - Create a mutable result from the diff.
//   - If recreate-required changes exist, return an error.
//
// Returns (result, true, nil) when the caller should continue applying changes.
// Returns (result, false, nil/err) when the caller should return result as-is.
func PrepareUpdate(
	diff *UpdateResult,
	diffErr error,
	opts UpdateOptions,
	recreateErr error,
) (*UpdateResult, bool, error) {
	if diffErr != nil {
		return nil, false, fmt.Errorf("failed to compute config diff: %w", diffErr)
	}

	if opts.DryRun {
		return diff, false, nil
	}

	result := NewUpdateResultFromDiff(diff)

	if diff.HasRecreateRequired() {
		return result, false, fmt.Errorf("%w: %d changes require recreation",
			recreateErr, len(diff.RecreateRequired))
	}

	// Wipe-required changes need explicit --force confirmation
	if diff.HasWipeRequired() && !opts.Force {
		return result, false, fmt.Errorf(
			"%w: %d changes require partition wipe (use --force to proceed, "+
				"or see https://ksail.devantler.tech/guides/talos-disk-encryption/ for manual steps)",
			ErrWipeRequired,
			len(diff.WipeRequired),
		)
	}

	// Rolling node replacement is disruptive (nodes are drained and recreated one
	// at a time) and needs explicit consent. It is gated on AllowRollingRecreate
	// rather than Force so that authorizing it never implicitly authorizes a
	// partition wipe detected during the same update.
	if diff.HasRollingRecreate() && !opts.AllowRollingRecreate {
		return result, false, fmt.Errorf(
			"%w: %d change(s) require replacing nodes one at a time (use --force to proceed)",
			ErrRollingRecreateRequired,
			len(diff.RollingRecreate),
		)
	}

	return result, true, nil
}

// --- Version Upgrade Types ---

// VersionInfo contains the current Kubernetes and distribution versions of a cluster.
type VersionInfo struct {
	// KubernetesVersion is the running Kubernetes version (e.g., "v1.35.1").
	KubernetesVersion string
	// DistributionVersion is the running distribution version (e.g., "v1.13.0" for Talos).
	// For distributions where Kubernetes and distribution versions are the same
	// (Kind, K3s), this matches KubernetesVersion.
	DistributionVersion string
}

// Upgrader is an optional interface for provisioners that support version upgrades.
// Not all provisioners support in-place upgrades — Kind, K3d, and VCluster require
// cluster recreation for version changes, while Talos supports rolling upgrades.
//
// This interface is defined in the clusterupdate package (rather than the parent
// clusterprovisioner package) to avoid import cycles, since child provisioner
// packages already import clusterupdate and the parent imports them via factory.go.
type Upgrader interface {
	// UpgradeKubernetes upgrades the Kubernetes version on the cluster.
	// For distributions that require recreation, this returns clustererr.ErrRecreationRequired.
	UpgradeKubernetes(ctx context.Context, clusterName string, fromVersion, toVersion string) error

	// UpgradeDistribution upgrades the distribution version on the cluster.
	// For Talos, this performs a rolling OS upgrade via LifecycleService.
	// For Kind/K3d/VCluster, this returns clustererr.ErrRecreationRequired.
	UpgradeDistribution(
		ctx context.Context, clusterName string, fromVersion, toVersion string,
	) error

	// GetCurrentVersions returns the running Kubernetes and distribution versions.
	GetCurrentVersions(ctx context.Context, clusterName string) (*VersionInfo, error)

	// KubernetesImageRef returns the OCI image repository used for Kubernetes
	// version discovery (e.g., "kindest/node", "rancher/k3s").
	KubernetesImageRef() string

	// DistributionImageRef returns the OCI image repository used for distribution
	// version discovery (e.g., "ghcr.io/siderolabs/talos").
	// Returns empty string if the distribution version equals the Kubernetes version.
	DistributionImageRef() string

	// PinnedDistributionVersion returns the distribution version pinned for the
	// cluster, or "" when the distribution version is OCI-discovered and the
	// cluster should follow the latest supported version. Talos pins it via
	// spec.cluster.talos.version; VCluster pins it implicitly to the embedded SDK
	// chart version (it cannot be changed by recreation). Kind/K3d have no separate
	// distribution image (their version is the Kubernetes version) and return "".
	PinnedDistributionVersion() string

	// PinnedKubernetesVersion returns the Kubernetes version pinned implicitly by
	// the distribution itself (independently of spec.cluster.kubernetesVersion), or
	// "" when the Kubernetes version is OCI-discovered. VCluster bakes its
	// Kubernetes version into the embedded SDK image, so it cannot be reached by a
	// discovery-driven recreation and is reported here; Talos/Kind/K3d return "".
	PinnedKubernetesVersion() string

	// VersionSuffix returns the tag suffix used by the distribution (e.g., "k3s" for K3s).
	// Returns empty string for distributions that use plain semver tags.
	VersionSuffix() string

	// PrepareConfigForVersion updates the in-memory distribution configuration
	// so that a subsequent cluster recreation uses the specified version.
	// For rolling-upgrade distributions (Talos), this is a no-op.
	PrepareConfigForVersion(upgradeType string, version string) error
}

// ExtractTag returns the tag portion of an OCI image reference, stripping any
// digest suffix (e.g., "v1.35.1" from "kindest/node:v1.35.1@sha256:abc...").
func ExtractTag(image string) string {
	// Strip digest if present (e.g., "@sha256:abc...")
	if digestIdx := strings.Index(image, "@"); digestIdx >= 0 {
		image = image[:digestIdx]
	}

	if idx := strings.LastIndex(image, ":"); idx >= 0 {
		return image[idx+1:]
	}

	return ""
}
