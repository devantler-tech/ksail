package eks

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
)

// NodegroupStackExists checks every CloudFormation summary page for the exact
// eksctl stack name. This catches unmanaged groups and unfinished creates even
// when eksctl's summary silently omits a failed DescribeStack result.
func (c *Client) NodegroupStackExists(
	ctx context.Context,
	clusterName, groupName string,
) (bool, error) {
	if c.nodegroupStacks == nil || clusterName == "" || groupName == "" {
		return false, errNodegroupInventory
	}

	target := "eksctl-" + clusterName + "-nodegroup-" + groupName

	return c.stackExists(ctx, target)
}

func (c *Client) stackExists(ctx context.Context, target string) (bool, error) {
	input := &cloudformation.ListStacksInput{}
	seen := make(map[string]bool)

	for {
		page, err := c.nodegroupStacks.ListStacks(ctx, input)
		if err != nil {
			return false, fmt.Errorf("list EKS node-group stacks: %w", err)
		}

		if page == nil || page.StackSummaries == nil {
			return false, errNodegroupInventory
		}

		found, err := nodegroupStackPageContains(page.StackSummaries, target)
		if err != nil || found {
			return found, err
		}

		token := aws.ToString(page.NextToken)
		if token == "" {
			return false, nil
		}

		if seen[token] {
			return false, fmt.Errorf("%w: repeated stack pagination token", errNodegroupInventory)
		}

		seen[token] = true
		input.NextToken = page.NextToken
	}
}

func nodegroupStackPageContains(stacks []cftypes.StackSummary, target string) (bool, error) {
	for _, stack := range stacks {
		if aws.ToString(stack.StackName) == "" || stack.StackStatus == "" {
			return false, errNodegroupInventory
		}

		if aws.ToString(stack.StackName) == target &&
			stack.StackStatus != cftypes.StackStatusDeleteComplete {
			return true, nil
		}
	}

	return false, nil
}
