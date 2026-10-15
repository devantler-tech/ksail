package eksinventory_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	inventory "github.com/devantler-tech/ksail/v7/internal/maintenance/eksinventory"
)

const capacityPlan = `{"apiVersion":"eksctl.io/v1alpha5",
"kind":"ClusterConfig",
"metadata":{"name":"fixture",
"region":"eu-central-1",
"version":"1.31"},
"iam":{"withOIDC":true},
"addons":[],
"managedNodeGroups":[{"name":"default",
"instanceType":"t3.medium",
"desiredCapacity":1,
"minSize":1,
"maxSize":1,
"volumeSize":20,
"amiFamily":"AmazonLinux2023"}]}`

type capacityFixture struct {
	quota        string
	instances    string
	reservations string
	types        string
	fail         string
	calls        []string
}

func newCapacityFixture() *capacityFixture {
	return &capacityFixture{
		quota:        `{"Quotas":[{"QuotaCode":"L-1216C47A","Value":4}]}`,
		instances:    `{"Reservations":[]}`,
		reservations: `{"CapacityReservations":[]}`,
		types:        `{"InstanceTypes":[{"InstanceType":"t3.medium","VCpuInfo":{"DefaultVCpus":2}}]}`,
	}
}

func (fixture *capacityFixture) query(_ context.Context, args ...string) ([]byte, error) {
	if len(args) != 10 {
		return nil, inventory.ErrInventory
	}

	fixture.calls = append(fixture.calls, args[0]+" "+args[1])
	if args[1] == fixture.fail {
		return []byte(`{"Quotas":[{"QuotaCode":"L-1216C47A","Value":99}]}`), fs.ErrPermission
	}

	switch args[1] {
	case "get-caller-identity":
		return []byte(`{"Account":"123456789012"}`), nil
	case "list-service-quotas":
		return []byte(fixture.quota), nil
	case "describe-instances":
		return fixture.instancePayload(args[8])
	case "describe-capacity-reservations":
		return []byte(fixture.reservations), nil
	case "describe-instance-types":
		return []byte(fixture.types), nil
	default:
		return nil, inventory.ErrInventory
	}
}

func instanceRows(rows string) string {
	return `{"Reservations":[{"OwnerId":"123456789012","Instances":[` + rows + `]}]}`
}

func instance(identity, kind, state, reservation string) string {
	association := ""
	if reservation != "" {
		association = `,"CapacityReservationId":"` + reservation + `"`
	}

	return fmt.Sprintf(
		`{"InstanceId":%q,
"InstanceType":%q,
"State":{"Name":%q},
"Placement":{"Tenancy":"default"},
"CpuOptions":{"CoreCount":1,
"ThreadsPerCore":1}%s}`,
		identity,
		kind,
		state,
		association,
	)
}

func reservation(state string, total, available int) string {
	return fmt.Sprintf(
		`{"CapacityReservations":[{"CapacityReservationId":"cr-fixture",
"OwnerId":"123456789012",
"UnusedReservationBillingOwnerId":"999999999999",
"InstanceType":"t3.medium",
"State":%q,
"TotalInstanceCount":%d,
"AvailableInstanceCount":%d,
"ReservationType":"default",
"Tenancy":"default",
"Interruptible":false}]}`,
		state,
		total,
		available,
	)
}

func TestCapacityAdmissionUsesCompleteQuotaAccounting(t *testing.T) {
	t.Parallel()

	for _, test := range capacityAccountingCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newCapacityFixture()
			test.change(fixture)

			var out bytes.Buffer

			err := inventory.CheckCapacity(
				t.Context(),
				"eu-central-1",
				"fixture", 1,
				[]byte(capacityPlan),
				&out,
				fixture.query,
			)
			if (err == nil) != test.admitted {
				t.Fatalf(
					"admission=%v want=%v output=%s err=%v",
					err == nil,
					test.admitted,
					out.String(),
					err,
				)
			}

			if !strings.Contains(out.String(), test.want) {
				t.Fatalf("missing %q in %s", test.want, out.String())
			}

			for _, s := range []string{"123456789012", "999999999999", "i-first", "cr-fixture"} {
				if strings.Contains(out.String(), s) {
					t.Fatalf("private value leaked: %s", s)
				}
			}
		})
	}
}

func TestCapacityReservationStates(t *testing.T) {
	t.Parallel()

	for _, state := range []string{"assessing", "scheduled", "pending", "active", "delayed"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()

			fixture := newCapacityFixture()
			fixture.reservations = reservation(state, 1, 1)

			var out bytes.Buffer

			err := inventory.CheckCapacity(
				t.Context(),
				"eu-central-1",
				"fixture", 1,
				[]byte(capacityPlan),
				&out,
				fixture.query,
			)
			if err == nil ||
				!strings.Contains(out.String(), "usage=2") {
				t.Fatalf("reservation not charged: %s", out.String())
			}
		})
	}
}

func TestCapacityUnknownNeverAdmits(t *testing.T) {
	t.Parallel()

	for _, test := range capacityUnknownCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newCapacityFixture()
			test.change(fixture)

			var out bytes.Buffer

			err := inventory.CheckCapacity(
				t.Context(),
				"eu-central-1",
				"fixture", 1,
				[]byte(capacityPlan),
				&out,
				fixture.query,
			)
			if err == nil || !strings.Contains(out.String(), "unknown") {
				t.Fatalf("incomplete read accepted: %s err=%v", out.String(), err)
			}

			if strings.Contains(out.String(), "admitted") {
				t.Fatal("unknown printed admission")
			}
		})
	}
}

func TestCapacityUnsupportedPlanNeverReadsAWS(t *testing.T) {
	t.Parallel()

	for _, plan := range []string{
		strings.Replace(capacityPlan, `"region":"eu-central-1"`, `"region":"us-east-1"`, 1),
		strings.Replace(capacityPlan, `"instanceType":"t3.medium"`, `"instanceTypes":["t3.medium"]`, 1),
		strings.Replace(capacityPlan, `"instanceType":"t3.medium"`, `"instanceType":"t3.medium","spot":true`, 1),
		strings.Replace(capacityPlan, `"instanceType":"t3.medium"`, `"instanceType":"t3.medium",
"launchTemplate":{"id":"lt-private"}`, 1),
		strings.Replace(capacityPlan, `"maxSize":1`, `"maxSize":9`, 1),
		strings.Replace(capacityPlan, `"addons":[]`, `"addons":[],"nodeGroups":[{}]`, 1),
		capacityPlan + capacityPlan,
	} {
		t.Run(plan, func(t *testing.T) {
			t.Parallel()

			fixture := newCapacityFixture()

			var out bytes.Buffer

			err := inventory.CheckCapacity(
				t.Context(),
				"eu-central-1",
				"fixture", 1,
				[]byte(plan),
				&out,
				fixture.query,
			)
			if err == nil {
				t.Fatal("unsupported plan accepted")
			}

			if len(fixture.calls) > 0 {
				t.Fatal("unsupported plan reached AWS")
			}
		})
	}
}

func TestCapacityFiveVCPUCeiling(t *testing.T) {
	t.Parallel()

	fixture := newCapacityFixture()
	fixture.types = `{"InstanceTypes":[{"InstanceType":"t3.medium","VCpuInfo":{"DefaultVCpus":8}}]}`
	fixture.quota = `{"Quotas":[{"QuotaCode":"L-1216C47A","Value":100}]}`

	var out bytes.Buffer

	err := inventory.CheckCapacity(
		t.Context(),
		"us-east-1",
		"fixture", 1,
		[]byte(strings.ReplaceAll(capacityPlan, "eu-central-1", "us-east-1")),
		&out,
		fixture.query,
	)
	if err == nil ||
		!strings.Contains(out.String(), "trial-ceiling") {
		t.Fatalf("ceiling not enforced: %s", out.String())
	}
}

func TestCapacityPaginationCannotUsePartialHeadroom(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"second-page-usage", "later-denial", "repeated-token", "wrong-envelope"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			fixture := newCapacityFixture()

			var out bytes.Buffer

			err := inventory.CheckCapacity(
				t.Context(),
				"eu-central-1",
				"fixture",
				1,
				[]byte(capacityPlan),
				&out,
				capacityPaginatedQuery(fixture, mode),
			)
			if err == nil {
				t.Fatalf("partial read admitted: %s", out.String())
			}

			want := "unknown"
			if mode == "second-page-usage" {
				want = "usage=2"
			}

			if !strings.Contains(out.String(), want) ||
				strings.Contains(out.String(), "private-token") {
				t.Fatalf("incorrect verdict: %s", out.String())
			}
		})
	}
}

func capacityPaginatedQuery(fixture *capacityFixture, mode string) inventory.Query {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		if args[1] != "describe-instances" {
			return fixture.query(ctx, args...)
		}

		var input map[string]any

		err := json.Unmarshal([]byte(args[8]), &input)
		if err != nil {
			return nil, fmt.Errorf("fixture input: %w", err)
		}

		return capacityInstancePage(mode, input)
	}
}

func capacityInstancePage(mode string, input map[string]any) ([]byte, error) {
	if input["NextToken"] == nil {
		if mode == "wrong-envelope" {
			return []byte(`{"Reservations":[],"nextToken":"private-token"}`), nil
		}

		return []byte(`{"Reservations":[],"NextToken":"private-token"}`), nil
	}

	if input["NextToken"] != "private-token" {
		return nil, inventory.ErrInventory
	}

	if mode == "later-denial" {
		return []byte(`{"Reservations":[]}`), fs.ErrPermission
	}

	if mode == "repeated-token" {
		return []byte(`{"Reservations":[],"NextToken":"private-token"}`), nil
	}

	return []byte(instanceRows(instance("i-later", "t3.medium", "running", ""))), nil
}

func TestCapacityCancelledObservationCannotAdmit(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	fixture := newCapacityFixture()

	var out bytes.Buffer

	err := inventory.CheckCapacity(
		ctx,
		"eu-central-1",
		"fixture", 1,
		[]byte(capacityPlan),
		&out,
		fixture.query,
	)
	if err == nil ||
		!strings.Contains(out.String(), "unknown (deadline)") {
		t.Fatalf("cancelled observation accepted: %s", out.String())
	}
}

func TestCapacityCurrentSmokeIncludesAutoScalingSurge(t *testing.T) {
	t.Parallel()

	fixture := newCapacityFixture()
	fixture.quota = `{"Quotas":[{"QuotaCode":"L-1216C47A","Value":16}]}`

	var output bytes.Buffer

	err := inventory.CheckCapacity(
		t.Context(),
		"eu-central-1",
		"fixture",
		2,
		[]byte(capacityPlan),
		&output,
		fixture.query,
	)
	if err != nil || !strings.Contains(output.String(), "admitted quota=16") ||
		!strings.Contains(output.String(), "target=4 surge=2 required=6 trial-ceiling=16") {
		t.Fatalf("two-node candidate ignored AWS surge: %s", output.String())
	}
}

func TestCapacityFrankfurtBudgetPreservesRegionalAndLiveLimits(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name, region, verdict, totals string
		peak, quota                   int
		reserved, pending             bool
		admitted                      bool
	}{
		{
			"exact Frankfurt envelope", "eu-central-1", "admitted",
			"target=14 surge=2 required=16 trial-ceiling=16", 7, 16, false, false, true,
		},
		{
			"above Frankfurt budget", "eu-central-1", "refused (trial-ceiling)",
			"target=16 surge=2 required=18 trial-ceiling=16", 8, 100, false, false, false,
		},
		{
			"lower applied quota", "eu-central-1", "refused (insufficient-headroom)",
			"target=4 surge=2 required=6 trial-ceiling=16", 2, 5, false, false, false,
		},
		{
			"reservation consumes headroom", "eu-central-1", "refused (insufficient-headroom)",
			"target=4 surge=2 required=6 trial-ceiling=16", 2, 16, true, false, false,
		},
		{
			"pending launch consumes buffer", "eu-central-1", "refused (insufficient-headroom)",
			"target=14 surge=2 required=16 trial-ceiling=16", 7, 16, false, true, false,
		},
		{
			"US budget unchanged", "us-east-1", "refused (trial-ceiling)",
			"target=4 surge=2 required=6 trial-ceiling=5", 2, 100, false, false, false,
		},
		{
			"other EU region unchanged", "eu-west-1", "refused (trial-ceiling)",
			"target=4 surge=2 required=6 trial-ceiling=5", 2, 100, false, false, false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newCapacityFixture()

			fixture.quota = fmt.Sprintf(
				`{"Quotas":[{"QuotaCode":"L-1216C47A","Value":%d}]}`,
				test.quota,
			)
			if test.reserved {
				fixture.reservations = reservation("active", 6, 6)
			}

			if test.pending {
				fixture.instances = instanceRows(instance("i-pending", "t3.medium", "pending", ""))
			}

			var output bytes.Buffer

			err := inventory.CheckCapacity(
				t.Context(), test.region, "fixture", test.peak,
				[]byte(strings.ReplaceAll(capacityPlan, "eu-central-1", test.region)),
				&output, fixture.query,
			)
			if (err == nil) != test.admitted || !strings.Contains(output.String(), test.verdict) ||
				!strings.Contains(output.String(), test.totals) {
				t.Fatalf("regional admission mismatch: error=%v output=%s", err, output.String())
			}
		})
	}
}

func (fixture *capacityFixture) instancePayload(inputJSON string) ([]byte, error) {
	var input map[string]any

	err := json.Unmarshal([]byte(inputJSON), &input)
	if err != nil {
		return nil, fmt.Errorf("fixture input: %w", err)
	}

	if input["IncludeManagedResources"] != true || input["Filters"] != nil {
		return nil, inventory.ErrInventory
	}

	return []byte(fixture.instances), nil
}

func capacityAccountingCases() []capacityAccountingCase {
	return append(
		append(capacityAccountingCases0(), capacityAccountingCases1()...),
		capacityAccountingCases2()...)
}

func capacityAccountingCases0() []capacityAccountingCase {
	return []capacityAccountingCase{
		{
			"exact headroom",
			func(*capacityFixture) {},
			"quota=4 usage=0 pending-buffer=0 target=2 surge=2 required=4",
			true,
		},
		{
			"zero quota",
			func(fixture *capacityFixture) { fixture.quota = `{"Quotas":[{"QuotaCode":"L-1216C47A","Value":0}]}` },
			"insufficient-headroom",
			false,
		},
		{"occupied reservation counted once", func(fixture *capacityFixture) {
			fixture.quota = `{"Quotas":[{"QuotaCode":"L-1216C47A","Value":6}]}`
			fixture.reservations = reservation("active", 1, 0)
			fixture.instances = instanceRows(
				instance("i-first", "t3.medium", "running", "cr-fixture"),
			)
		}, "quota=6 usage=2 pending-buffer=0 target=2 surge=2 required=4", true},
	}
}

func capacityAccountingCases1() []capacityAccountingCase {
	return []capacityAccountingCase{
		{"shared out reservation remains full", func(fixture *capacityFixture) {
			fixture.quota = `{"Quotas":[{"QuotaCode":"L-1216C47A","Value":7}]}`
			fixture.reservations = reservation("active", 2, 0)
		}, "usage=4", false},
		{
			"unused reservation",
			func(fixture *capacityFixture) { fixture.reservations = reservation("active", 1, 1) },
			"usage=2",
			false,
		},
		{"stopped does not consume instance quota", func(fixture *capacityFixture) {
			fixture.instances = instanceRows(instance("i-first", "t3.medium", "stopped", ""))
		}, "usage=0 pending-buffer=0", true},
	}
}

func capacityAccountingCases2() []capacityAccountingCase {
	return []capacityAccountingCase{
		{"pending is separately buffered", func(fixture *capacityFixture) {
			fixture.instances = instanceRows(instance("i-first", "t3.medium", "pending", ""))
		}, "usage=0 pending-buffer=2", false},
		{"CPU optimization cannot reduce usage", func(fixture *capacityFixture) {
			fixture.instances = instanceRows(instance("i-first", "m5.2xlarge", "running", ""))
			fixture.types = `{"InstanceTypes":[{"InstanceType":"t3.medium",
"VCpuInfo":{"DefaultVCpus":2}},
{"InstanceType":"m5.2xlarge",
"VCpuInfo":{"DefaultVCpus":8}}]}`
		}, "usage=8", false},
		{"mixed running types", func(fixture *capacityFixture) {
			fixture.instances = instanceRows(
				instance(
					"i-first",
					"t3.medium",
					"running",
					"",
				) + "," + instance(
					"i-second",
					"m5.large",
					"running",
					"",
				),
			)
			fixture.types = `{"InstanceTypes":[{"InstanceType":"t3.medium",
"VCpuInfo":{"DefaultVCpus":2}},
{"InstanceType":"m5.large",
"VCpuInfo":{"DefaultVCpus":2}}]}`
		}, "usage=4", false},
	}
}

func capacityUnknownCases() []capacityUnknownCase {
	return append(
		append(
			append(
				append(
					append(
						append(capacityUnknownCases0(), capacityUnknownCases1()...),
						capacityUnknownCases2()...),
					capacityUnknownCases3()...),
				capacityUnknownCases4()...),
			capacityUnknownCases5()...),
		capacityUnknownCases6()...)
}

func capacityUnknownCases0() []capacityUnknownCase {
	return []capacityUnknownCase{
		{"missing quota", func(fixture *capacityFixture) { fixture.quota = `{"Quotas":[]}` }},
		{
			"null inventory",
			func(fixture *capacityFixture) { fixture.instances = `{"Reservations":null}` },
		},
		{"missing collection", func(fixture *capacityFixture) { fixture.reservations = `{}` }},
	}
}

func capacityUnknownCases1() []capacityUnknownCase {
	return []capacityUnknownCase{
		{
			"partial stdout and failure",
			func(fixture *capacityFixture) { fixture.fail = "list-service-quotas" },
		},
		{
			"malformed quota number",
			func(fixture *capacityFixture) { fixture.quota = `{"Quotas":[{"QuotaCode":"L-1216C47A","Value":"4"}]}` },
		},
		{
			"fractional quota",
			func(fixture *capacityFixture) { fixture.quota = `{"Quotas":[{"QuotaCode":"L-1216C47A","Value":4.5}]}` },
		},
	}
}

func capacityUnknownCases2() []capacityUnknownCase {
	return []capacityUnknownCase{
		{"duplicate JSON quota", func(fixture *capacityFixture) {
			fixture.quota = `{"Quotas":[],"Quotas":[{"QuotaCode":"L-1216C47A","Value":99}]}`
		}},
		{"duplicate instance", func(fixture *capacityFixture) {
			fixture.instances = instanceRows(
				instance(
					"i-first",
					"t3.medium",
					"running",
					"",
				) + "," + instance(
					"i-first",
					"t3.medium",
					"running",
					"",
				),
			)
		}},
		{"unknown shutdown", func(fixture *capacityFixture) {
			fixture.instances = instanceRows(instance("i-first", "t3.medium", "shutting-down", ""))
		}},
	}
}

func capacityUnknownCases3() []capacityUnknownCase {
	return []capacityUnknownCase{
		{"foreign instance owner", func(fixture *capacityFixture) {
			fixture.instances = strings.Replace(
				instanceRows(instance("i-first", "t3.medium", "running", "")),
				"123456789012",
				"999999999999",
				1,
			)
		}},
		{"unresolved reservation", func(fixture *capacityFixture) {
			fixture.instances = instanceRows(
				instance("i-first", "t3.medium", "running", "cr-missing"),
			)
		}},
		{"wrong occupant type", func(fixture *capacityFixture) {
			fixture.reservations = reservation("active", 1, 0)
			fixture.instances = instanceRows(
				instance("i-first", "m5.large", "running", "cr-fixture"),
			)
		}},
	}
}

func capacityUnknownCases4() []capacityUnknownCase {
	return []capacityUnknownCase{
		{"foreign reservation", func(fixture *capacityFixture) {
			fixture.reservations = strings.Replace(
				reservation("active", 1, 1),
				"123456789012",
				"999999999999",
				1,
			)
		}},
		{"special reservation", func(fixture *capacityFixture) {
			fixture.reservations = strings.Replace(
				reservation("active", 1, 1),
				`"ReservationType":"default"`,
				`"ReservationType":"capacity-block"`,
				1,
			)
		}},
		{"interruptible reservation", func(fixture *capacityFixture) {
			fixture.reservations = strings.Replace(
				reservation("active", 1, 1),
				`"Interruptible":false`,
				`"Interruptible":true`,
				1,
			)
		}},
	}
}

func capacityUnknownCases5() []capacityUnknownCase {
	return []capacityUnknownCase{
		{"inconsistent reservation", func(fixture *capacityFixture) {
			fixture.reservations = reservation("active", 1, 1)
			fixture.instances = instanceRows(
				instance("i-first", "t3.medium", "running", "cr-fixture"),
			)
		}},
		{"unknown type", func(fixture *capacityFixture) {
			fixture.instances = instanceRows(instance("i-first", "future1.large", "running", ""))
		}},
		{"HPC separate quota", func(fixture *capacityFixture) {
			fixture.instances = instanceRows(instance("i-first", "hpc7a.96xlarge", "running", ""))
		}},
	}
}

func capacityUnknownCases6() []capacityUnknownCase {
	return []capacityUnknownCase{
		{"Inf separate quota", func(fixture *capacityFixture) {
			fixture.instances = instanceRows(instance("i-first", "inf2.xlarge", "running", ""))
		}},
		{
			"missing type metadata",
			func(fixture *capacityFixture) { fixture.types = `{"InstanceTypes":[]}` },
		},
		{"string CPU count", func(fixture *capacityFixture) {
			fixture.types = `{"InstanceTypes":[{"InstanceType":"t3.medium","VCpuInfo":{"DefaultVCpus":"2"}}]}`
		}},
	}
}

type capacityAccountingCase struct {
	name     string
	change   func(*capacityFixture)
	want     string
	admitted bool
}
type capacityUnknownCase struct {
	name   string
	change func(*capacityFixture)
}
