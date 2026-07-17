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
}

// NewUpdatableProvisioner wraps an EKS provisioner with in-place update support.
func NewUpdatableProvisioner(provisioner *Provisioner) *UpdatableProvisioner {
	return &UpdatableProvisioner{Provisioner: provisioner}
}

// managedNodeGroupConfig is the subset of an eksctl.yaml managedNodeGroups
// entry the updater diffs. Pointer fields distinguish "not declared" (nil,
// dimension is skipped) from an explicit zero.
type managedNodeGroupConfig struct {
	Name            string `json:"name"`
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

	desired, err := u.desiredNodegroups()
	if err != nil {
		return result, err
	}

	if len(desired) == 0 {
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

// GetCurrentConfig retrieves the current cluster configuration, merging
// persisted non-introspectable state so configured values do not read as
// false diffs on every update.
func (u *UpdatableProvisioner) GetCurrentConfig(
	_ context.Context,
	clusterName string,
) (*v1alpha1.ClusterSpec, *v1alpha1.ProviderSpec, error) {
	spec := clusterupdate.DefaultCurrentSpec(
		v1alpha1.DistributionEKS,
		v1alpha1.ProviderAWS,
	)

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

	desired, err := u.desiredNodegroups()
	if err != nil {
		return err
	}

	if len(desired) == 0 {
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
// live group toward the declared sizes. `--nodes` is always required, so an
// undeclared desiredCapacity falls back to the live value; min/max are only
// passed when declared and different (negative skips the flag).
func (u *UpdatableProvisioner) scaleNodegroup(
	ctx context.Context,
	clusterName string,
	group managedNodeGroupConfig,
	live eksctl.NodegroupSummary,
) error {
	desiredCapacity := live.DesiredCap
	if group.DesiredCapacity != nil {
		desiredCapacity = *group.DesiredCapacity
	}

	minSize := -1
	if group.MinSize != nil && *group.MinSize != live.MinSize {
		minSize = *group.MinSize
	}

	maxSize := -1
	if group.MaxSize != nil && *group.MaxSize != live.MaxSize {
		maxSize = *group.MaxSize
	}

	//nolint:wrapcheck // wrapped by the caller with the nodegroup name.
	return u.client.ScaleNodegroup(
		ctx, clusterName, group.Name, u.region, desiredCapacity, minSize, maxSize,
	)
}

// desiredNodegroups parses the managedNodeGroups block of the declarative
// eksctl.yaml. A missing file or an empty block yields no groups (nothing
// to diff) rather than an error.
func (u *UpdatableProvisioner) desiredNodegroups() ([]managedNodeGroupConfig, error) {
	if strings.TrimSpace(u.configPath) == "" {
		return nil, nil
	}

	_, err := os.Stat(u.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to stat EKS config file: %w", err)
	}

	canonical, err := fsutil.EvalCanonicalPath(u.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to canonicalize EKS config path: %w", err)
	}

	data, err := fsutil.ReadFileSafe(filepath.Dir(canonical), canonical)
	if err != nil {
		return nil, fmt.Errorf("failed to read EKS config file: %w", err)
	}

	var config struct {
		ManagedNodeGroups []managedNodeGroupConfig `json:"managedNodeGroups"`
	}

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse EKS config file: %w", err)
	}

	return config.ManagedNodeGroups, nil
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
