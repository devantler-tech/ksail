package eksinventory_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/maintenance/eksinventory"
)

func readFixture(t *testing.T, name string, target any) {
	t.Helper()

	root, err := os.OpenRoot("testdata")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		closeErr := root.Close()
		if closeErr != nil {
			t.Error(closeErr)
		}
	})

	data, err := root.ReadFile(name + ".json")
	if err != nil {
		t.Fatal(err)
	}

	err = json.Unmarshal(data, target)
	if err != nil {
		t.Fatal(err)
	}
}

func emptyPages(t *testing.T) map[string]json.RawMessage {
	t.Helper()

	var pages map[string]json.RawMessage
	readFixture(t, "empty", &pages)

	return pages
}

func executeReport(t *testing.T, query eksinventory.Query) string {
	t.Helper()

	var output bytes.Buffer

	err := eksinventory.Report(context.Background(), "eu-central-1", &output, query)
	if err != nil {
		t.Fatal(err)
	}

	return output.String()
}

func report(t *testing.T, pages map[string]json.RawMessage) string {
	t.Helper()

	var requests map[string]map[string]any
	readFixture(t, "requests", &requests)

	return executeReport(t, func(_ context.Context, args ...string) ([]byte, error) {
		validateRequest(t, args, requests)

		body, found := pages[args[0]+" "+args[1]]
		if !found {
			t.Fatal("unexpected operation")
		}

		return body, nil
	})
}

func validateRequest(t *testing.T, args []string, expected map[string]map[string]any) {
	t.Helper()

	if len(args) != 10 {
		t.Fatal("unexpected AWS argument count")
	}

	flags := []string{
		"--region",
		"eu-central-1",
		"--output",
		"json",
		"--no-paginate",
		"--cli-input-json",
	}
	if !slices.Equal(args[2:8], flags) || args[9] != "--no-cli-pager" {
		t.Fatal("unexpected AWS read flags")
	}

	var input map[string]any

	err := json.Unmarshal([]byte(args[8]), &input)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(input, expected[args[0]+" "+args[1]]) {
		t.Fatal("request differs from documented API contract")
	}
}

type resourceCase struct {
	Operation string          `json:"operation"`
	Body      json.RawMessage `json:"body"`
	Want      string          `json:"want"`
}

func TestReportCountsEveryResourceFamily(t *testing.T) {
	t.Parallel()

	var cases []resourceCase
	readFixture(t, "resources", &cases)

	for _, test := range cases {
		t.Run(test.Operation, func(t *testing.T) {
			t.Parallel()
			pages := emptyPages(t)
			pages[test.Operation] = test.Body

			got := report(t, pages)
			if !strings.Contains(got, test.Want) || strings.Contains(got, "private-") {
				t.Fatalf("missing aggregate or leaked identity:\n%s", got)
			}
		})
	}
}

func TestReportRejectsIncompleteOrMalformedInventories(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`{}`, `null`, `{"Volumes":null}`, `{"Volumes":{}}`, `{"Volumes":[{}]}`,
		`{"Volumes":[{"VolumeId":"secret","State":"arn:private"}]}`,
		`{"Volumes":[{"VolumeId":"secret","State":"available"},{"VolumeId":"secret","State":"available"}]}`,
		`{"Volumes":[]} {"Volumes":[]}`, `{"Volumes":[],"NextToken":123}`, `{"Volumes":`,
		`{"Volumes":[],"nextToken":"private-hidden-page"}`,
	} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			pages := emptyPages(t)
			pages["ec2 describe-volumes"] = json.RawMessage(body)

			got := report(t, pages)
			if !strings.Contains(got, "EBS volumes, by state: unknown") ||
				strings.Contains(got, "secret") {
				t.Fatalf("invalid inventory accepted or leaked:\n%s", got)
			}
		})
	}
}

type paginationCase struct {
	Operation  string          `json:"operation"`
	First      json.RawMessage `json:"first"`
	Last       json.RawMessage `json:"last"`
	InputToken string          `json:"inputToken"`
	Want       string          `json:"want"`
}

func TestReportUsesNativePageTokensAndRetainsLaterResources(t *testing.T) {
	t.Parallel()

	var cases []paginationCase
	readFixture(t, "pagination", &cases)

	for _, test := range cases {
		t.Run(test.Operation, func(t *testing.T) {
			t.Parallel()
			pages := emptyPages(t)
			calls := 0

			got := executeReport(t, func(_ context.Context, args ...string) ([]byte, error) {
				if args[0]+" "+args[1] != test.Operation {
					return pages[args[0]+" "+args[1]], nil
				}

				calls++

				return paginationResponse(t, test, args[8], calls), nil
			})
			if calls != 2 || !strings.Contains(got, test.Want) ||
				strings.Contains(got, "private-") {
				t.Fatalf("pagination incomplete: calls=%d output=%s", calls, got)
			}
		})
	}
}

func paginationResponse(t *testing.T, test paginationCase, inputJSON string, calls int) []byte {
	t.Helper()

	var input map[string]any

	err := json.Unmarshal([]byte(inputJSON), &input)
	if err != nil {
		t.Fatal(err)
	}

	if calls == 1 {
		if _, found := input[test.InputToken]; found {
			t.Fatal("first page must have no token")
		}

		return test.First
	}

	if calls > 2 || input[test.InputToken] != "private-token" {
		t.Fatal("native paging token lost")
	}

	return test.Last
}

func TestReportNeverUsesPartialOutputFromFailedOrLoopingPages(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"denied", "loop", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			pages := emptyPages(t)
			calls := 0
			got := executeReport(t, func(_ context.Context, args ...string) ([]byte, error) {
				if args[0]+" "+args[1] != "ec2 describe-volumes" {
					return pages[args[0]+" "+args[1]], nil
				}

				calls++
				if calls == 1 || mode == "loop" {
					return []byte(`{"Volumes":[],"NextToken":"private-token"}`), nil
				}

				if mode == "deadline" {
					return []byte(`{"Volumes":[]}`), context.DeadlineExceeded
				}

				return []byte(
						`{"Volumes":[]}`,
					), fmt.Errorf(
						"private-account AccessDenied: %w",
						fs.ErrPermission,
					)
			})
			assertUnknownPage(t, got, calls)
		})
	}
}

func assertUnknownPage(t *testing.T, output string, calls int) {
	t.Helper()

	if calls != 2 || !strings.Contains(output, "EBS volumes, by state: unknown") ||
		strings.Contains(
			output,
			"EBS volumes, by state: 0",
		) || strings.Contains(output, "private-") {
		t.Fatalf("partial observation accepted: calls=%d output=%s", calls, output)
	}
}

func TestReportEmptyIsBoundedDiagnosticNotTeardownCertificate(t *testing.T) {
	t.Parallel()

	got := report(t, emptyPages(t))
	for _, want := range []string{
		"EKS clusters: 0", "CloudFormation stacks, by state: 0", "NAT gateways, by state: 0",
		"Elastic IP addresses: 0", "EBS volumes, by state: 0", "EC2 instant fleets: unknown",
		"not proof of a run's cleanup", "Running On-Demand Standard vCPUs = 5",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}
