//go:build unix

package eksinventory_test

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/maintenance/eksinventory"
)

const secondAutoScalingGroupARN = "arn:aws:autoscaling:eu-central-1:123456789012:" +
	"autoScalingGroup:def:autoScalingGroupName/eks-workers-b"

// twoGroupOwnershipQuery answers like ownershipQuery, except that the node group holds a second,
// empty Auto Scaling group.
func twoGroupOwnershipQuery(t *testing.T) eksinventory.Query {
	t.Helper()

	base := ownershipQuery(t)

	return func(ctx context.Context, args ...string) ([]byte, error) {
		switch args[0] + " " + args[1] {
		case "eks describe-nodegroup":
			return []byte(`{"nodegroup":{"nodegroupArn":"arn:aws:eks:eu-central-1:123456789012:` +
				`nodegroup/regional-diagnostic/workers/abc","resources":{"autoScalingGroups":` +
				`[{"name":"eks-workers"},{"name":"eks-workers-b"}]}}}`), nil
		case "autoscaling describe-auto-scaling-groups":
			if args[8] == `{"AutoScalingGroupNames":["eks-workers-b"]}` {
				return []byte(`{"AutoScalingGroups":[{"AutoScalingGroupARN":"` +
					secondAutoScalingGroupARN +
					`","AutoScalingGroupName":"eks-workers-b","Instances":[]}]}`), nil
			}
		}

		return base(ctx, args...)
	}
}

// TestOwnershipCaptureRecordsEveryAutoScalingGroupOfANodegroup pins that a successful capture of
// the first Auto Scaling group does not end the node group walk before the second one.
func TestOwnershipCaptureRecordsEveryAutoScalingGroupOfANodegroup(t *testing.T) {
	t.Parallel()

	dir := privateBaselineDir(t)
	baseline := captureBaselineFixture(t, dir)
	manifest := filepath.Join(dir, "ownership.json")

	err := eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest,
		eksinventory.OwnershipHints{WorkloadReadComplete: true}, io.Discard,
		twoGroupOwnershipQuery(t),
	)
	if err != nil {
		t.Fatal(err)
	}

	var got ownershipSnapshot

	err = json.Unmarshal(readPrivateTestFile(t, manifest), &got)
	if err != nil {
		t.Fatal(err)
	}

	for _, resource := range got.Resources {
		if resource.Kind == "autoscaling-group" && resource.Identity == secondAutoScalingGroupARN {
			return
		}
	}

	t.Fatalf("second Auto Scaling group missing: %#v", got.Resources)
}
