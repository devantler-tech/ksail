//go:build unix

package eksinventory_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/maintenance/eksinventory"
)

type ownershipSnapshot struct {
	SchemaVersion        int                          `json:"schemaVersion"`
	CaptureCount         int                          `json:"captureCount"`
	CompleteCaptureCount int                          `json:"completeCaptureCount"`
	AccountID            string                       `json:"accountId"`
	Context              eksinventory.BaselineContext `json:"context"`
	Cluster              string                       `json:"clusterIdentity"`
	Resources            []ownedResource              `json:"resources"`
	Coverage             map[string]ownershipCoverage `json:"coverage"`
}

type ownedResource struct {
	Kind     string `json:"kind"`
	Identity string `json:"identity"`
	Parent   string `json:"parent"`
}

type ownershipCoverage struct {
	ReadComplete      bool   `json:"readComplete"`
	OwnershipCoverage string `json:"ownershipCoverage"`
	Reason            string `json:"reason,omitempty"`
	LastRead          string `json:"lastRead,omitempty"`
}

var errTestClusterNotFound = errors.New("cluster not found")

// The node group stack as eksctl names it and as CloudFormation lists it. The role that runs the
// capture holds CloudFormation access on eksctl stack ARNs only, so the stack is read by its name.
const (
	nodegroupStackName = "eksctl-regional-diagnostic-nodegroup-workers"
	nodegroupStackID   = "arn:aws:cloudformation:eu-central-1:123456789012:stack/" +
		nodegroupStackName + "/0f6c5e40-8b5d-11f1-9a3e-06d1c2b3a4f5"
	//nolint:lll // One literal provider response keeps the real field set visible.
	nodegroupStackResources = `{"StackResourceSummaries":[` +
		`{"LogicalResourceId":"LaunchTemplate","PhysicalResourceId":"lt-0123456789abcdef0","ResourceType":"AWS::EC2::LaunchTemplate","LastUpdatedTimestamp":"2026-10-16T20:12:40.512Z","ResourceStatus":"CREATE_COMPLETE","DriftInformation":{"StackResourceDriftStatus":"NOT_CHECKED"}},` +
		`{"LogicalResourceId":"ManagedNodeGroup","PhysicalResourceId":"regional-diagnostic/workers","ResourceType":"AWS::EKS::Nodegroup","LastUpdatedTimestamp":"2026-10-16T20:27:29.044Z","ResourceStatus":"UPDATE_COMPLETE","DriftInformation":{"StackResourceDriftStatus":"NOT_CHECKED"}},` +
		`{"LogicalResourceId":"NodeInstanceRole","PhysicalResourceId":"eksctl-regional-diagnostic-nodegrou-NodeInstanceRole-AbCdEfGh1234","ResourceType":"AWS::IAM::Role","LastUpdatedTimestamp":"2026-10-16T20:11:58.913Z","ResourceStatus":"CREATE_COMPLETE","DriftInformation":{"StackResourceDriftStatus":"NOT_CHECKED"}}` +
		`]}`
)

func captureBaselineFixture(t *testing.T, dir string) string {
	t.Helper()

	path := filepath.Join(dir, "baseline.json")

	err := eksinventory.CaptureBaseline(
		context.Background(), baselineBinding(), path, io.Discard, baselineQuery(t, nil),
	)
	if err != nil {
		t.Fatal(err)
	}

	return path
}

//nolint:cyclop,funlen,gocognit,gocyclo,lll // One complete provider fixture keeps cross-family relationships coherent.
func ownershipQuery(t *testing.T) eksinventory.Query {
	t.Helper()

	return func(_ context.Context, args ...string) ([]byte, error) {
		key := args[0] + " " + args[1]

		var input map[string]any

		err := json.Unmarshal([]byte(args[8]), &input)
		if err != nil {
			t.Fatal(err)
		}

		switch key {
		case "sts get-caller-identity":
			return []byte(
				`{"Account":"123456789012","Arn":"arn:aws:sts::123456789012:assumed-role/eks-ci/run"}`,
			), nil
		case "eks describe-cluster":
			return []byte(
				`{"cluster":{"arn":"arn:aws:eks:eu-central-1:123456789012:cluster/regional-diagnostic","createdAt":"2024-03-14T15:31:44.348Z"}}`,
			), nil
		case "cloudformation describe-stacks":
			if input["StackName"] == nodegroupStackName {
				return []byte(`{"Stacks":[{"StackId":"` + nodegroupStackID + `","StackName":"` +
					nodegroupStackName + `","StackStatus":"UPDATE_COMPLETE"}]}`), nil
			}

			return []byte(
				`{"Stacks":[{"StackId":"arn:aws:cloudformation:eu-central-1:123456789012:stack/root/one"}]}`,
			), nil
		case "cloudformation list-stack-resources":
			if input["StackName"] == nodegroupStackID {
				return []byte(nodegroupStackResources), nil
			}

			stack := input["StackName"]
			if stack == "arn:aws:cloudformation:eu-central-1:123456789012:stack/root/one" {
				if input["NextToken"] == "root-page-2" {
					return []byte(
						`{"StackResourceSummaries":[{"LogicalResourceId":"Nat","PhysicalResourceId":"nat-0123456789abcdef0","ResourceType":"AWS::EC2::NatGateway","ResourceStatus":"CREATE_COMPLETE"},{"LogicalResourceId":"VPC","PhysicalResourceId":"vpc-0123456789abcdef0","ResourceType":"AWS::EC2::VPC","ResourceStatus":"CREATE_COMPLETE"}]}`,
					), nil
				}

				return []byte(
					`{"StackResourceSummaries":[{"LogicalResourceId":"Nested","PhysicalResourceId":"arn:aws:cloudformation:eu-central-1:123456789012:stack/nested/two","ResourceType":"AWS::CloudFormation::Stack","ResourceStatus":"CREATE_COMPLETE"}],"NextToken":"root-page-2"}`,
				), nil
			}

			if stack == "arn:aws:cloudformation:eu-central-1:123456789012:stack/nested/two" {
				return []byte(
					`{"StackResourceSummaries":[{"LogicalResourceId":"Fleet","PhysicalResourceId":"fleet-0123456789abcdef0","ResourceType":"AWS::EC2::EC2Fleet","ResourceStatus":"CREATE_FAILED"}]}`,
				), nil
			}

			return []byte(`{"StackResourceSummaries":[]}`), nil
		case "eks list-nodegroups":
			return []byte(`{"nodegroups":["workers"]}`), nil
		case "eks describe-nodegroup":
			return []byte(
				`{"nodegroup":{"nodegroupArn":"arn:aws:eks:eu-central-1:123456789012:nodegroup/regional-diagnostic/workers/abc","resources":{"autoScalingGroups":[{"name":"eks-workers"}]}}}`,
			), nil
		case "autoscaling describe-auto-scaling-groups":
			if input["MaxRecords"] != nil || input["NextToken"] != nil {
				t.Fatalf("exact autoscaling lookup used pagination input: %#v", input)
			}

			return []byte(
				`{"AutoScalingGroups":[{"AutoScalingGroupARN":"arn:aws:autoscaling:eu-central-1:123456789012:autoScalingGroup:abc:autoScalingGroupName/eks-workers","AutoScalingGroupName":"eks-workers","Instances":[{"InstanceId":"i-0123456789abcdef0"}]}]}`,
			), nil
		case "ec2 describe-instances":
			if input["MaxResults"] != nil || input["NextToken"] != nil {
				t.Fatalf("exact instance lookup used pagination input: %#v", input)
			}

			return []byte(
				`{"Reservations":[{"Instances":[{"InstanceId":"i-0123456789abcdef0","NetworkInterfaces":[{"NetworkInterfaceId":"eni-0123456789abcdef0"}],"BlockDeviceMappings":[{"Ebs":{"VolumeId":"vol-0123456789abcdef0"}}]}]}]}`,
			), nil
		case "ec2 describe-nat-gateways":
			if input["MaxResults"] != nil || input["NextToken"] != nil {
				t.Fatalf("exact NAT lookup used pagination input: %#v", input)
			}

			return []byte(
				`{"NatGateways":[{"NatGatewayId":"nat-0123456789abcdef0","NatGatewayAddresses":[{"AllocationId":"eipalloc-0123456789abcdef0","NetworkInterfaceId":"eni-11111111111111111"}]}]}`,
			), nil
		case "ec2 describe-fleets":
			if input["MaxResults"] != nil || input["NextToken"] != nil {
				t.Fatalf("exact fleet lookup used pagination input: %#v", input)
			}

			return []byte(
				`{"Fleets":[{"FleetId":"fleet-0123456789abcdef0","FleetState":"active","Type":"instant"}]}`,
			), nil
		case "ec2 describe-volumes":
			if input["MaxResults"] != nil || input["NextToken"] != nil {
				t.Fatalf("exact volume lookup used pagination input: %#v", input)
			}

			return []byte(
				`{"Volumes":[{"VolumeId":"vol-11111111111111111","State":"in-use"}]}`,
			), nil
		case "elb describe-load-balancers":
			if input["LoadBalancerNames"] != nil && input["PageSize"] != nil {
				t.Fatalf("exact classic load balancer lookup used pagination input: %#v", input)
			}

			return []byte(
				`{"LoadBalancerDescriptions":[{"LoadBalancerName":"classic-owned","DNSName":"classic-owned.eu-central-1.elb.amazonaws.com"}]}`,
			), nil
		case "elbv2 describe-load-balancers":
			if input["LoadBalancerArns"] != nil && input["PageSize"] != nil {
				t.Fatalf("exact ELBv2 lookup used pagination input: %#v", input)
			}

			return []byte(
				`{"LoadBalancers":[{"LoadBalancerArn":"arn:aws:elasticloadbalancing:eu-central-1:123456789012:loadbalancer/app/owned/abc","DNSName":"owned-123.eu-central-1.elb.amazonaws.com","State":{"Code":"active"}}]}`,
			), nil
		default:
			t.Fatalf("unexpected ownership read %s with %#v", key, input)

			return nil, nil
		}
	}
}

//nolint:cyclop,funlen,lll // The assertion enumerates the complete expected ownership graph literally.
func TestOwnershipCapturePreservesNestedAndDetachedRelationships(t *testing.T) {
	t.Parallel()
	dir := privateBaselineDir(t)
	baseline := captureBaselineFixture(t, dir)
	manifest := filepath.Join(dir, "ownership.json")
	hints := eksinventory.OwnershipHints{
		ClassicLoadBalancers: []string{"classic-owned"},
		LoadBalancerARNs: []string{
			"arn:aws:elasticloadbalancing:eu-central-1:123456789012:loadbalancer/app/owned/abc",
		},
		VolumeIDs: []string{"vol-11111111111111111"},
		LoadBalancerHosts: []string{
			"classic-owned.eu-central-1.elb.amazonaws.com",
			"owned-123.eu-central-1.elb.amazonaws.com",
		},
		WorkloadReadComplete: true,
	}

	var output bytes.Buffer

	err := eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest, hints, &output,
		ownershipQuery(t),
	)
	if err != nil {
		t.Fatal(err)
	}

	var got ownershipSnapshot

	err = json.Unmarshal(readPrivateTestFile(t, manifest), &got)
	if err != nil {
		t.Fatal(err)
	}

	if got.SchemaVersion != 3 || got.AccountID != "123456789012" ||
		got.Context != baselineBinding() || !strings.Contains(got.Cluster, ":cluster/regional-diagnostic|") {
		t.Fatalf("ownership binding incomplete: %#v", got)
	}

	want := []ownedResource{
		{
			Kind:     "address",
			Identity: "eipalloc-0123456789abcdef0",
			Parent:   "nat-gateway:nat-0123456789abcdef0",
		},
		{
			Kind:     "autoscaling-group",
			Identity: "arn:aws:autoscaling:eu-central-1:123456789012:autoScalingGroup:abc:autoScalingGroupName/eks-workers",
			Parent:   "nodegroup:arn:aws:eks:eu-central-1:123456789012:nodegroup/regional-diagnostic/workers/abc",
		},
		{
			Kind:     "classic-load-balancer",
			Identity: "classic-owned",
			Parent:   "cluster:arn:aws:eks:eu-central-1:123456789012:cluster/regional-diagnostic|2024-03-14T15:31:44.348Z",
		},
		{
			Kind:     "fleet",
			Identity: "fleet-0123456789abcdef0",
			Parent:   "stack:arn:aws:cloudformation:eu-central-1:123456789012:stack/nested/two",
		},
		{
			Kind:     "instance",
			Identity: "i-0123456789abcdef0",
			Parent:   "autoscaling-group:arn:aws:autoscaling:eu-central-1:123456789012:autoScalingGroup:abc:autoScalingGroupName/eks-workers",
		},
		{
			Kind:     "load-balancer",
			Identity: "arn:aws:elasticloadbalancing:eu-central-1:123456789012:loadbalancer/app/owned/abc",
			Parent:   "cluster:arn:aws:eks:eu-central-1:123456789012:cluster/regional-diagnostic|2024-03-14T15:31:44.348Z",
		},
		{
			Kind:     "nat-gateway",
			Identity: "nat-0123456789abcdef0",
			Parent:   "stack:arn:aws:cloudformation:eu-central-1:123456789012:stack/root/one",
		},
		{
			Kind:     "network-interface",
			Identity: "eni-0123456789abcdef0",
			Parent:   "instance:i-0123456789abcdef0",
		},
		{
			Kind:     "network-interface",
			Identity: "eni-11111111111111111",
			Parent:   "nat-gateway:nat-0123456789abcdef0",
		},
		{
			Kind:     "nodegroup",
			Identity: "arn:aws:eks:eu-central-1:123456789012:nodegroup/regional-diagnostic/workers/abc",
			Parent:   "cluster:arn:aws:eks:eu-central-1:123456789012:cluster/regional-diagnostic|2024-03-14T15:31:44.348Z",
		},
		{
			Kind:     "stack",
			Identity: nodegroupStackID,
			Parent:   "nodegroup:arn:aws:eks:eu-central-1:123456789012:nodegroup/regional-diagnostic/workers/abc",
		},
		{
			Kind:     "stack",
			Identity: "arn:aws:cloudformation:eu-central-1:123456789012:stack/nested/two",
			Parent:   "stack:arn:aws:cloudformation:eu-central-1:123456789012:stack/root/one",
		},
		{
			Kind:     "stack",
			Identity: "arn:aws:cloudformation:eu-central-1:123456789012:stack/root/one",
			Parent:   "cluster:arn:aws:eks:eu-central-1:123456789012:cluster/regional-diagnostic|2024-03-14T15:31:44.348Z",
		},
		{Kind: "volume", Identity: "vol-0123456789abcdef0", Parent: "instance:i-0123456789abcdef0"},
		{
			Kind:     "volume",
			Identity: "vol-11111111111111111",
			Parent:   "cluster:arn:aws:eks:eu-central-1:123456789012:cluster/regional-diagnostic|2024-03-14T15:31:44.348Z",
		},
		{
			Kind:     "vpc",
			Identity: "vpc-0123456789abcdef0",
			Parent:   "stack:arn:aws:cloudformation:eu-central-1:123456789012:stack/root/one",
		},
	}
	if !reflect.DeepEqual(got.Resources, want) {
		t.Fatalf("relationships differ:\n got: %#v\nwant: %#v", got.Resources, want)
	}

	if got.Coverage["instantFleets"].OwnershipCoverage != "exact-identities" ||
		!got.Coverage["cloudformation"].ReadComplete {
		t.Fatalf("coverage does not separate reads and ownership: %#v", got.Coverage)
	}

	if strings.Contains(output.String(), "123456789012") ||
		strings.Contains(output.String(), "vol-") ||
		!strings.Contains(output.String(), "ownership=exact-identities") {
		t.Fatalf("unsafe or incomplete public summary: %s", output.String())
	}
}

//nolint:forcetypeassert // The fixture is decoded from the known manifest written immediately above.
func TestOwnershipCaptureRejectsAChangedBaselineBeforeAWSReads(t *testing.T) {
	t.Parallel()
	dir := privateBaselineDir(t)
	baseline := captureBaselineFixture(t, dir)
	manifest := filepath.Join(dir, "ownership.json")

	err := eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest, eksinventory.OwnershipHints{},
		io.Discard, ownershipQuery(t),
	)
	if err != nil {
		t.Fatal(err)
	}

	var snapshot map[string]any

	err = json.Unmarshal(readPrivateTestFile(t, baseline), &snapshot)
	if err != nil {
		t.Fatal(err)
	}

	observations := snapshot["observations"].([]any)
	first := observations[0].(map[string]any)
	first["identities"] = []any{"injected-resource"}

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(baseline, data, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	query := func(context.Context, ...string) ([]byte, error) {
		t.Fatal("changed baseline caused AWS reads")

		return nil, nil
	}

	err = eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest, eksinventory.OwnershipHints{},
		io.Discard, query,
	)
	if err == nil {
		t.Fatal("changed baseline accepted for cumulative capture")
	}
}

//nolint:forcetypeassert // The fixture is decoded from the known manifest written immediately above.
func TestOwnershipCaptureRejectsAnUnknownBaselineFamilyBeforeAWSReads(t *testing.T) {
	t.Parallel()
	dir := privateBaselineDir(t)
	baseline := captureBaselineFixture(t, dir)

	var snapshot map[string]any

	err := json.Unmarshal(readPrivateTestFile(t, baseline), &snapshot)
	if err != nil {
		t.Fatal(err)
	}

	observations := snapshot["observations"].([]any)
	first := observations[0].(map[string]any)
	first["service"] = "unapproved"
	first["operation"] = "read"

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(baseline, data, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	query := func(context.Context, ...string) ([]byte, error) {
		t.Fatal("unknown baseline family caused AWS reads")

		return nil, nil
	}

	err = eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, filepath.Join(dir, "ownership.json"),
		eksinventory.OwnershipHints{}, io.Discard, query,
	)
	if err == nil {
		t.Fatal("unknown baseline family accepted")
	}
}

func TestOwnershipCaptureRejectsAnAlteredRelationshipBeforeAWSReads(t *testing.T) {
	t.Parallel()
	dir := privateBaselineDir(t)
	baseline := captureBaselineFixture(t, dir)

	manifest := filepath.Join(dir, "ownership.json")

	err := eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest, eksinventory.OwnershipHints{},
		io.Discard, ownershipQuery(t),
	)
	if err != nil {
		t.Fatal(err)
	}

	var snapshot map[string]any

	err = json.Unmarshal(readPrivateTestFile(t, manifest), &snapshot)
	if err != nil {
		t.Fatal(err)
	}

	resources, resourcesValid := snapshot["resources"].([]any)
	if !resourcesValid || len(resources) == 0 {
		t.Fatal("ownership resources missing")
	}

	resource, resourceValid := resources[0].(map[string]any)
	if !resourceValid {
		t.Fatal("ownership resource is not an object")
	}

	resource["parent"] = "cluster:altered"

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(manifest, data, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	query := func(context.Context, ...string) ([]byte, error) {
		t.Fatal("altered relationship caused AWS reads")

		return nil, nil
	}

	err = eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest, eksinventory.OwnershipHints{},
		io.Discard, query,
	)
	if err == nil {
		t.Fatal("altered relationship accepted")
	}
}

func TestOwnershipCaptureRejectsUnknownEvidenceFieldsBeforeAWSReads(t *testing.T) {
	t.Parallel()
	dir := privateBaselineDir(t)
	baseline := captureBaselineFixture(t, dir)
	manifest := filepath.Join(dir, "ownership.json")

	err := eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest, eksinventory.OwnershipHints{},
		io.Discard, ownershipQuery(t),
	)
	if err != nil {
		t.Fatal(err)
	}

	var snapshot map[string]any

	err = json.Unmarshal(readPrivateTestFile(t, manifest), &snapshot)
	if err != nil {
		t.Fatal(err)
	}

	snapshot["unreviewedField"] = true

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(manifest, data, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	query := func(context.Context, ...string) ([]byte, error) {
		t.Fatal("unknown evidence field caused AWS reads")

		return nil, nil
	}

	err = eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest, eksinventory.OwnershipHints{},
		io.Discard, query,
	)
	if err == nil {
		t.Fatal("unknown evidence field accepted")
	}
}

//nolint:cyclop,funlen,lll // The two captures deliberately exercise distinct provider relationship graphs.
func TestOwnershipCaptureUnionsRelationshipsAcrossScaleDown(t *testing.T) {
	t.Parallel()
	dir := privateBaselineDir(t)
	baseline := captureBaselineFixture(t, dir)
	manifest := filepath.Join(dir, "ownership.json")

	first := ownershipQuery(t)

	err := eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest, eksinventory.OwnershipHints{},
		io.Discard, first,
	)
	if err != nil {
		t.Fatal(err)
	}

	second := func(ctx context.Context, args ...string) ([]byte, error) {
		switch args[0] + " " + args[1] {
		case "eks describe-cluster":
			return []byte(
				`{"cluster":{"arn":"arn:aws:eks:eu-central-1:123456789012:cluster/regional-diagnostic","createdAt":1710430304.348}}`,
			), nil
		case "autoscaling describe-auto-scaling-groups":
			return []byte(
				`{"AutoScalingGroups":[{"AutoScalingGroupARN":"arn:aws:autoscaling:eu-central-1:123456789012:autoScalingGroup:abc:autoScalingGroupName/eks-workers","AutoScalingGroupName":"eks-workers","Instances":[{"InstanceId":"i-11111111111111111"}]}]}`,
			), nil
		case "ec2 describe-instances":
			return []byte(
				`{"Reservations":[{"Instances":[{"InstanceId":"i-11111111111111111","NetworkInterfaces":[],"BlockDeviceMappings":[]}]}]}`,
			), nil
		default:
			return first(ctx, args...)
		}
	}

	err = eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest, eksinventory.OwnershipHints{},
		io.Discard, second,
	)
	if err != nil {
		t.Fatal(err)
	}

	var got ownershipSnapshot

	err = json.Unmarshal(readPrivateTestFile(t, manifest), &got)
	if err != nil {
		t.Fatal(err)
	}

	instances := make([]string, 0, 2)

	for _, resource := range got.Resources {
		if resource.Kind == "instance" {
			instances = append(instances, resource.Identity)
		}
	}

	if got.CaptureCount != 2 || !reflect.DeepEqual(
		instances,
		[]string{"i-0123456789abcdef0", "i-11111111111111111"},
	) {
		t.Fatalf(
			"scale-down erased cumulative identities: captures=%d instances=%v",
			got.CaptureCount,
			instances,
		)
	}
}

//nolint:cyclop // The failure fixture checks both evidence state and retained graph content.
func TestOwnershipCaptureKeepsPriorRelationshipsWhenALaterPageFails(t *testing.T) {
	t.Parallel()
	dir := privateBaselineDir(t)
	baseline := captureBaselineFixture(t, dir)
	manifest := filepath.Join(dir, "ownership.json")

	completeQuery := ownershipQuery(t)

	err := eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest, eksinventory.OwnershipHints{},
		io.Discard, completeQuery,
	)
	if err != nil {
		t.Fatal(err)
	}

	incompleteQuery := func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] == "cloudformation" && args[1] == "list-stack-resources" &&
			strings.Contains(args[8], "root-page-2") {
			return []byte(`{"StackResourceSummaries":[]}`), fs.ErrPermission
		}

		return completeQuery(ctx, args...)
	}

	err = eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest, eksinventory.OwnershipHints{},
		io.Discard, incompleteQuery,
	)
	if !errors.Is(err, eksinventory.ErrInventory) {
		t.Fatalf("later-page failure accepted: %v", err)
	}

	var got ownershipSnapshot

	err = json.Unmarshal(readPrivateTestFile(t, manifest), &got)
	if err != nil {
		t.Fatal(err)
	}

	coverage := got.Coverage["cloudformation"]
	if coverage.ReadComplete || coverage.Reason != "access-denied" ||
		got.CaptureCount != 2 || got.CompleteCaptureCount != 1 {
		t.Fatalf("incomplete attempt became complete evidence: %#v", got)
	}

	if !slices.ContainsFunc(got.Resources, func(resource ownedResource) bool {
		return resource.Kind == "nat-gateway" && resource.Identity == "nat-0123456789abcdef0"
	}) {
		t.Fatal("later-page failure erased a previously captured detached identity")
	}
}

func TestOwnershipCaptureRejectsPaginationOnASingleResponse(t *testing.T) {
	t.Parallel()
	dir := privateBaselineDir(t)
	baseline := captureBaselineFixture(t, dir)
	manifest := filepath.Join(dir, "ownership.json")
	base := ownershipQuery(t)
	query := func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] == "sts" && args[1] == "get-caller-identity" {
			return []byte(
				`{"Account":"123456789012","Arn":"arn:aws:sts::123456789012:assumed-role/eks-ci/run","NextToken":"hidden-page"}`,
			), nil
		}

		return base(ctx, args...)
	}

	err := eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest, eksinventory.OwnershipHints{},
		io.Discard, query,
	)
	if !errors.Is(err, eksinventory.ErrInventory) {
		t.Fatalf("unexpected pagination accepted: %v", err)
	}

	_, err = os.Stat(manifest)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("unbound ownership manifest was published: %v", err)
	}

	err = eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest, eksinventory.OwnershipHints{},
		io.Discard, base,
	)
	if err != nil {
		t.Fatalf("follow-up capture could not recover: %v", err)
	}

	var got ownershipSnapshot

	err = json.Unmarshal(readPrivateTestFile(t, manifest), &got)
	if err != nil {
		t.Fatal(err)
	}

	if got.AccountID != "123456789012" || !got.Coverage["account"].ReadComplete {
		t.Fatalf("follow-up capture did not publish a verified account binding: %#v", got)
	}
}

func TestOwnershipCaptureAcceptsNetworkLoadBalancerHostname(t *testing.T) {
	t.Parallel()
	dir := privateBaselineDir(t)
	baseline := captureBaselineFixture(t, dir)
	manifest := filepath.Join(dir, "ownership.json")
	base := ownershipQuery(t)
	nlbARN := "arn:aws:elasticloadbalancing:eu-central-1:123456789012:loadbalancer/net/owned/abc"
	nlbHost := "owned-123.elb.eu-central-1.amazonaws.com"
	query := func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] == "elbv2" && args[1] == "describe-load-balancers" {
			return []byte(`{"LoadBalancers":[{"LoadBalancerArn":"` + nlbARN +
				`","DNSName":"` + nlbHost + `","State":{"Code":"active"}}]}`), nil
		}

		return base(ctx, args...)
	}

	err := eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest, eksinventory.OwnershipHints{
			LoadBalancerHosts:    []string{nlbHost},
			WorkloadReadComplete: true,
		}, io.Discard, query,
	)
	if err != nil {
		t.Fatalf("network load balancer hostname rejected: %v", err)
	}

	var got ownershipSnapshot

	err = json.Unmarshal(readPrivateTestFile(t, manifest), &got)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.ContainsFunc(got.Resources, func(resource ownedResource) bool {
		return resource.Kind == "load-balancer" && resource.Identity == nlbARN
	}) {
		t.Fatalf("network load balancer identity was not recorded: %#v", got.Resources)
	}
}

//nolint:cyclop // Two incomplete passes prove failed-create evidence remains cumulative.
func TestOwnershipCaptureCanExtendFailedCreateStackEvidence(t *testing.T) {
	t.Parallel()
	dir := privateBaselineDir(t)
	baseline := captureBaselineFixture(t, dir)
	manifest := filepath.Join(dir, "ownership.json")
	base := ownershipQuery(t)
	reads := 0
	query := func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] == "sts" {
			reads++
		}

		if args[0] == "eks" && args[1] == "describe-cluster" {
			return nil, errTestClusterNotFound
		}

		return base(ctx, args...)
	}

	for range 2 {
		err := eksinventory.CaptureOwnership(
			context.Background(),
			baselineBinding(),
			baseline,
			manifest,
			eksinventory.OwnershipHints{},
			io.Discard,
			query,
		)
		if !errors.Is(err, eksinventory.ErrInventory) {
			t.Fatalf("failed-create capture did not remain incomplete: %v", err)
		}
	}

	var got ownershipSnapshot

	err := json.Unmarshal(readPrivateTestFile(t, manifest), &got)
	if err != nil {
		t.Fatal(err)
	}

	if reads != 2 || got.CaptureCount != 2 || got.Cluster != "" ||
		!slices.ContainsFunc(got.Resources, func(resource ownedResource) bool {
			return resource.Kind == "stack" && strings.Contains(resource.Identity, ":stack/root/")
		}) {
		t.Fatalf("failed-create evidence was not cumulative: reads=%d snapshot=%#v", reads, got)
	}
}

func TestOwnershipCaptureNamesTheFailedReadWithoutIdentifiers(t *testing.T) {
	t.Parallel()
	dir := privateBaselineDir(t)
	baseline := captureBaselineFixture(t, dir)
	manifest := filepath.Join(dir, "ownership.json")
	base := ownershipQuery(t)
	query := func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] == "cloudformation" && args[1] == "describe-stacks" &&
			strings.Contains(args[8], nodegroupStackName) {
			return nil, fs.ErrPermission
		}

		return base(ctx, args...)
	}

	var output bytes.Buffer

	err := eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest, eksinventory.OwnershipHints{},
		&output, query,
	)
	if !errors.Is(err, eksinventory.ErrInventory) {
		t.Fatalf("a denied compute read was accepted: %v", err)
	}

	summary := output.String()

	const failedCompute = "compute: read=unknown ownership=partial recorded=1" +
		" reason=access-denied last-read=cloudformation:describe-stacks\n"
	if !strings.Contains(summary, failedCompute) {
		t.Fatalf("summary does not name the failed read:\n%s", summary)
	}

	requirePublicOwnershipSummary(t, summary)
	requirePrivateFailedRead(t, manifest)
}

// requirePrivateFailedRead proves the owner-only manifest keeps the failed compute read and that
// the read is not attributed to the family captured after it.
func requirePrivateFailedRead(t *testing.T, manifest string) {
	t.Helper()

	var got ownershipSnapshot

	err := json.Unmarshal(readPrivateTestFile(t, manifest), &got)
	if err != nil {
		t.Fatal(err)
	}

	compute := got.Coverage["compute"]
	if compute.ReadComplete || compute.Reason != "access-denied" ||
		compute.LastRead != "cloudformation:describe-stacks" {
		t.Fatalf("private manifest did not keep the failed read: %#v", compute)
	}

	if network := got.Coverage["network"]; !network.ReadComplete || network.LastRead != "" {
		t.Fatalf("the failed read leaked into the next family: %#v", network)
	}
}

// requirePublicOwnershipSummary proves a capture summary keeps failure detail on incomplete
// families only and prints none of the fixture's account, ARN, cluster or node group identifiers.
func requirePublicOwnershipSummary(t *testing.T, summary string) {
	t.Helper()

	for line := range strings.SplitSeq(summary, "\n") {
		if strings.Contains(line, "read=complete") &&
			(strings.Contains(line, "reason=") || strings.Contains(line, "last-read=")) {
			t.Fatalf("a complete family carries failure detail: %q", line)
		}
	}

	for _, identifier := range []string{"123456789012", "arn:", "regional-diagnostic", "workers"} {
		if strings.Contains(summary, identifier) {
			t.Fatalf("public summary printed identifier %q:\n%s", identifier, summary)
		}
	}
}

// stackRead answers one CloudFormation read for a binding case; handled reports whether it did.
type stackRead func(command string, input map[string]any) (data []byte, err error, handled bool)

// TestOwnershipCaptureRefusesANodegroupStackItCannotBind proves the node group stack enters the
// ownership graph only when the stack eksctl named for the node group lists exactly that node group.
//
//nolint:funlen // Each case names one way a real stack can fail the binding.
func TestOwnershipCaptureRefusesANodegroupStackItCannotBind(t *testing.T) {
	t.Parallel()

	nodeGroupRow := func(physicalID string) string {
		return `{"LogicalResourceId":"ManagedNodeGroup","PhysicalResourceId":"` + physicalID +
			`","ResourceType":"AWS::EKS::Nodegroup","ResourceStatus":"CREATE_COMPLETE"}`
	}

	stackList := func(rows ...string) stackRead {
		return func(command string, input map[string]any) ([]byte, error, bool) {
			if command != "list-stack-resources" || input["StackName"] != nodegroupStackID {
				return nil, nil, false
			}

			return []byte(`{"StackResourceSummaries":[` + strings.Join(rows, ",") + `]}`), nil, true
		}
	}

	stackLookup := func(data []byte, err error) stackRead {
		return func(command string, input map[string]any) ([]byte, error, bool) {
			if command != "describe-stacks" || input["StackName"] != nodegroupStackName {
				return nil, nil, false
			}

			return data, err, true
		}
	}

	for _, test := range []struct {
		name   string
		reason string
		read   stackRead
	}{
		{
			name:   "stack is missing",
			reason: "not-found",
			read:   stackLookup(nil, eksinventory.ResourceNotFoundForTest()),
		},
		{
			name:   "stack identity names another stack",
			reason: "invalid-or-incomplete-data",
			read: stackLookup([]byte(`{"Stacks":[{"StackId":"arn:aws:cloudformation:eu-central-1:`+
				`123456789012:stack/`+nodegroupStackName+`-other/0f6c5e40"}]}`), nil),
		},
		{
			name:   "stack lists no node group",
			reason: "invalid-or-incomplete-data",
			read:   stackList(),
		},
		{
			name:   "stack lists another node group",
			reason: "invalid-or-incomplete-data",
			read:   stackList(nodeGroupRow("other-cluster/workers")),
		},
		{
			name:   "stack lists a second node group",
			reason: "invalid-or-incomplete-data",
			read: stackList(
				nodeGroupRow("regional-diagnostic/workers"), nodeGroupRow("regional-diagnostic/workers"),
			),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireUnboundNodegroupStackRefused(t, test.read, test.reason)
		})
	}
}

// withStackRead answers the CloudFormation reads read handles and leaves the rest to the fixture.
func withStackRead(t *testing.T, read stackRead) eksinventory.Query {
	t.Helper()

	base := ownershipQuery(t)

	return func(ctx context.Context, args ...string) ([]byte, error) {
		var input map[string]any

		err := json.Unmarshal([]byte(args[8]), &input)
		if err != nil {
			t.Fatal(err)
		}

		if args[0] == "cloudformation" {
			data, readErr, handled := read(args[1], input)
			if handled {
				return data, readErr
			}
		}

		return base(ctx, args...)
	}
}

func requireUnboundNodegroupStackRefused(t *testing.T, read stackRead, reason string) {
	t.Helper()

	dir := privateBaselineDir(t)
	baseline := captureBaselineFixture(t, dir)
	manifest := filepath.Join(dir, "ownership.json")

	err := eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest,
		eksinventory.OwnershipHints{}, io.Discard, withStackRead(t, read),
	)
	if !errors.Is(err, eksinventory.ErrInventory) {
		t.Fatalf("an unbound node group stack was accepted: %v", err)
	}

	var got ownershipSnapshot

	err = json.Unmarshal(readPrivateTestFile(t, manifest), &got)
	if err != nil {
		t.Fatal(err)
	}

	compute := got.Coverage["compute"]
	if compute.ReadComplete || compute.Reason != reason {
		t.Fatalf("compute coverage = %#v, want reason %q", compute, reason)
	}

	for _, resource := range got.Resources {
		if (resource.Kind == "stack" && strings.HasPrefix(resource.Parent, "nodegroup:")) ||
			resource.Kind == "autoscaling-group" {
			t.Fatalf("an unbound node group stack entered the graph: %#v", resource)
		}
	}
}
