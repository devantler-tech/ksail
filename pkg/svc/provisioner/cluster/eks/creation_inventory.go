package eksprovisioner

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
)

const nodegroupCreationTag = "ksail.io/nodegroup-creation-id"

type managedNodegroupReader interface {
	ListManagedNodegroups(ctx context.Context, name string) ([]ekstypes.Nodegroup, error)
}

type creationInventory struct {
	groups      map[string]eksctl.NodegroupSummary
	creationIDs map[string]string
}

// listCreationNodegroups uses paginated SDK results for managed groups. eksctl
// supplies additional names to cross-check. Its summary can be incomplete, so
// absence also requires a direct CloudFormation stack check before creation.
func (u *UpdatableProvisioner) listCreationNodegroups(
	ctx context.Context, name string, desired []managedNodeGroupConfig,
) (*creationInventory, error) {
	clusterName := u.resolveName(name)

	live, err := u.client.ListNodegroups(ctx, clusterName, u.region)
	if err != nil {
		return nil, fmt.Errorf("list nodegroups: %w", err)
	}

	listed, err := indexCreationSummary(live, clusterName)
	if err != nil {
		return nil, err
	}

	inventory, err := u.readManagedInventory(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	for groupName, group := range listed {
		_, managed := inventory.groups[groupName]
		if managed != strings.EqualFold(group.NodeGroupType, "managed") {
			return nil, fmt.Errorf(
				"%w: inventory disagrees about %s",
				errUnknownNodegroupState,
				groupName,
			)
		}
	}

	for _, group := range desired {
		if entry, exists := listed[group.Name]; exists &&
			strings.EqualFold(entry.NodeGroupType, "unmanaged") {
			return nil, fmt.Errorf(
				"%w: %s already exists as an unmanaged group",
				errUnknownNodegroupState,
				group.Name,
			)
		}
	}

	return inventory, nil
}

func (u *UpdatableProvisioner) verifyCreationStackAbsent(
	ctx context.Context,
	name, groupName string,
) error {
	api, err := u.resolveAWSClient(ctx)
	if err != nil {
		return err
	}

	reader, supported := api.(interface {
		NodegroupStackExists(ctx context.Context, clusterName, groupName string) (bool, error)
	})
	if !supported {
		return errUnknownNodegroupState
	}

	exists, err := reader.NodegroupStackExists(ctx, u.resolveName(name), groupName)
	if err != nil {
		return fmt.Errorf("verify node-group stack absence: %w", err)
	}

	if exists {
		return fmt.Errorf("%w: a stack already exists for %s", errNodegroupPlanChanged, groupName)
	}

	return nil
}

func indexCreationSummary(
	live []eksctl.NodegroupSummary,
	clusterName string,
) (map[string]eksctl.NodegroupSummary, error) {
	if live == nil {
		return nil, fmt.Errorf(
			"%w: expected a JSON array, received empty or null output",
			errUnknownNodegroupState,
		)
	}

	seen := make(map[string]eksctl.NodegroupSummary, len(live))
	for _, group := range live {
		_, duplicate := seen[group.Name]
		if group.Name == "" || duplicate || group.Cluster != clusterName {
			return nil, fmt.Errorf(
				"%w: invalid, duplicate or wrong-cluster entry %q",
				errUnknownNodegroupState,
				group.Name,
			)
		}

		if !strings.EqualFold(group.NodeGroupType, "managed") &&
			!strings.EqualFold(group.NodeGroupType, "unmanaged") {
			return nil, fmt.Errorf(
				"%w: unknown group type for %s",
				errUnknownNodegroupState,
				group.Name,
			)
		}

		seen[group.Name] = group
	}

	return seen, nil
}

func (u *UpdatableProvisioner) readManagedInventory(
	ctx context.Context,
	clusterName string,
) (*creationInventory, error) {
	api, err := u.resolveAWSClient(ctx)
	if err != nil {
		return nil, err
	}

	reader, supported := api.(managedNodegroupReader)
	if !supported {
		return nil, errUnknownNodegroupState
	}

	groups, err := reader.ListManagedNodegroups(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("read complete managed node-group inventory: %w", err)
	}

	if groups == nil {
		return nil, errUnknownNodegroupState
	}

	inventory := &creationInventory{
		groups:      make(map[string]eksctl.NodegroupSummary, len(groups)),
		creationIDs: make(map[string]string, len(groups)),
	}
	for _, group := range groups {
		summary, err := managedCreationSummary(group, clusterName)
		if err != nil {
			return nil, err
		}

		if _, duplicate := inventory.groups[summary.Name]; duplicate {
			return nil, errUnknownNodegroupState
		}

		inventory.groups[summary.Name] = summary
		inventory.creationIDs[summary.Name] = group.Tags[nodegroupCreationTag]
	}

	return inventory, nil
}

func managedCreationSummary(
	group ekstypes.Nodegroup,
	clusterName string,
) (eksctl.NodegroupSummary, error) {
	name := aws.ToString(group.NodegroupName)
	if name == "" || aws.ToString(group.ClusterName) != clusterName ||
		group.Status != ekstypes.NodegroupStatusActive {
		return eksctl.NodegroupSummary{}, fmt.Errorf(
			"%w: %s has status %q or wrong identity",
			errUnknownNodegroupState,
			name,
			group.Status,
		)
	}

	scaling := group.ScalingConfig
	if scaling == nil || scaling.DesiredSize == nil || scaling.MinSize == nil ||
		scaling.MaxSize == nil {
		return eksctl.NodegroupSummary{}, fmt.Errorf(
			"%w: %s has incomplete scaling settings",
			errUnknownNodegroupState,
			name,
		)
	}

	return eksctl.NodegroupSummary{
		Cluster:       clusterName,
		Name:          name,
		NodeGroupType: "managed",
		Status:        string(group.Status),
		DesiredCap: int(
			*scaling.DesiredSize,
		),
		MinSize:      int(*scaling.MinSize),
		MaxSize:      int(*scaling.MaxSize),
		InstanceType: strings.Join(group.InstanceTypes, ","),
	}, nil
}
