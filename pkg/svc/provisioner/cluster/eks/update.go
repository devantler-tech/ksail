package eksprovisioner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/detector"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"sigs.k8s.io/yaml"
)

// UpdatableProvisioner wraps Provisioner with the Updater capability:
// managed node-group scaling changes declared in eksctl.yaml are applied
// in-place via `eksctl scale nodegroup` instead of the recreate flow.
//
// It is a separate type (rather than methods on Provisioner) so the
// capability can be gated: the orchestrator discovers Updater by type
// assertion, and the factory only returns this wrapper when
// spec.cluster.eks.experimentalInPlaceUpdates is set. Graduating the flag
// (once the path is validated against a live EKS cluster) means moving the
// methods onto Provisioner and deleting the wrapper and the spec field.
type UpdatableProvisioner struct {
	*Provisioner

	// componentDetector probes the running cluster's components for the
	// update baseline; injected by the orchestrator via SetComponentDetector.
	componentDetector *detector.ComponentDetector
}

// NewUpdatableProvisioner wraps an EKS provisioner with in-place update support.
func NewUpdatableProvisioner(provisioner *Provisioner) *UpdatableProvisioner {
	return &UpdatableProvisioner{Provisioner: provisioner}
}

// SetComponentDetector implements the ComponentDetectorAware capability so
// the orchestrator can inject the live-cluster component detector.
func (u *UpdatableProvisioner) SetComponentDetector(d *detector.ComponentDetector) {
	u.componentDetector = d
}

// managedNodeGroupConfig is the subset of an eksctl.yaml managedNodeGroups
// entry the updater diffs. Pointer fields distinguish "not declared" (nil,
// dimension is skipped) from an explicit zero.
type managedNodeGroupConfig struct {
	Name            string `json:"name"`
	InstanceType    string `json:"instanceType,omitempty"`
	DesiredCapacity *int   `json:"desiredCapacity,omitempty"`
	MinSize         *int   `json:"minSize,omitempty"`
	MaxSize         *int   `json:"maxSize,omitempty"`
}

// Update applies configuration changes to a running EKS cluster.
// The first supported in-place dimension is managed node-group scaling
// (desiredCapacity/minSize/maxSize); everything else reports as
// recreate-required and is handled by the orchestrator's recreate flow.
func (u *UpdatableProvisioner) Update(
	ctx context.Context,
	name string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	opts clusterupdate.UpdateOptions,
) (*clusterupdate.UpdateResult, error) {
	//nolint:wrapcheck // error context added inside RunUpdate.
	return clusterupdate.RunUpdate(
		ctx, name, oldSpec, newSpec, opts, clustererr.ErrRecreationRequired,
		u.DiffConfig, u.applyNodegroupScaling, "failed to scale nodegroups",
	)
}

// DiffConfig computes the differences between the declared eksctl.yaml
// managed node groups and the live cluster state. Scaling changes on an
// existing managed node group are in-place; adding or removing node groups
// is classified recreate-required (not supported in-place yet).
func (u *UpdatableProvisioner) DiffConfig(
	ctx context.Context,
	name string,
	_, _ *v1alpha1.ClusterSpec,
) (*clusterupdate.UpdateResult, error) {
	result := clusterupdate.NewEmptyUpdateResult()

	desired, declared, err := u.desiredNodegroups()
	if err != nil {
		return result, err
	}

	// No config file at all: there is no declared source of truth to diff
	// against, so report nothing rather than flagging live groups as removals.
	if !declared {
		return result, nil
	}

	live, err := u.client.ListNodegroups(ctx, u.resolveName(name), u.region)
	if err != nil {
		return result, fmt.Errorf("failed to list nodegroups: %w", err)
	}

	liveByName := liveManagedNodegroups(live)

	for _, group := range desired {
		liveGroup, exists := liveByName[group.Name]
		if !exists {
			result.RecreateRequired = append(result.RecreateRequired, clusterupdate.Change{
				Field:    nodegroupField(group.Name),
				NewValue: group.Name,
				Category: clusterupdate.ChangeCategoryRecreateRequired,
				Reason:   "adding managed node groups in-place is not supported yet",
			})

			continue
		}

		result.RecreateRequired = append(
			result.RecreateRequired, immutableChanges(group, liveGroup)...,
		)
		result.InPlaceChanges = append(
			result.InPlaceChanges, scalingChanges(group, liveGroup)...,
		)

		delete(liveByName, group.Name)
	}

	for liveName := range liveByName {
		result.RecreateRequired = append(result.RecreateRequired, clusterupdate.Change{
			Field:    nodegroupField(liveName),
			OldValue: liveName,
			Category: clusterupdate.ChangeCategoryRecreateRequired,
			Reason:   "removing managed node groups in-place is not supported yet",
		})
	}

	return result, nil
}

// GetCurrentConfig retrieves the current cluster configuration: component
// state via the injected detector when available (marked Unknown otherwise,
// so the diff engine never fabricates confident component diffs from
// defaults), merged with persisted non-introspectable state.
func (u *UpdatableProvisioner) GetCurrentConfig(
	ctx context.Context,
	clusterName string,
) (*v1alpha1.ClusterSpec, *v1alpha1.ProviderSpec, error) {
	spec := clusterupdate.DefaultCurrentSpec(
		v1alpha1.DistributionEKS,
		v1alpha1.ProviderAWS,
	)

	if u.componentDetector != nil {
		detected, err := u.componentDetector.DetectComponents(
			ctx,
			v1alpha1.DistributionEKS,
			v1alpha1.ProviderAWS,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("detect components: %w", err)
		}

		spec = detected
	} else {
		clusterupdate.MarkComponentsUnknown(spec)
	}

	err := clusterupdate.MergePersistedState(spec, clusterName)
	if err != nil {
		return nil, nil, fmt.Errorf("merge persisted state: %w", err)
	}

	return spec, nil, nil
}

// applyNodegroupScaling converges each declared managed node group's
// scaling toward the eksctl.yaml values via `eksctl scale nodegroup`,
// recording applied and failed changes on the result.
func (u *UpdatableProvisioner) applyNodegroupScaling(
	ctx context.Context,
	name string,
	result *clusterupdate.UpdateResult,
) error {
	clusterName := u.resolveName(name)

	desired, declared, err := u.desiredNodegroups()
	if err != nil {
		return err
	}

	if !declared || len(desired) == 0 {
		return nil
	}

	live, err := u.client.ListNodegroups(ctx, clusterName, u.region)
	if err != nil {
		return fmt.Errorf("failed to list nodegroups: %w", err)
	}

	liveByName := liveManagedNodegroups(live)

	for _, group := range desired {
		liveGroup, exists := liveByName[group.Name]
		if !exists {
			continue
		}

		changes := scalingChanges(group, liveGroup)
		if len(changes) == 0 {
			continue
		}

		err := u.scaleNodegroup(ctx, clusterName, group, liveGroup)
		if err != nil {
			result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
				Field:  nodegroupField(group.Name),
				Reason: fmt.Sprintf("failed to scale nodegroup %s: %v", group.Name, err),
			})

			return fmt.Errorf("failed to scale nodegroup %s: %w", group.Name, err)
		}

		result.AppliedChanges = append(result.AppliedChanges, changes...)
	}

	return nil
}

// scaleNodegroup issues one `eksctl scale nodegroup` call converging the
// live group toward the declared sizes. eksctl requires `--nodes` on every
// scale (its ValidateNumberOfNodes rejects a nil desired capacity), so when
// desiredCapacity is deliberately undeclared — the cluster autoscaler owns
// the current size — the freshly-listed live value is passed, clamped into
// the new min/max bounds. The list-to-scale window is a small inherent race
// against a concurrent autoscaler decision; the experimental flag gates this
// path until it is validated against a live cluster (see the graduation
// issue referenced by the flag's docs).
func (u *UpdatableProvisioner) scaleNodegroup(
	ctx context.Context,
	clusterName string,
	group managedNodeGroupConfig,
	live eksctl.NodegroupSummary,
) error {
	minSize := -1
	if group.MinSize != nil && *group.MinSize != live.MinSize {
		minSize = *group.MinSize
	}

	maxSize := -1
	if group.MaxSize != nil && *group.MaxSize != live.MaxSize {
		maxSize = *group.MaxSize
	}

	desiredCapacity := live.DesiredCap
	if group.DesiredCapacity != nil {
		desiredCapacity = *group.DesiredCapacity
	} else {
		if group.MinSize != nil && desiredCapacity < *group.MinSize {
			desiredCapacity = *group.MinSize
		}

		if group.MaxSize != nil && desiredCapacity > *group.MaxSize {
			desiredCapacity = *group.MaxSize
		}
	}

	//nolint:wrapcheck // wrapped by the caller with the nodegroup name.
	return u.client.ScaleNodegroup(
		ctx, clusterName, group.Name, u.region, desiredCapacity, minSize, maxSize,
	)
}

// desiredNodegroups parses the managedNodeGroups block of the declarative
// eksctl.yaml. The second return reports whether a config file was present
// at all: a present file with no (remaining) managed node groups is a real
// declaration — live managed groups then diff as removals — while a missing
// file means there is nothing declared to diff against.
func (u *UpdatableProvisioner) desiredNodegroups() ([]managedNodeGroupConfig, bool, error) {
	if strings.TrimSpace(u.configPath) == "" {
		return nil, false, nil
	}

	_, err := os.Stat(u.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}

		return nil, false, fmt.Errorf("failed to stat EKS config file: %w", err)
	}

	canonical, err := fsutil.EvalCanonicalPath(u.configPath)
	if err != nil {
		return nil, false, fmt.Errorf("failed to canonicalize EKS config path: %w", err)
	}

	data, err := fsutil.ReadFileSafe(filepath.Dir(canonical), canonical)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read EKS config file: %w", err)
	}

	var config struct {
		ManagedNodeGroups []managedNodeGroupConfig `json:"managedNodeGroups"`
	}

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse EKS config file: %w", err)
	}

	return config.ManagedNodeGroups, true, nil
}

// liveManagedNodegroups indexes the live summaries by name, keeping only
// managed node groups (unmanaged ones are declared under nodeGroups, which
// this updater does not reconcile).
func liveManagedNodegroups(
	live []eksctl.NodegroupSummary,
) map[string]eksctl.NodegroupSummary {
	byName := make(map[string]eksctl.NodegroupSummary, len(live))

	for _, group := range live {
		if group.NodeGroupType != "" && !strings.EqualFold(group.NodeGroupType, "managed") {
			continue
		}

		byName[group.Name] = group
	}

	return byName
}

// immutableChanges returns recreate-required changes for declared node-group
// fields that cannot change in place. An empty live instance type means the
// summary did not report one — unknown is not drift, so the comparison only
// runs when both sides are known.
func immutableChanges(
	group managedNodeGroupConfig,
	live eksctl.NodegroupSummary,
) []clusterupdate.Change {
	if group.InstanceType == "" || live.InstanceType == "" ||
		strings.EqualFold(group.InstanceType, live.InstanceType) {
		return nil
	}

	return []clusterupdate.Change{{
		Field:    nodegroupField(group.Name) + ".instanceType",
		OldValue: live.InstanceType,
		NewValue: group.InstanceType,
		Category: clusterupdate.ChangeCategoryRecreateRequired,
		Reason:   "changing a managed node group's instance type requires replacing the node group",
	}}
}

// scalingChanges returns one in-place change per declared scaling dimension
// that differs from the live nodegroup.
func scalingChanges(
	group managedNodeGroupConfig,
	live eksctl.NodegroupSummary,
) []clusterupdate.Change {
	var changes []clusterupdate.Change

	dimensions := []struct {
		suffix  string
		desired *int
		live    int
	}{
		{"desiredCapacity", group.DesiredCapacity, live.DesiredCap},
		{"minSize", group.MinSize, live.MinSize},
		{"maxSize", group.MaxSize, live.MaxSize},
	}

	for _, dimension := range dimensions {
		if dimension.desired == nil || *dimension.desired == dimension.live {
			continue
		}

		changes = append(changes, clusterupdate.Change{
			Field:    nodegroupField(group.Name) + "." + dimension.suffix,
			OldValue: strconv.Itoa(dimension.live),
			NewValue: strconv.Itoa(*dimension.desired),
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "EKS supports scaling managed node groups in-place",
		})
	}

	return changes
}

// nodegroupField renders the diff field path for a managed node group.
func nodegroupField(name string) string {
	return "eks.managedNodeGroups[" + name + "]"
}
