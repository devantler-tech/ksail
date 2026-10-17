package eksinventory_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/maintenance/eksinventory"
)

func TestAWSBoundaryRejectsMutationsBeforeStartingAProcess(t *testing.T) {
	if os.Getenv("KSAIL_EKS_TEST_PROCESS") != "" {
		helperProcess()

		return
	}

	dir := prepareAWSProcess(t)
	marker := filepath.Join(dir, "invoked")

	_, err := eksinventory.QueryAWSForTest(
		context.Background(),
		"ec2",
		"delete-volume",
		"--region",
		"eu-central-1",
		"--output",
		"json",
		"--no-paginate",
		"--cli-input-json",
		"{}",
		"--no-cli-pager",
	)
	if err == nil {
		t.Fatal("mutation was accepted")
	}

	_, err = os.Stat(marker)
	if !os.IsNotExist(err) {
		t.Fatal("forbidden operation started a process")
	}

	t.Setenv("KSAIL_EKS_TEST_PROCESS", "timeout")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	data, err := eksinventory.QueryAWSForTest(
		ctx,
		"ec2",
		"describe-volumes",
		"--region",
		"eu-central-1",
		"--output",
		"json",
		"--no-paginate",
		"--cli-input-json",
		"{}",
		"--no-cli-pager",
	)
	if err == nil || len(data) != 0 {
		t.Fatal("timed-out process retained plausible stdout")
	}
}

//nolint:paralleltest // The subprocess fixture mutates PATH and process-control environment variables.
func TestAWSBoundaryAllowsOnlyOwnershipReadOperations(t *testing.T) {
	if os.Getenv("KSAIL_EKS_TEST_PROCESS") != "" {
		helperProcess()

		return
	}

	prepareAWSProcess(t)

	for _, operation := range [][2]string{
		{"sts", "get-caller-identity"},
		{"eks", "describe-cluster"},
		{"eks", "list-nodegroups"},
		{"eks", "describe-nodegroup"},
		{"cloudformation", "describe-stacks"},
		{"cloudformation", "list-stack-resources"},
		{"cloudformation", "describe-stack-resources"},
		{"ec2", "describe-vpcs"},
	} {
		_, err := eksinventory.QueryAWSForTest(
			context.Background(), operation[0], operation[1],
			"--region", "eu-central-1", "--output", "json", "--no-paginate",
			"--cli-input-json", "{}", "--no-cli-pager",
		)
		if err != nil {
			t.Fatalf("ownership read %s %s rejected: %v", operation[0], operation[1], err)
		}
	}
}

func TestAWSBoundaryClassifiesOnlyExactReadNotFoundResponses(t *testing.T) {
	if os.Getenv("KSAIL_EKS_TEST_PROCESS") != "" {
		helperProcess()

		return
	}

	prepareAWSProcess(t)
	t.Setenv("KSAIL_EKS_TEST_PROCESS", "not-found-exact")

	for _, operation := range [][2]string{
		{"eks", "describe-cluster"},
		{"eks", "describe-nodegroup"},
		{"cloudformation", "describe-stacks"},
		{"ec2", "describe-instances"},
		{"ec2", "describe-fleets"},
		{"ec2", "describe-nat-gateways"},
		{"ec2", "describe-addresses"},
		{"ec2", "describe-network-interfaces"},
		{"ec2", "describe-volumes"},
		{"ec2", "describe-vpcs"},
		{"elb", "describe-load-balancers"},
		{"elbv2", "describe-load-balancers"},
	} {
		_, err := eksinventory.QueryAWSForTest(
			context.Background(), operation[0], operation[1], "--region", "eu-central-1",
			"--output", "json", "--no-paginate", "--cli-input-json", "{}", "--no-cli-pager",
		)
		if eksinventory.FailureReasonForTest(err) != "not-found" {
			t.Fatalf(
				"%s %s absence was not distinguished from an incomplete read: %v",
				operation[0], operation[1], err,
			)
		}
	}
}

func prepareAWSProcess(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	err = os.Symlink(executable, filepath.Join(dir, "aws"))
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir)
	t.Setenv("KSAIL_EKS_TEST_PROCESS", "invoked")

	t.Setenv("KSAIL_EKS_TEST_DIR", dir)

	return dir
}

//nolint:lll // Exact AWS CLI error fixtures retain the operation and error-code spellings.
func helperProcess() {
	if os.Getenv("KSAIL_EKS_TEST_PROCESS") == "not-found-exact" {
		diagnostics := map[string]string{
			"eks describe-cluster":            "An error occurred (ResourceNotFoundException) when calling the DescribeCluster operation: No cluster found",
			"eks describe-nodegroup":          "An error occurred (ResourceNotFoundException) when calling the DescribeNodegroup operation: No node group found",
			"cloudformation describe-stacks":  "An error occurred (ValidationError) when calling the DescribeStacks operation: Stack does not exist",
			"ec2 describe-instances":          "An error occurred (InvalidInstanceID.NotFound) when calling the DescribeInstances operation: not found",
			"ec2 describe-fleets":             "An error occurred (InvalidFleetId.NotFound) when calling the DescribeFleets operation: not found",
			"ec2 describe-nat-gateways":       "An error occurred (NatGatewayNotFound) when calling the DescribeNatGateways operation: not found",
			"ec2 describe-addresses":          "An error occurred (InvalidAllocationID.NotFound) when calling the DescribeAddresses operation: not found",
			"ec2 describe-network-interfaces": "An error occurred (InvalidNetworkInterfaceID.NotFound) when calling the DescribeNetworkInterfaces operation: not found",
			"ec2 describe-volumes":            "An error occurred (InvalidVolume.NotFound) when calling the DescribeVolumes operation: not found",
			"ec2 describe-vpcs":               "An error occurred (InvalidVpcID.NotFound) when calling the DescribeVpcs operation: not found",
			"elb describe-load-balancers":     "An error occurred (LoadBalancerNotFound) when calling the DescribeLoadBalancers operation: not found",
			"elbv2 describe-load-balancers":   "An error occurred (LoadBalancerNotFound) when calling the DescribeLoadBalancers operation: not found",
		}
		_, _ = fmt.Fprintln(os.Stderr, diagnostics[os.Args[1]+" "+os.Args[2]])

		os.Exit(254)
	}

	if os.Getenv("KSAIL_EKS_TEST_PROCESS") == "timeout" {
		_, _ = fmt.Fprintln(os.Stdout, `{"Volumes":[]}`)

		time.Sleep(time.Minute)
		os.Exit(0)
	}

	root, err := os.OpenRoot(os.Getenv("KSAIL_EKS_TEST_DIR"))
	if err != nil {
		os.Exit(1)
	}

	err = root.WriteFile("invoked", []byte("invoked"), 0o600)
	if err != nil {
		os.Exit(1)
	}

	err = root.Close()
	if err != nil {
		os.Exit(1)
	}

	os.Exit(0)
}
