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
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/maintenance/eksinventory"
)

var errFixtureResourceAbsent = errors.New("fixture resource absent")

func ownershipVerificationFixture(t *testing.T) string {
	t.Helper()
	dir := privateBaselineDir(t)
	baseline := captureBaselineFixture(t, dir)
	manifest := filepath.Join(dir, "ownership.json")

	err := eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest,
		eksinventory.OwnershipHints{
			ClassicLoadBalancers: []string{"classic-owned"},
			LoadBalancerARNs: []string{
				"arn:aws:elasticloadbalancing:eu-central-1:123456789012:loadbalancer/app/owned/abc",
			},
			LoadBalancerHosts: []string{
				"classic-owned.eu-central-1.elb.amazonaws.com",
				"owned-123.eu-central-1.elb.amazonaws.com",
			},
			VolumeIDs:            []string{"vol-11111111111111111"},
			WorkloadReadComplete: true,
		},
		io.Discard,
		ownershipQuery(t),
	)
	if err != nil {
		t.Fatal(err)
	}

	return manifest
}

func TestOwnershipVerificationAcceptsOnlyACompleteCleanTeardown(t *testing.T) {
	t.Parallel()
	manifest := ownershipVerificationFixture(t)

	var output bytes.Buffer

	err := eksinventory.VerifyOwnership(
		context.Background(), baselineBinding(), manifest, &output,
		verificationQuery(t, "", nil),
	)
	if err != nil {
		t.Fatal(err)
	}

	got := output.String()
	if !strings.Contains(got, "cleanup-verdict=absent") ||
		strings.Contains(got, "123456789012") || strings.Contains(got, "vol-") ||
		strings.Contains(got, "arn:aws") {
		t.Fatalf("unsafe or incomplete teardown summary: %s", got)
	}
}

func TestOwnershipVerificationAcceptsACompleteZeroFleetCapture(t *testing.T) {
	t.Parallel()
	dir := privateBaselineDir(t)
	baseline := captureBaselineFixture(t, dir)
	manifest := filepath.Join(dir, "ownership.json")
	query := ownershipQuery(t)

	withoutFleet := func(ctx context.Context, args ...string) ([]byte, error) {
		var input map[string]any

		err := json.Unmarshal([]byte(args[8]), &input)
		if err != nil {
			t.Fatal(err)
		}

		if args[0] == "cloudformation" && args[1] == "list-stack-resources" &&
			input["StackName"] == "arn:aws:cloudformation:eu-central-1:123456789012:stack/nested/two" {
			return []byte(`{"StackResourceSummaries":[]}`), nil
		}

		if args[0] == "ec2" && args[1] == "describe-fleets" {
			t.Fatal("zero-fleet capture attempted an exact fleet read")
		}

		return query(ctx, args...)
	}

	err := eksinventory.CaptureOwnership(
		context.Background(), baselineBinding(), baseline, manifest,
		eksinventory.OwnershipHints{WorkloadReadComplete: true}, io.Discard, withoutFleet,
	)
	if err != nil {
		t.Fatal(err)
	}

	err = eksinventory.VerifyOwnership(
		context.Background(), baselineBinding(), manifest, io.Discard,
		verificationQuery(t, "", nil),
	)
	if err != nil {
		t.Fatalf("complete zero-fleet capture was rejected: %v", err)
	}
}

func TestOwnershipVerificationRejectsEveryRecordedLeftoverKind(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{
		"cluster", "stack", "nodegroup", "autoscaling-group", "instance", "fleet",
		"nat-gateway", "address", "network-interface", "volume",
		"classic-load-balancer", "load-balancer", "vpc",
	} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			manifest := ownershipVerificationFixture(t)

			var output bytes.Buffer

			err := eksinventory.VerifyOwnership(
				context.Background(), baselineBinding(), manifest, &output,
				verificationQuery(t, kind, nil),
			)
			if !errors.Is(err, eksinventory.ErrInventory) {
				t.Fatalf("%s leftover accepted: %v", kind, err)
			}

			got := output.String()
			if !strings.Contains(got, kind+": state=remaining count=1") ||
				strings.Contains(got, "123456789012") || strings.Contains(got, "arn:aws") {
				t.Fatalf("unsafe or missing %s verdict: %s", kind, got)
			}
		})
	}
}

func TestOwnershipVerificationTreatsFailedNATGatewayAsRemaining(t *testing.T) {
	t.Parallel()
	manifest := ownershipVerificationFixture(t)

	var output bytes.Buffer

	err := eksinventory.VerifyOwnership(
		context.Background(), baselineBinding(), manifest, &output,
		verificationQuery(t, "nat-gateway-failed", nil),
	)
	if !errors.Is(err, eksinventory.ErrInventory) ||
		!strings.Contains(output.String(), "nat-gateway: state=remaining count=1") {
		t.Fatalf(
			"failed NAT gateway became clean teardown evidence: err=%v output=%s",
			err,
			output.String(),
		)
	}
}

func TestOwnershipVerificationRejectsIdentityReplacement(t *testing.T) {
	t.Parallel()
	manifest := ownershipVerificationFixture(t)

	var output bytes.Buffer

	err := eksinventory.VerifyOwnership(
		context.Background(), baselineBinding(), manifest, &output,
		verificationQuery(t, "cluster-replacement", nil),
	)
	if !errors.Is(err, eksinventory.ErrInventory) ||
		!strings.Contains(
			output.String(),
			"cluster: state=unknown count=1 reason=identity-replaced",
		) {
		t.Fatalf("cluster replacement was not rejected: err=%v output=%s", err, output.String())
	}
}

//nolint:forcetypeassert // The fixture is decoded from the manifest written immediately above.
func TestOwnershipVerificationRejectsIncompleteEvidenceBeforeAWSReads(t *testing.T) {
	t.Parallel()
	manifest := ownershipVerificationFixture(t)

	var snapshot map[string]any

	err := json.Unmarshal(readPrivateTestFile(t, manifest), &snapshot)
	if err != nil {
		t.Fatal(err)
	}

	coverage := snapshot["coverage"].(map[string]any)
	instant := coverage["instantFleets"].(map[string]any)
	instant["ownershipCoverage"] = "unknown"

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(manifest, data, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	reads := 0
	query := func(context.Context, ...string) ([]byte, error) {
		reads++

		return nil, nil
	}

	var output bytes.Buffer

	err = eksinventory.VerifyOwnership(
		context.Background(), baselineBinding(), manifest, &output, query,
	)
	if !errors.Is(err, eksinventory.ErrInventory) || reads != 0 ||
		!strings.Contains(output.String(), "cleanup-verdict=unknown") {
		t.Fatalf(
			"incomplete ownership reached AWS or passed: reads=%d err=%v output=%s",
			reads,
			err,
			output.String(),
		)
	}
}

func TestOwnershipVerificationRejectsALaterPageFailureWithoutPartialCounts(t *testing.T) {
	t.Parallel()
	manifest := ownershipVerificationFixture(t)
	regionalVolumeRead := func(_ context.Context, args ...string) ([]byte, error) {
		var input map[string]any

		err := json.Unmarshal([]byte(args[8]), &input)
		if err != nil {
			t.Fatal(err)
		}

		if input["NextToken"] != nil {
			return []byte(`{"Volumes":[]}`), fs.ErrPermission
		}

		return []byte(
			`{"Volumes":[{"VolumeId":"private-partial","State":"available"}],"NextToken":"page-2"}`,
		), nil
	}

	var output bytes.Buffer

	err := eksinventory.VerifyOwnership(
		context.Background(), baselineBinding(), manifest, &output,
		verificationQuery(t, "", regionalVolumeRead),
	)
	if !errors.Is(err, eksinventory.ErrInventory) ||
		!strings.Contains(
			output.String(),
			"regional-census: state=unknown count=1 reason=access-denied",
		) ||
		strings.Contains(output.String(), "private-partial") {
		t.Fatalf(
			"partial regional read became cleanup evidence: err=%v output=%s",
			err,
			output.String(),
		)
	}
}

func TestOwnershipVerificationRejectsAnUnattributedResourceInItsOwnedVPC(t *testing.T) {
	t.Parallel()
	manifest := ownershipVerificationFixture(t)

	var output bytes.Buffer

	err := eksinventory.VerifyOwnership(
		context.Background(), baselineBinding(), manifest, &output,
		verificationQuery(t, "vpc-unattributed", nil),
	)
	if !errors.Is(err, eksinventory.ErrInventory) ||
		!strings.Contains(
			output.String(),
			"vpc-census: state=unknown count=1 reason=unattributed-vpc-resource",
		) ||
		strings.Contains(output.String(), "eni-unattributed") {
		t.Fatalf(
			"unattributed VPC resource became clean evidence: err=%v output=%s",
			err,
			output.String(),
		)
	}
}

func TestOwnershipVerificationTreatsOnlyAbsentClassicELBVPCsAsUnrelated(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name          string
		row           string
		expectedState string
	}{
		{name: "missing", row: `{"LoadBalancerName":"legacy-classic"}`, expectedState: "absent"},
		{name: "null", row: `{"LoadBalancerName":"legacy-classic","VPCId":null}`, expectedState: "absent"},
		{name: "malformed", row: `{"LoadBalancerName":"legacy-classic","VPCId":42}`, expectedState: "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			manifest := ownershipVerificationFixture(t)
			query := verificationQuery(t, "vpc", nil)
			vpcCensusReads := 0

			withClassicELB := func(ctx context.Context, args ...string) ([]byte, error) {
				var input map[string]any

				err := json.Unmarshal([]byte(args[8]), &input)
				if err != nil {
					t.Fatal(err)
				}

				if args[0] == "elb" && args[1] == "describe-load-balancers" &&
					input["LoadBalancerNames"] == nil && vpcCensusReads == 0 {
					vpcCensusReads++

					return []byte(`{"LoadBalancerDescriptions":[` + test.row + `]}`), nil
				}

				return query(ctx, args...)
			}

			var output bytes.Buffer

			err := eksinventory.VerifyOwnership(
				context.Background(), baselineBinding(), manifest, &output, withClassicELB,
			)
			if !errors.Is(err, eksinventory.ErrInventory) || vpcCensusReads != 1 ||
				!strings.Contains(
					output.String(),
					"vpc-census: state="+test.expectedState+" count=1",
				) {
				t.Fatalf(
					"classic ELB VPC classification failed: reads=%d err=%v output=%s",
					vpcCensusReads,
					err,
					output.String(),
				)
			}
		})
	}
}

func TestOwnershipVerificationRejectsMissingVPCOnFilteredEC2Rows(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		vpc  string
	}{
		{name: "missing"},
		{name: "null", vpc: `,"VpcId":null`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			manifest := ownershipVerificationFixture(t)
			query := verificationQuery(t, "vpc", nil)

			withIncompleteFilteredRow := func(ctx context.Context, args ...string) ([]byte, error) {
				var input map[string]any

				err := json.Unmarshal([]byte(args[8]), &input)
				if err != nil {
					t.Fatal(err)
				}

				if args[0] == "ec2" && args[1] == "describe-instances" &&
					input["InstanceIds"] == nil && input["Filters"] != nil {
					return []byte(
						`{"Reservations":[{"Instances":[{"InstanceId":"i-incomplete"` +
							test.vpc + `}]}]}`,
					), nil
				}

				return query(ctx, args...)
			}

			var output bytes.Buffer

			err := eksinventory.VerifyOwnership(
				context.Background(), baselineBinding(), manifest, &output,
				withIncompleteFilteredRow,
			)
			if !errors.Is(err, eksinventory.ErrInventory) ||
				!strings.Contains(
					output.String(),
					"vpc-census: state=unknown count=1 reason=invalid-or-incomplete-data",
				) {
				t.Fatalf(
					"incomplete filtered VPC row became clean evidence: err=%v output=%s",
					err,
					output.String(),
				)
			}
		})
	}
}

func TestOwnershipVerificationRejectsAnUnattributedRegionalSurvivor(t *testing.T) {
	t.Parallel()
	manifest := ownershipVerificationFixture(t)
	regionalVolumeRead := func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte(`{"Volumes":[{"VolumeId":"private-new-volume","State":"available"}]}`), nil
	}

	var output bytes.Buffer

	err := eksinventory.VerifyOwnership(
		context.Background(), baselineBinding(), manifest, &output,
		verificationQuery(t, "", regionalVolumeRead),
	)
	if !errors.Is(err, eksinventory.ErrInventory) ||
		!strings.Contains(
			output.String(),
			"regional-census: state=unknown count=1 reason=unattributed-new-resource",
		) ||
		strings.Contains(output.String(), "private-new-volume") {
		t.Fatalf(
			"unattributed regional survivor became clean evidence: err=%v output=%s",
			err,
			output.String(),
		)
	}
}

func TestOwnershipVerificationNamespacesRegionalIdentitiesByOperation(t *testing.T) {
	t.Parallel()
	manifest := ownershipVerificationFixture(t)
	query := verificationQuery(t, "", nil)

	withCollidingClassicELB := func(ctx context.Context, args ...string) ([]byte, error) {
		var input map[string]any

		err := json.Unmarshal([]byte(args[8]), &input)
		if err != nil {
			t.Fatal(err)
		}

		if args[0] == "elb" && args[1] == "describe-load-balancers" &&
			input["LoadBalancerNames"] == nil {
			return []byte(
				`{"LoadBalancerDescriptions":[{"LoadBalancerName":"regional-diagnostic"}]}`,
			), nil
		}

		return query(ctx, args...)
	}

	var output bytes.Buffer

	err := eksinventory.VerifyOwnership(
		context.Background(), baselineBinding(), manifest, &output, withCollidingClassicELB,
	)
	if !errors.Is(err, eksinventory.ErrInventory) ||
		!strings.Contains(
			output.String(),
			"regional-census: state=unknown count=1 reason=unattributed-new-resource",
		) {
		t.Fatalf(
			"cross-family identity collision hid a survivor: err=%v output=%s",
			err,
			output.String(),
		)
	}
}

//nolint:cyclop,funlen,gocognit,gocyclo,lll,maintidx // One coherent provider fixture covers all exact and regional reads.
func verificationQuery(
	t *testing.T,
	remainingKind string,
	regionalVolumes eksinventory.Query,
) eksinventory.Query {
	t.Helper()
	regional := baselineQuery(t, regionalVolumes)
	notFound := eksinventory.ResourceNotFoundForTest()

	return func(ctx context.Context, args ...string) ([]byte, error) {
		key := args[0] + " " + args[1]

		var input map[string]any

		err := json.Unmarshal([]byte(args[8]), &input)
		if err != nil {
			t.Fatal(err)
		}

		switch key {
		case "sts get-caller-identity":
			return []byte(
				`{"Account":"123456789012","Arn":"arn:aws:sts::123456789012:assumed-role/eks-ci/verify"}`,
			), nil
		case "eks describe-cluster":
			if remainingKind == "cluster" || remainingKind == "cluster-replacement" {
				createdAt := "2024-03-14T15:31:44.348Z"
				if remainingKind == "cluster-replacement" {
					createdAt = "2025-03-14T15:31:44.348Z"
				}

				return []byte(
					`{"cluster":{"arn":"arn:aws:eks:eu-central-1:123456789012:cluster/regional-diagnostic","createdAt":"` + createdAt + `"}}`,
				), nil
			}

			return nil, errors.Join(errFixtureResourceAbsent, notFound)
		case "cloudformation describe-stacks":
			identity, _ := input["StackName"].(string)
			if remainingKind == "stack" && strings.Contains(identity, ":stack/root/") {
				return []byte(
					`{"Stacks":[{"StackId":"` + identity + `","StackStatus":"DELETE_FAILED"}]}`,
				), nil
			}

			return nil, errors.Join(errFixtureResourceAbsent, notFound)
		case "eks describe-nodegroup":
			if remainingKind == "nodegroup" {
				return []byte(
					`{"nodegroup":{"nodegroupArn":"arn:aws:eks:eu-central-1:123456789012:nodegroup/regional-diagnostic/workers/abc","status":"ACTIVE"}}`,
				), nil
			}

			return nil, errors.Join(errFixtureResourceAbsent, notFound)
		case "autoscaling describe-auto-scaling-groups":
			if input["AutoScalingGroupNames"] == nil {
				return regional(ctx, args...)
			}

			if remainingKind == "autoscaling-group" {
				return []byte(
					`{"AutoScalingGroups":[{"AutoScalingGroupARN":"arn:aws:autoscaling:eu-central-1:123456789012:autoScalingGroup:abc:autoScalingGroupName/eks-workers","AutoScalingGroupName":"eks-workers","DesiredCapacity":0}]}`,
				), nil
			}

			return []byte(`{"AutoScalingGroups":[]}`), nil
		case "ec2 describe-instances":
			if input["InstanceIds"] == nil {
				return regional(ctx, args...)
			}

			if remainingKind == "instance" {
				return []byte(
					`{"Reservations":[{"Instances":[{"InstanceId":"i-0123456789abcdef0","State":{"Name":"running"}}]}]}`,
				), nil
			}

			return []byte(`{"Reservations":[]}`), nil
		case "ec2 describe-fleets":
			if input["FleetIds"] == nil {
				return regional(ctx, args...)
			}

			if remainingKind == "fleet" {
				return []byte(
					`{"Fleets":[{"FleetId":"fleet-0123456789abcdef0","FleetState":"active","Type":"instant"}]}`,
				), nil
			}

			return []byte(`{"Fleets":[]}`), nil
		case "ec2 describe-nat-gateways":
			if input["NatGatewayIds"] == nil {
				return regional(ctx, args...)
			}

			if remainingKind == "nat-gateway" || remainingKind == "nat-gateway-failed" {
				state := "available"
				if remainingKind == "nat-gateway-failed" {
					state = "failed"
				}

				return []byte(
					`{"NatGateways":[{"NatGatewayId":"nat-0123456789abcdef0","State":"` +
						state + `"}]}`,
				), nil
			}

			return []byte(`{"NatGateways":[]}`), nil
		case "ec2 describe-addresses":
			if input["AllocationIds"] == nil {
				return regional(ctx, args...)
			}

			if remainingKind == "address" {
				return []byte(`{"Addresses":[{"AllocationId":"eipalloc-0123456789abcdef0"}]}`), nil
			}

			return []byte(`{"Addresses":[]}`), nil
		case "ec2 describe-network-interfaces":
			if input["NetworkInterfaceIds"] == nil {
				if remainingKind == "vpc-unattributed" && input["Filters"] != nil {
					return []byte(
						`{"NetworkInterfaces":[{"NetworkInterfaceId":"eni-unattributed","VpcId":"vpc-0123456789abcdef0","Status":"available"}]}`,
					), nil
				}

				return regional(ctx, args...)
			}

			if remainingKind == "network-interface" &&
				strings.Contains(args[8], "eni-0123456789abcdef0") {
				return []byte(
					`{"NetworkInterfaces":[{"NetworkInterfaceId":"eni-0123456789abcdef0","Status":"available"}]}`,
				), nil
			}

			return []byte(`{"NetworkInterfaces":[]}`), nil
		case "ec2 describe-volumes":
			if input["VolumeIds"] == nil {
				return regional(ctx, args...)
			}

			if remainingKind == "volume" && strings.Contains(args[8], "vol-0123456789abcdef0") {
				return []byte(
					`{"Volumes":[{"VolumeId":"vol-0123456789abcdef0","State":"available"}]}`,
				), nil
			}

			return []byte(`{"Volumes":[]}`), nil
		case "ec2 describe-vpcs":
			if remainingKind == "vpc" || remainingKind == "vpc-unattributed" {
				return []byte(
					`{"Vpcs":[{"VpcId":"vpc-0123456789abcdef0","State":"available"}]}`,
				), nil
			}

			return []byte(`{"Vpcs":[]}`), nil
		case "elb describe-load-balancers":
			if input["LoadBalancerNames"] == nil {
				return regional(ctx, args...)
			}

			if remainingKind == "classic-load-balancer" {
				return []byte(
					`{"LoadBalancerDescriptions":[{"LoadBalancerName":"classic-owned"}]}`,
				), nil
			}

			return []byte(`{"LoadBalancerDescriptions":[]}`), nil
		case "elbv2 describe-load-balancers":
			if input["LoadBalancerArns"] == nil {
				return regional(ctx, args...)
			}

			if remainingKind == "load-balancer" {
				return []byte(
					`{"LoadBalancers":[{"LoadBalancerArn":"arn:aws:elasticloadbalancing:eu-central-1:123456789012:loadbalancer/app/owned/abc","State":{"Code":"active"}}]}`,
				), nil
			}

			return []byte(`{"LoadBalancers":[]}`), nil
		default:
			return regional(ctx, args...)
		}
	}
}
