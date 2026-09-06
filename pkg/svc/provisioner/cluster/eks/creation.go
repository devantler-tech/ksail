package eksprovisioner

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"sigs.k8s.io/yaml"
)

var (
	errInvalidNodegroupConfig = errors.New("invalid managed node-group creation config")
	errUnknownNodegroupState  = errors.New("managed node-group state is not authoritative")
	errNodegroupPlanChanged   = errors.New(
		"EKS node-group update plan changed; run cluster update again",
	)
	errNodegroupIdentityRequired = errors.New(
		"managed node-group creation requires a verified cluster name and region",
	)
	errNodegroupNotConverged = errors.New(
		"created managed node group is not ACTIVE with the declared settings",
	)
	managedNodegroupName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,62}$`)
)

// nodegroupCreationPlan holds the exact source read for this Update invocation.
// Full group objects preserve advanced eksctl settings; the typed subset drives
// the diff. No later read can silently replace the declaration being applied.
type nodegroupCreationPlan struct {
	path        string
	source      []byte
	config      map[string]any
	desired     []managedNodeGroupConfig
	groups      map[string]map[string]any
	additions   map[string]bool
	operationID string
}

func (u *UpdatableProvisioner) updateWithNodegroupCreation(
	ctx context.Context,
	name string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	opts clusterupdate.UpdateOptions,
) (*clusterupdate.UpdateResult, error) {
	var plan *nodegroupCreationPlan

	diff := func(ctx context.Context, name string, _, _ *v1alpha1.ClusterSpec) (*clusterupdate.UpdateResult, error) {
		var (
			result *clusterupdate.UpdateResult
			err    error
		)

		plan, result, err = u.planNodegroupCreation(ctx, name)

		return result, err
	}
	apply := func(ctx context.Context, name string, result *clusterupdate.UpdateResult) error {
		if plan == nil || !result.HasInPlaceChanges() {
			return nil
		}

		return u.applyNodegroupPlan(ctx, name, plan, result)
	}

	//nolint:wrapcheck // RunUpdate adds the operation context.
	return clusterupdate.RunUpdate(ctx, name, oldSpec, newSpec, opts,
		clustererr.ErrRecreationRequired, diff, apply, "failed to update managed nodegroups")
}

func (u *UpdatableProvisioner) planNodegroupCreation(
	ctx context.Context, name string,
) (*nodegroupCreationPlan, *clusterupdate.UpdateResult, error) {
	result := clusterupdate.NewEmptyUpdateResult()
	if strings.TrimSpace(u.configPath) == "" {
		return nil, result, nil
	}

	plan, err := readNodegroupCreationPlan(u.configPath)
	if err != nil {
		return nil, result, err
	}

	live, err := u.listCreationNodegroups(ctx, name, plan.desired)
	if err != nil {
		return nil, result, err
	}

	for _, group := range plan.desired {
		_, exists := live.groups[group.Name]
		plan.additions[group.Name] = !exists
	}

	return plan, diffManagedNodegroups(plan.desired, live.groups, true), nil
}

func readNodegroupCreationPlan(path string) (*nodegroupCreationPlan, error) {
	canonical, err := fsutil.EvalCanonicalPath(path)
	if err != nil {
		return nil, fmt.Errorf("canonicalize EKS node-group config: %w", err)
	}

	data, err := fsutil.ReadFileSafe(filepath.Dir(canonical), canonical)
	if err != nil {
		return nil, fmt.Errorf("read EKS node-group config: %w", err)
	}

	plan := &nodegroupCreationPlan{
		path: canonical, source: data, groups: make(map[string]map[string]any),
		additions: make(map[string]bool), operationID: rand.Text(),
	}

	err = plan.parse()
	if err != nil {
		return nil, err
	}

	return plan, nil
}

func (p *nodegroupCreationPlan) parse() error {
	err := yaml.UnmarshalStrict(p.source, &p.config)
	if err != nil {
		return fmt.Errorf("parse EKS node-group config: %w", err)
	}

	_, metadataOK := p.config["metadata"].(map[string]any)
	if !metadataOK || p.config["apiVersion"] != "eksctl.io/v1alpha5" ||
		p.config["kind"] != "ClusterConfig" {
		return errInvalidNodegroupConfig
	}

	var typed struct {
		ManagedNodeGroups []managedNodeGroupConfig `json:"managedNodeGroups"`
	}

	err = yaml.Unmarshal(p.source, &typed)
	if err != nil {
		return fmt.Errorf("parse managed node-group settings: %w", err)
	}

	p.desired = typed.ManagedNodeGroups

	return p.indexDesiredGroups()
}

func (p *nodegroupCreationPlan) indexDesiredGroups() error {
	rawGroups, _ := p.config["managedNodeGroups"].([]any)
	if len(rawGroups) != len(p.desired) {
		return errInvalidNodegroupConfig
	}

	for index, group := range p.desired {
		if !managedNodegroupName.MatchString(group.Name) || p.groups[group.Name] != nil {
			return fmt.Errorf(
				"%w: invalid or duplicate name %q",
				errInvalidNodegroupConfig,
				group.Name,
			)
		}

		rawGroup, valid := rawGroups[index].(map[string]any)
		if !valid || rawGroup["name"] != group.Name {
			return errInvalidNodegroupConfig
		}

		if tags, declared := rawGroup["tags"]; declared {
			if _, valid := tags.(map[string]any); !valid {
				return errInvalidNodegroupConfig
			}
		}

		p.groups[group.Name] = rawGroup
	}

	return nil
}

func (u *UpdatableProvisioner) applyNodegroupPlan(
	ctx context.Context,
	name string,
	plan *nodegroupCreationPlan,
	result *clusterupdate.UpdateResult,
) error {
	for _, group := range plan.desired {
		changes, err := u.refreshAndApplyNodegroup(ctx, name, plan, group)
		if err != nil {
			result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
				Field: nodegroupField(group.Name), Reason: err.Error(),
			})

			return fmt.Errorf("update nodegroup %s: %w", group.Name, err)
		}

		result.AppliedChanges = append(result.AppliedChanges, changes...)
	}

	return nil
}

func (u *UpdatableProvisioner) refreshAndApplyNodegroup(
	ctx context.Context, name string, plan *nodegroupCreationPlan, group managedNodeGroupConfig,
) ([]clusterupdate.Change, error) {
	live, err := u.listCreationNodegroups(ctx, name, plan.desired)
	if err != nil {
		return nil, err
	}

	if diffManagedNodegroups(plan.desired, maps.Clone(live.groups), true).HasRecreateRequired() {
		return nil, errNodegroupPlanChanged
	}

	for _, desired := range plan.desired {
		if _, exists := live.groups[desired.Name]; !exists && !plan.additions[desired.Name] {
			return nil, fmt.Errorf("%w: %s disappeared", errNodegroupPlanChanged, desired.Name)
		}
	}

	return u.applyPlannedNodegroup(ctx, name, plan, group, live)
}

func (u *UpdatableProvisioner) applyPlannedNodegroup(
	ctx context.Context, name string, plan *nodegroupCreationPlan,
	group managedNodeGroupConfig, live *creationInventory,
) ([]clusterupdate.Change, error) {
	liveGroup, exists := live.groups[group.Name]
	if !exists {
		err := u.createPlannedNodegroup(ctx, name, plan, group)
		if err != nil {
			return nil, err
		}

		return []clusterupdate.Change{nodegroupCreationChange(group.Name)}, nil
	}

	if plan.additions[group.Name] {
		return nil, fmt.Errorf(
			"%w: %s appeared after planning",
			errNodegroupPlanChanged,
			group.Name,
		)
	}

	changes := scalingChanges(group, liveGroup)
	if len(changes) == 0 {
		return nil, nil
	}

	err := u.verifyNodegroupPlan(ctx, name, plan)
	if err != nil {
		return nil, err
	}

	desired, minimum, maximum := nodegroupScaleTargets(group, liveGroup)

	err = u.client.ScaleNodegroup(ctx, u.name, group.Name, u.region, desired, minimum, maximum)
	if err != nil {
		return nil, fmt.Errorf("scale managed nodegroup: %w", err)
	}

	return changes, nil
}

func (u *UpdatableProvisioner) createPlannedNodegroup(
	ctx context.Context, name string, plan *nodegroupCreationPlan, group managedNodeGroupConfig,
) error {
	config := maps.Clone(plan.config)
	metadata, _ := config["metadata"].(map[string]any)
	metadata = maps.Clone(metadata)
	metadata["name"], metadata["region"] = u.name, u.region
	config["metadata"] = metadata
	rawGroup := plan.groups[group.Name]
	effectiveGroup := maps.Clone(rawGroup)
	tags, _ := rawGroup["tags"].(map[string]any)
	effectiveTags := make(map[string]any, len(tags)+1)
	maps.Copy(effectiveTags, tags)
	effectiveTags[nodegroupCreationTag] = plan.operationID
	effectiveGroup["tags"] = effectiveTags
	config["managedNodeGroups"] = []any{effectiveGroup}
	delete(config, "nodeGroups")

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal managed node-group creation config: %w", err)
	}

	path, cleanup, err := writeEffectiveCreateConfig(data)
	if err != nil {
		return err
	}
	defer cleanup()

	err = u.verifyCreationStackAbsent(ctx, name, group.Name)
	if err != nil {
		return err
	}

	// The private snapshot is complete before the identity check. The same
	// credential-bound client used for the read executes this exact file.
	err = u.verifyNodegroupPlan(ctx, name, plan)
	if err != nil {
		return err
	}

	err = u.client.CreateNodegroup(ctx, path)
	if err != nil {
		return fmt.Errorf("create managed nodegroup: %w", err)
	}

	live, err := u.listCreationNodegroups(ctx, name, plan.desired)
	if err != nil {
		return err
	}

	created, exists := live.groups[group.Name]
	if !exists || !nodegroupConverged(group, created) ||
		live.creationIDs[group.Name] != plan.operationID {
		return fmt.Errorf(
			"%w: %s; inspect EKS before retrying",
			errNodegroupNotConverged,
			group.Name,
		)
	}

	return nil
}

func (u *UpdatableProvisioner) verifyNodegroupPlan(
	ctx context.Context, name string, plan *nodegroupCreationPlan,
) error {
	if u.ownershipVerifier == nil || u.name == "" || u.region == "" ||
		u.resolveName(name) != u.name {
		return errNodegroupIdentityRequired
	}

	err := eksidentity.VerifyBeforeMutation(ctx, u.ownershipVerifier)
	if err != nil {
		return fmt.Errorf("verify EKS ownership before node-group update: %w", err)
	}

	return plan.verifySource(ctx, u.configPath)
}

func (p *nodegroupCreationPlan) verifySource(ctx context.Context, sourcePath string) error {
	canonical, err := fsutil.EvalCanonicalPath(sourcePath)
	if err != nil {
		return fmt.Errorf("recheck EKS config path: %w", err)
	}

	data, err := fsutil.ReadFileSafe(filepath.Dir(canonical), canonical)
	if err != nil {
		return fmt.Errorf("recheck EKS config: %w", err)
	}

	if canonical != p.path || !bytes.Equal(data, p.source) {
		return errNodegroupPlanChanged
	}

	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("node-group update canceled: %w", err)
	}

	return nil
}

func nodegroupConverged(group managedNodeGroupConfig, live eksctl.NodegroupSummary) bool {
	return live.Status == "ACTIVE" && len(scalingChanges(group, live)) == 0 &&
		(group.InstanceType == "" || group.InstanceType == live.InstanceType)
}

func nodegroupCreationChange(name string) clusterupdate.Change {
	return clusterupdate.Change{
		Field: nodegroupField(name), NewValue: name, Category: clusterupdate.ChangeCategoryInPlace,
		Reason: "experimental managed node-group creation is enabled",
	}
}
