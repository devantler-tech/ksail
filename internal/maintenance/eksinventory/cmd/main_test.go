package main_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	command "github.com/devantler-tech/ksail/v7/internal/maintenance/eksinventory/cmd"
)

func prepareBaselineAWS(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "private")

	err := os.Mkdir(dir, 0o700)
	if err != nil {
		t.Fatal(err)
	}

	aws := filepath.Join(dir, "aws")

	//nolint:gosec // Private executable fixture; owner execution is required.
	err = os.WriteFile(
		aws,
		[]byte("#!/bin/sh\ncase \"$1 $2\" in\n"+
			"'eks list-clusters') echo '{\"clusters\":[]}' ;;\n"+
			"'cloudformation list-stacks') echo '{\"StackSummaries\":[]}' ;;\n"+
			"'ec2 describe-fleets') echo '{\"Fleets\":[]}' ;;\n"+
			"'ec2 describe-spot-fleet-requests') echo '{\"SpotFleetRequestConfigs\":[]}' ;;\n"+
			"'ec2 describe-spot-instance-requests') echo '{\"SpotInstanceRequests\":[]}' ;;\n"+
			"'ec2 describe-instances') echo '{\"Reservations\":[]}' ;;\n"+
			"'autoscaling describe-auto-scaling-groups') echo '{\"AutoScalingGroups\":[]}' ;;\n"+
			"'ec2 describe-nat-gateways') echo '{\"NatGateways\":[]}' ;;\n"+
			"'ec2 describe-addresses') echo '{\"Addresses\":[]}' ;;\n"+
			"'ec2 describe-network-interfaces') echo '{\"NetworkInterfaces\":[]}' ;;\n"+
			"'ec2 describe-volumes') echo '{\"Volumes\":[]}' ;;\n"+
			"'elb describe-load-balancers') echo '{\"LoadBalancerDescriptions\":[]}' ;;\n"+
			"'elbv2 describe-load-balancers') echo '{\"LoadBalancers\":[]}' ;;\n"+
			"'service-quotas list-service-quotas') echo '{\"Quotas\":[]}' ;;\n"+
			"*) exit 91 ;;\nesac\n"),
		0o700,
	)
	if err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestBaselineRefusalPrintsSanitizedDiagnostic(t *testing.T) {
	dir := prepareBaselineAWS(t)
	t.Setenv("PATH", dir)

	manifest := filepath.Join(dir, "baseline.json")

	err := os.WriteFile(manifest, []byte("preserve"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer

	got := command.RunForTest([]string{
		"--region", "eu-central-1", "--baseline-file", manifest,
		"--cluster-name", "trial", "--run-id", "123", "--run-attempt", "2",
		"--source-sha", strings.Repeat("a", 40),
	}, &output)
	if got != 1 || !strings.Contains(output.String(), "Baseline capture failed") ||
		strings.Contains(output.String(), dir) {
		t.Fatalf("missing sanitized refusal: status=%d output=%s", got, output.String())
	}
}

func TestBaselineCaptureWritesPrivateRunBoundEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("baseline capture requires Unix directory ownership and permissions")
	}

	dir := prepareBaselineAWS(t)
	t.Setenv("PATH", dir)
	manifest := filepath.Join(dir, "baseline.json")

	var output bytes.Buffer

	got := command.RunForTest([]string{
		"--region", "eu-central-1", "--baseline-file", manifest,
		"--cluster-name", "trial", "--run-id", "123", "--run-attempt", "2",
		"--source-sha", strings.Repeat("a", 40),
	}, &output)
	if got != 0 {
		t.Fatalf("capture exit %d: %s", got, output.String())
	}

	info, err := os.Stat(manifest)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0o600 {
		t.Fatalf("manifest mode %v permits unintended readers", info.Mode())
	}
}

func TestUsageDoesNotRequireAWS(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		args []string
		want int
	}{
		{[]string{"--help"}, 0},
		{nil, 2},
		{[]string{"--region"}, 2},
		{[]string{"--unknown"}, 2},
		{[]string{"--region", "eu-central-1;command"}, 2},
		{[]string{"--region", "eu-central-1", "extra"}, 2},
		{[]string{"--region", "eu-central-1", "--baseline-file", ""}, 2},
		{[]string{"--region", "eu-central-1", "--run-id", "123"}, 2},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			if got := command.RunForTest(test.args, &output); got != test.want {
				t.Fatalf("exit %d, want %d", got, test.want)
			}
		})
	}
}
