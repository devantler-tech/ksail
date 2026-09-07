package eks

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awseks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

var errNodegroupInventory = errors.New("EKS managed node-group inventory is incomplete")

// ListManagedNodegroups reads every page and describes every group using the
// client's frozen AWS configuration. A partial response never authorizes absence.
// eksctl's bulk summary currently omits EKS pagination, so creation must not use
// that summary alone to decide which managed groups exist.
func (c *Client) ListManagedNodegroups(
	ctx context.Context,
	clusterName string,
) ([]ekstypes.Nodegroup, error) {
	if c.nodegroups == nil || clusterName == "" {
		return nil, errNodegroupInventory
	}

	groups := make([]ekstypes.Nodegroup, 0)
	seenNames := make(map[string]bool)
	seenTokens := make(map[string]bool)

	input := &awseks.ListNodegroupsInput{ClusterName: aws.String(clusterName)}
	for {
		page, err := c.nodegroups.ListNodegroups(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("list EKS managed nodegroups: %w", err)
		}

		if page == nil || page.Nodegroups == nil {
			return nil, errNodegroupInventory
		}

		described, err := c.describeManagedPage(ctx, clusterName, page.Nodegroups, seenNames)
		if err != nil {
			return nil, err
		}

		groups = append(groups, described...)

		token := aws.ToString(page.NextToken)
		if token == "" {
			return groups, nil
		}

		if seenTokens[token] {
			return nil, fmt.Errorf("%w: repeated pagination token", errNodegroupInventory)
		}

		seenTokens[token] = true
		input.NextToken = page.NextToken
	}
}

func (c *Client) describeManagedNodegroup(
	ctx context.Context,
	clusterName, name string,
) (*ekstypes.Nodegroup, error) {
	output, err := c.nodegroups.DescribeNodegroup(ctx, &awseks.DescribeNodegroupInput{
		ClusterName: aws.String(clusterName), NodegroupName: aws.String(name),
	})
	if err != nil {
		return nil, fmt.Errorf("describe EKS managed nodegroup %s: %w", name, err)
	}

	if output == nil || output.Nodegroup == nil {
		return nil, errNodegroupInventory
	}

	group := output.Nodegroup
	if aws.ToString(group.ClusterName) != clusterName || aws.ToString(group.NodegroupName) != name {
		return nil, fmt.Errorf("%w: node-group identity mismatch", errNodegroupInventory)
	}

	return group, nil
}

func (c *Client) describeManagedPage(
	ctx context.Context, clusterName string, names []string, seen map[string]bool,
) ([]ekstypes.Nodegroup, error) {
	groups := make([]ekstypes.Nodegroup, 0, len(names))
	for _, name := range names {
		if name == "" || seen[name] {
			return nil, fmt.Errorf("%w: duplicate or empty group name", errNodegroupInventory)
		}

		seen[name] = true

		group, err := c.describeManagedNodegroup(ctx, clusterName, name)
		if err != nil {
			return nil, err
		}

		groups = append(groups, *group)
	}

	return groups, nil
}
