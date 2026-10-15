package eksinventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"regexp"
	"slices"
	"sort"
)

// ErrCapacityRejected marks an observed quota or trial ceiling that cannot admit the plan.
var ErrCapacityRejected = errors.New("EKS capacity refused")

const (
	defaultTrialVCPUCeiling   = 5
	frankfurtTrialVCPUCeiling = 16
	maxCapacityTypes          = 100
	rebalanceDivisor          = 10
	maxJSONDepth              = 64
	capacityDefault           = "default"
	capacityPending           = "pending"
	capacityRunning           = "running"
	describeInstances         = "describe-instances"
)

type capacityTotals struct{ quota, usage, pending, target, surge, required int }

// CheckCapacity reads complete regional observations before admitting an EKS smoke plan.
// Config is one JSON eksctl document, converted from the generated YAML by the caller.
// peakNodes must equal the largest node-group maxSize used by the smoke scenario.
// The caller must keep every later scaling step within that bound.
// A nil query uses the bounded, read-only AWS CLI boundary. Every incomplete observation
// returns an error and prints unknown. Admission is neither a reservation nor trial authorization.
func CheckCapacity(
	ctx context.Context,
	region, cluster string,
	peakNodes int,
	config []byte,
	output io.Writer,
	query Query,
) error {
	kind, err := capacityPlanType(config, region, cluster, peakNodes)
	if err != nil {
		return capacityUnknown(output, err)
	}

	if query == nil {
		query = awsQuery
	}

	totals, err := capacityObservation(ctx, region, kind, peakNodes, query)
	if err != nil {
		return capacityUnknown(output, err)
	}

	trialCeiling := defaultTrialVCPUCeiling
	if region == "eu-central-1" {
		trialCeiling = frankfurtTrialVCPUCeiling
	}

	verdict := "admitted"
	if totals.required > trialCeiling {
		verdict = "refused (trial-ceiling)"
	} else if totals.quota-totals.usage-totals.pending < totals.required {
		verdict = "refused (insufficient-headroom)"
	}

	_, err = fmt.Fprintf(
		output,
		"EKS capacity (%s): %s quota=%d usage=%d pending-buffer=%d "+
			"target=%d surge=%d required=%d trial-ceiling=%d vCPUs\n"+
			"Point-in-time quota check only; cleanup and trial authorization remain separate.\n",
		region,
		verdict,
		totals.quota,
		totals.usage,
		totals.pending,
		totals.target,
		totals.surge,
		totals.required,
		trialCeiling,
	)
	if err != nil {
		return fmt.Errorf("write capacity verdict: %w", err)
	}

	if verdict != "admitted" {
		return ErrCapacityRejected
	}

	return nil
}

func capacityUnknown(output io.Writer, cause error) error {
	_, err := fmt.Fprintf(
		output,
		"EKS capacity: unknown (%s); creation refused\n",
		failureReason(cause),
	)
	if err != nil {
		return fmt.Errorf("write capacity failure: %w", err)
	}

	return fmt.Errorf("capacity observation incomplete: %w", ErrInventory)
}

func capacityObservation(
	ctx context.Context,
	region, plannedType string,
	peakNodes int,
	query Query,
) (capacityTotals, error) {
	totals, instances, reservations, err := capacitySnapshot(ctx, region, query)
	if err != nil {
		return totals, err
	}

	types := map[string]bool{plannedType: true}
	for _, instance := range instances {
		types[instance.kind] = true
	}

	for _, reservation := range reservations {
		types[reservation.kind] = true
	}

	cpus, err := capacityCPUs(ctx, region, types, query)
	if err != nil {
		return totals, err
	}

	totals.target = cpus[plannedType] * peakNodes
	// AWS AZ rebalancing may exceed max by ten percent or one instance.
	// Round upward so a fractional margin never understates demand.
	totals.surge = cpus[plannedType] * max(1, (peakNodes+rebalanceDivisor-1)/rebalanceDivisor)
	totals.required = totals.target + totals.surge
	totals.usage, totals.pending, err = capacityUsage(instances, reservations, cpus)

	return totals, err
}

func capacitySnapshot(
	ctx context.Context,
	region string,
	query Query,
) (capacityTotals, []capacityInstance, map[string]capacityReservation, error) {
	var totals capacityTotals

	account, err := capacityAccount(ctx, region, query)
	if err != nil {
		return totals, nil, nil, err
	}

	quotaRows, err := capacityPages(
		ctx,
		region,
		operation{
			service:     "service-quotas",
			command:     "list-service-quotas",
			collection:  "Quotas",
			inputToken:  nextTokenKey,
			outputToken: nextTokenKey,
			pageSize:    maxResultsKey,
		},
		map[string]any{"ServiceCode": ec2Service},
		query,
	)
	if err != nil {
		return totals, nil, nil, err
	}

	totals.quota, err = capacityQuota(quotaRows)
	if err != nil {
		return totals, nil, nil, err
	}

	instances, reservations, err := capacityResourceSnapshot(ctx, region, account, query)

	return totals, instances, reservations, err
}

func capacityResourceSnapshot(
	ctx context.Context,
	region, account string,
	query Query,
) ([]capacityInstance, map[string]capacityReservation, error) {
	instanceRows, err := capacityPages(
		ctx,
		region,
		operation{
			service:     ec2Service,
			command:     describeInstances,
			collection:  "Reservations",
			inputToken:  nextTokenKey,
			outputToken: nextTokenKey,
			pageSize:    maxResultsKey,
		},
		map[string]any{"IncludeManagedResources": true},
		query,
	)
	if err != nil {
		return nil, nil, err
	}

	reservationRows, err := capacityPages(
		ctx,
		region,
		operation{
			service:     ec2Service,
			command:     "describe-capacity-reservations",
			collection:  "CapacityReservations",
			inputToken:  nextTokenKey,
			outputToken: nextTokenKey,
			pageSize:    maxResultsKey,
		},
		nil,
		query,
	)
	if err != nil {
		return nil, nil, err
	}

	instances, err := capacityInstances(instanceRows, account)
	if err != nil {
		return nil, nil, err
	}

	reservations, err := capacityReservations(reservationRows, account)

	return instances, reservations, err
}

func capacityAccount(ctx context.Context, region string, query Query) (string, error) {
	identity, err := queryPage(
		ctx,
		region,
		operation{service: "sts", command: "get-caller-identity"},
		map[string]any{},
		query,
	)
	if err != nil {
		return "", err
	}

	err = uniqueJSON(identity)
	if err != nil {
		return "", err
	}

	account, err := stringField(identity, "Account")
	if err != nil || !regexp.MustCompile("^[0-9]{12}$").MatchString(account) {
		return "", ErrInventory
	}

	return account, nil
}

func capacityPages(
	ctx context.Context,
	region string,
	spec operation,
	extra map[string]any,
	query Query,
) ([]json.RawMessage, error) {
	var result []json.RawMessage

	tokens := map[string]bool{}
	token := ""

	for range maxPages {
		input := map[string]any{}
		maps.Copy(input, extra)

		if spec.pageSize != "" {
			input[spec.pageSize] = maxCapacityTypes
		}

		if token != "" {
			input[spec.inputToken] = token
		}

		rows, next, err := capacityPage(ctx, region, spec, input, query)
		if err != nil {
			return nil, err
		}

		if len(result)+len(rows) > maxRecords {
			return nil, ErrInventory
		}

		result = append(result, rows...)
		if next == "" {
			return result, nil
		}

		if tokens[next] {
			return nil, ErrInventory
		}

		tokens[next] = true
		token = next
	}

	return nil, ErrInventory
}

func capacityPage(
	ctx context.Context,
	region string,
	spec operation,
	input map[string]any,
	query Query,
) ([]json.RawMessage, string, error) {
	payload, err := queryPage(ctx, region, spec, input, query)
	if err != nil {
		return nil, "", err
	}

	err = uniqueJSON(payload)
	if err != nil {
		return nil, "", err
	}

	rows, err := records(payload, []string{spec.collection})
	if err != nil {
		return nil, "", err
	}

	next, err := nextToken(payload, spec.outputToken)

	return rows, next, err
}

func capacityQuota(rows []json.RawMessage) (int, error) {
	codes := map[string]bool{}
	quota := -1

	for _, row := range rows {
		code, err := stringField(row, "QuotaCode")
		if err != nil || codes[code] {
			return 0, ErrInventory
		}

		codes[code] = true
		if code != "L-1216C47A" {
			continue
		}

		quota, err = capacityQuotaValue(row)
		if err != nil {
			return 0, err
		}
	}

	if quota < 0 {
		return 0, ErrInventory
	}

	return quota, nil
}

func capacityQuotaValue(row json.RawMessage) (int, error) {
	raw, err := field(row, "Value")
	if err != nil {
		return 0, err
	}

	var value *float64

	err = json.Unmarshal(raw, &value)
	if err != nil || value == nil {
		return 0, ErrInventory
	}

	if *value < 0 || *value > 1e9 || math.Trunc(*value) != *value {
		return 0, ErrInventory
	}

	return int(*value), nil
}

func capacityCPUs(
	ctx context.Context,
	region string,
	types map[string]bool,
	query Query,
) (map[string]int, error) {
	if len(types) > maxCapacityTypes {
		return nil, ErrInventory
	}

	names := make([]string, 0, len(types))
	for kind := range types {
		if !standardType(kind) {
			return nil, ErrInventory
		}

		names = append(names, kind)
	}

	sort.Strings(names)

	rows, err := capacityPages(
		ctx,
		region,
		operation{
			service:     ec2Service,
			command:     "describe-instance-types",
			collection:  "InstanceTypes",
			inputToken:  nextTokenKey,
			outputToken: nextTokenKey,
		},
		map[string]any{"InstanceTypes": names},
		query,
	)
	if err != nil {
		return nil, err
	}

	return capacityTypeMetadata(rows, types)
}

func capacityTypeMetadata(rows []json.RawMessage, types map[string]bool) (map[string]int, error) {
	cpus := map[string]int{}

	for _, row := range rows {
		kind, err := stringField(row, "InstanceType")
		if err != nil || !types[kind] || cpus[kind] != 0 {
			return nil, ErrInventory
		}

		count, err := capacityInteger(row, "VCpuInfo.DefaultVCpus", 1)
		if err != nil || count > maxRecords {
			return nil, ErrInventory
		}

		cpus[kind] = count
	}

	if len(cpus) != len(types) {
		return nil, ErrInventory
	}

	return cpus, nil
}

func standardType(kind string) bool {
	// A numeric generation after a Standard family letter excludes HPC, Inf, Trn and DL.
	return regexp.MustCompile(`^[acdhimrtz][1-9][0-9]*[a-z0-9-]*\.[a-z0-9]+$`).MatchString(kind)
}

func capacityInteger(row json.RawMessage, key string, minimum int) (int, error) {
	raw, err := field(row, key)
	if err != nil {
		return 0, err
	}

	var number *int

	err = json.Unmarshal(raw, &number)
	if err != nil || number == nil {
		return 0, ErrInventory
	}

	if *number < minimum || *number > 1000000000 {
		return 0, ErrInventory
	}

	return *number, nil
}

func capacityPlanType(config []byte, region, cluster string, peakNodes int) (string, error) {
	if !capacityPlanInputs(region, cluster, peakNodes, len(config)) {
		return "", ErrInventory
	}

	err := uniqueJSON(config)
	if err != nil {
		return "", err
	}

	err = capacityAllowedFields(
		config,
		[]string{"apiVersion", "kind", "metadata", "iam", "addons", "managedNodeGroups"},
	)
	if err != nil {
		return "", err
	}

	err = capacityMatchStrings(
		config,
		map[string]string{
			"apiVersion":      "eksctl.io/v1alpha5",
			"kind":            "ClusterConfig",
			"metadata.name":   cluster,
			"metadata.region": region,
		},
	)
	if err != nil {
		return "", err
	}

	groups, err := records(config, []string{"managedNodeGroups"})
	if err != nil || len(groups) != 1 {
		return "", ErrInventory
	}

	return capacityGroupType(groups[0])
}

func capacityAllowedFields(data json.RawMessage, allowed []string) error {
	var object map[string]json.RawMessage

	err := json.Unmarshal(data, &object)
	if err != nil || object == nil {
		return ErrInventory
	}

	for key := range object {
		if !slices.Contains(allowed, key) {
			return ErrInventory
		}
	}

	return nil
}

func capacityMatchStrings(data json.RawMessage, expected map[string]string) error {
	for key, want := range expected {
		actual, err := stringField(data, key)
		if err != nil || actual != want {
			return ErrInventory
		}
	}

	return nil
}

func capacityGroupType(groupData json.RawMessage) (string, error) {
	err := capacityAllowedFields(
		groupData,
		[]string{
			"name",
			"instanceType",
			"desiredCapacity",
			"minSize",
			"maxSize",
			"volumeSize",
			"amiFamily",
			"iam",
		},
	)
	if err != nil {
		return "", err
	}

	_, err = stringField(groupData, "name")
	if err != nil {
		return "", err
	}

	err = capacityInitialSizes(groupData)
	if err != nil {
		return "", err
	}

	instanceType, err := stringField(groupData, "instanceType")
	if err != nil || !standardType(instanceType) {
		return "", ErrInventory
	}

	return instanceType, nil
}

func capacityInitialSizes(data json.RawMessage) error {
	for _, key := range []string{"desiredCapacity", "minSize", "maxSize"} {
		count, err := capacityInteger(data, key, 1)
		if err != nil || count != 1 {
			return ErrInventory
		}
	}

	return nil
}

func capacityPlanInputs(region, cluster string, peakNodes, size int) bool {
	return size <= maxResponseBytes && peakNodes >= 1 && peakNodes <= maxCapacityTypes &&
		cluster != "" &&
		regexp.MustCompile("^[a-z]{2}(-[a-z]+)+-[0-9]+$").MatchString(region)
}
