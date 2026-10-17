// Package eksinventory produces read-only, regional AWS resource diagnostics.
// It does not establish resource ownership or certify a cluster's teardown.
package eksinventory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrInventory marks an incomplete or invalid AWS observation.
var ErrInventory = errors.New("incomplete AWS inventory")

var errAWSRead = errors.New("AWS read failed")

var errResourceNotFound = errors.New("AWS resource not found")

var errEvidenceLimit = errors.New("baseline evidence limit")

const (
	awsArgumentCount               = 10
	maxGroups                      = 40
	nextTokenKey                   = "NextToken"
	eksTokenKey                    = "nextToken"
	nextMarkerKey                  = "NextMarker"
	maxResultsKey                  = "MaxResults"
	markerKey                      = "Marker"
	stateField                     = "State"
	terminatedState                = "terminated"
	failedState                    = "failed"
	deletedState                   = "deleted"
	notFoundReason                 = "not-found"
	invalidDataReason              = "invalid-or-incomplete-data"
	exactRelationships             = "exact-relationships"
	instantFleetsFamily            = "instantFleets"
	includeManagedResources        = "IncludeManagedResources"
	reservationsInstances          = "Reservations.Instances"
	instanceIDField                = "InstanceId"
	autoScalingGroupsCollection    = "AutoScalingGroups"
	autoScalingService             = "autoscaling"
	describeAutoScalingGroups      = "describe-auto-scaling-groups"
	describeFleets                 = "describe-fleets"
	describeNatGateways            = "describe-nat-gateways"
	natGatewaysCollection          = "NatGateways"
	natGatewayIDField              = "NatGatewayId"
	describeAddresses              = "describe-addresses"
	describeNetworkInterfaces      = "describe-network-interfaces"
	networkInterfacesCollection    = "NetworkInterfaces"
	networkInterfaceIDField        = "NetworkInterfaceId"
	describeVolumes                = "describe-volumes"
	describeLoadBalancers          = "describe-load-balancers"
	classicLoadBalancersCollection = "LoadBalancerDescriptions"
	classicLoadBalancerNameField   = "LoadBalancerName"
	loadBalancersCollection        = "LoadBalancers"
	loadBalancerARNField           = "LoadBalancerArn"
	vpcIDField                     = "VpcId"
	describeNodegroup              = "describe-nodegroup"
	describeStacks                 = "describe-stacks"
	describeVPCs                   = "describe-vpcs"
	instanceKind                   = "instance"
	fleetKind                      = "fleet"
	addressKind                    = "address"
	autoScalingGroupKind           = "autoscaling-group"
	classicLoadBalancerKind        = "classic-load-balancer"
	loadBalancerKind               = "load-balancer"
	natGatewayKind                 = "nat-gateway"
	networkInterfaceKind           = "network-interface"
	stackKind                      = "stack"
	volumeKind                     = "volume"
	vpcKind                        = "vpc"
	nodegroupKind                  = "nodegroup"
	quotaKind                      = "quota"
	ec2Service                     = "ec2"
	eksService                     = "eks"
	elbService                     = "elb"
	elbv2Service                   = "elbv2"
	cloudFormationService          = "cloudformation"
	pageSizeKey                    = "PageSize"
	repositoryName                 = "devantler-tech/ksail"
	maxResponseBytes               = 4 << 20
	maxPages                       = 200
	maxRecords                     = 100000
	callTimeout                    = 30 * time.Second
	unlimitedIdentityBytes         = -1
)

// Query is the AWS CLI boundary; nil selects the installed aws executable.
type Query func(context.Context, ...string) ([]byte, error)

type operation struct {
	service, command, title, collection, identity string
	state, allowed, extra, kind                   string
	inputToken, outputToken, pageSize             string
}

type observation struct {
	groups         map[string]int
	identities     map[string]bool
	count          int
	identityBudget int
	identityBytes  int
}

// Report prints only complete per-family counts and validated aggregate groups.
// Provider failures are diagnostic unknowns; only invalid usage or output failure
// returns an error. In particular, a zero exit status is not cleanup evidence.
func Report(ctx context.Context, region string, output io.Writer, query Query) error {
	valid, _ := regexp.MatchString(`^[a-z]{2}(-[a-z]+)+-[0-9]+$`, region)
	if !valid {
		return fmt.Errorf("invalid AWS region: %w", ErrInventory)
	}

	if query == nil {
		query = awsQuery
	}

	_, headerErr := fmt.Fprintf(
		output,
		"EKS regional resource diagnostic (%s)\nDiagnostic only: not proof of a run's cleanup.\n",
		region,
	)
	if headerErr != nil {
		return fmt.Errorf("write diagnostic header: %w", headerErr)
	}

	for _, operationSpec := range operations() {
		observed, err := collect(ctx, region, operationSpec, query, unlimitedIdentityBytes)
		if err != nil {
			_, writeErr := fmt.Fprintf(
				output,
				"%s: unknown (%s)\n",
				operationSpec.title,
				failureReason(err),
			)
			if writeErr != nil {
				return fmt.Errorf("write unknown observation: %w", writeErr)
			}

			continue
		}

		groupErr := printGroups(output, operationSpec.title, observed.groups, observed.count)
		if groupErr != nil {
			return groupErr
		}
	}

	_, err := fmt.Fprintln(
		output,
		"EC2 instant fleets: unknown (regional listing requires exact fleet IDs)\n"+
			"Deleted stack and NAT records may be historical; counts do not establish run ownership.",
	)
	if err != nil {
		return fmt.Errorf("write coverage limitation: %w", err)
	}

	return nil
}

func collect(
	ctx context.Context,
	region string,
	operationSpec operation,
	query Query,
	identityBudget int,
) (observation, error) {
	observed := &observation{
		groups: make(
			map[string]int,
		),
		identities:     make(map[string]bool),
		identityBudget: identityBudget,
	}
	tokens := make(map[string]bool)
	token := ""

	for range maxPages {
		contextErr := ctx.Err()
		if contextErr != nil {
			return observation{}, fmt.Errorf("inventory deadline: %w", contextErr)
		}

		input := requestInput(operationSpec, token)

		payload, err := queryPage(ctx, region, operationSpec, input, query)
		if err != nil {
			return observation{}, err
		}

		rows, err := records(payload, strings.Split(operationSpec.collection, "."))
		if err != nil {
			return observation{}, err
		}

		added, err := addRows(operationSpec, rows, observed)
		if err != nil {
			return observation{}, err
		}

		observed.count += added

		next, err := nextToken(payload, operationSpec.outputToken)
		if err != nil {
			return observation{}, err
		}

		if next == "" {
			return *observed, nil
		}

		if tokens[next] {
			return observation{}, fmt.Errorf("repeated pagination token: %w", ErrInventory)
		}

		tokens[next] = true
		token = next
	}

	return observation{}, fmt.Errorf("pagination limit exceeded: %w", ErrInventory)
}

func requestInput(operationSpec operation, token string) map[string]any {
	input := make(map[string]any)
	if slices.Contains(
		[]string{describeInstances, describeNetworkInterfaces, describeVolumes},
		operationSpec.command,
	) {
		input[includeManagedResources] = true
	}

	if operationSpec.pageSize != "" {
		input[operationSpec.pageSize] = 100
	}

	if token != "" {
		input[operationSpec.inputToken] = token
	}

	if operationSpec.kind == quotaKind {
		input["ServiceCode"] = ec2Service
	}

	if operationSpec.kind == instanceKind {
		input["Filters"] = []map[string]any{
			{
				"Name": "instance-state-name",
				"Values": []string{
					capacityPending,
					capacityRunning,
					"stopping",
					"stopped",
					"shutting-down",
				},
			},
		}
	}

	return input
}

func queryPage(
	ctx context.Context,
	region string,
	operationSpec operation,
	input map[string]any,
	query Query,
) (json.RawMessage, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode inventory request: %w", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	data, err := query(
		callCtx,
		operationSpec.service,
		operationSpec.command,
		"--region",
		region,
		"--output",
		"json",
		"--no-paginate",
		"--cli-input-json",
		string(encoded),
		"--no-cli-pager",
	)
	if err != nil {
		return nil, fmt.Errorf("AWS read failed: %w", err)
	}

	if callCtx.Err() != nil {
		return nil, fmt.Errorf("AWS read exceeded deadline: %w", callCtx.Err())
	}

	if len(data) > maxResponseBytes || !json.Valid(data) {
		return nil, fmt.Errorf("invalid or excessive AWS response: %w", ErrInventory)
	}

	var object map[string]json.RawMessage

	decodeErr := json.Unmarshal(data, &object)
	if decodeErr != nil || object == nil {
		return nil, fmt.Errorf("missing AWS response object: %w", ErrInventory)
	}

	return json.RawMessage(data), nil
}

func records(data json.RawMessage, path []string) ([]json.RawMessage, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("empty resource path: %w", ErrInventory)
	}

	rows := []json.RawMessage{data}

	for _, key := range path {
		var next []json.RawMessage

		for _, row := range rows {
			children, err := collectionRecords(row, key)
			if err != nil {
				return nil, err
			}

			next = append(next, children...)
		}

		rows = next
	}

	return rows, nil
}

func collectionRecords(data json.RawMessage, key string) ([]json.RawMessage, error) {
	var object map[string]json.RawMessage

	err := json.Unmarshal(data, &object)
	if err != nil || object == nil {
		return nil, fmt.Errorf("invalid resource object: %w", ErrInventory)
	}

	raw, found := object[key]
	if !found || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("missing resource collection: %w", ErrInventory)
	}

	var rows []json.RawMessage

	err = json.Unmarshal(raw, &rows)
	if err != nil {
		return nil, fmt.Errorf("invalid resource collection: %w", ErrInventory)
	}

	return rows, nil
}

func field(data json.RawMessage, path string) (json.RawMessage, error) {
	value := data

	for key := range strings.SplitSeq(path, ".") {
		var object map[string]json.RawMessage

		err := json.Unmarshal(value, &object)
		if err != nil {
			return nil, fmt.Errorf("invalid resource field: %w", ErrInventory)
		}

		var found bool

		value, found = object[key]
		if !found {
			return nil, fmt.Errorf("missing resource field: %w", ErrInventory)
		}
	}

	return value, nil
}

func stringField(data json.RawMessage, path string) (string, error) {
	value, err := field(data, path)
	if err != nil {
		return "", err
	}

	return stringValue(value)
}

func stringValue(value json.RawMessage) (string, error) {
	var result string

	err := json.Unmarshal(
		value,
		&result,
	)
	if err != nil || result == "" || len(result) > 4096 ||
		strings.ContainsAny(result, "\r\n\x00") {
		return "", fmt.Errorf("invalid string field: %w", ErrInventory)
	}

	return result, nil
}

func classify(operationSpec operation, row json.RawMessage) (string, string, bool, error) {
	if operationSpec.kind == "names" {
		identity, err := stringValue(row)

		return identity, "", true, err
	}

	identity, err := stringField(row, operationSpec.identity)
	if err != nil {
		return "", "", false, err
	}

	group, include, err := grouping(operationSpec, row)

	return identity, group, include, err
}

func grouping(operationSpec operation, row json.RawMessage) (string, bool, error) {
	if operationSpec.kind == quotaKind {
		return quotaGroup(row)
	}

	if operationSpec.kind == "capacity" {
		return capacityGroup(row, operationSpec.state)
	}

	if operationSpec.state == "" {
		return "", true, nil
	}

	state, err := stringField(row, operationSpec.state)
	if err != nil {
		return "", false, err
	}

	if !slices.Contains(strings.Fields(operationSpec.allowed), state) {
		return "", false, fmt.Errorf("unrecognized resource state: %w", ErrInventory)
	}

	if operationSpec.kind == instanceKind && state == terminatedState {
		return "", false, nil
	}

	if operationSpec.extra == "" {
		return state, true, nil
	}

	return extraGroup(operationSpec, row, state)
}

func extraGroup(operationSpec operation, row json.RawMessage, state string) (string, bool, error) {
	extra, err := stringField(row, operationSpec.extra)
	if err != nil {
		return "", false, err
	}

	if operationSpec.kind == fleetKind &&
		!slices.Contains([]string{"instant", "request", "maintain"}, extra) {
		return "", false, fmt.Errorf("unrecognized fleet type: %w", ErrInventory)
	}

	if operationSpec.kind == instanceKind {
		valid, _ := regexp.MatchString(`^[a-z][a-z0-9-]{0,40}\.[a-z0-9]{1,30}$`, extra)
		if !valid {
			return "", false, fmt.Errorf("invalid instance type: %w", ErrInventory)
		}
	}

	return state + " " + extra, true, nil
}

func quotaGroup(row json.RawMessage) (string, bool, error) {
	label, err := quotaLabel(row)
	if err != nil || label == "" {
		return "", false, err
	}

	value, err := field(row, "Value")
	if err != nil {
		return "", false, err
	}

	number, err := strconv.ParseFloat(string(value), 64)
	if err != nil || number < 0 || math.IsNaN(number) || math.IsInf(number, 0) {
		return "", false, fmt.Errorf("invalid quota value: %w", ErrInventory)
	}

	return label + " = " + strconv.FormatFloat(number, 'f', -1, 64), true, nil
}

func nextToken(data json.RawMessage, key string) (string, error) {
	var object map[string]json.RawMessage

	err := json.Unmarshal(data, &object)
	if err != nil {
		return "", fmt.Errorf("invalid pagination envelope: %w", ErrInventory)
	}

	if unexpectedToken(object, key) {
		return "", fmt.Errorf("unexpected pagination field: %w", ErrInventory)
	}

	if key == "" {
		return "", nil
	}

	value, found := object[key]
	if !found || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return "", nil
	}

	var token string

	err = json.Unmarshal(value, &token)
	if err != nil || len(token) > 4096 {
		return "", fmt.Errorf("invalid pagination token: %w", ErrInventory)
	}

	return token, nil
}

func printGroups(output io.Writer, title string, groups map[string]int, count int) error {
	_, countErr := fmt.Fprintf(output, "%s: %d\n", title, count)
	if countErr != nil {
		return fmt.Errorf("write inventory count: %w", countErr)
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys[:min(len(keys), maxGroups)] {
		_, groupErr := fmt.Fprintf(output, "  %d %s\n", groups[key], key)
		if groupErr != nil {
			return fmt.Errorf("write inventory group: %w", groupErr)
		}
	}

	if len(keys) > maxGroups {
		_, limitErr := fmt.Fprintln(
			output,
			"  additional groups omitted; total includes every resource",
		)
		if limitErr != nil {
			return fmt.Errorf("write group limit: %w", limitErr)
		}
	}

	return nil
}

type boundedBuffer struct{ bytes.Buffer }

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	if buffer.Len()+len(data) > maxResponseBytes {
		return 0, fmt.Errorf("AWS response exceeds limit: %w", ErrInventory)
	}

	written, err := buffer.Buffer.Write(data)
	if err != nil {
		return written, fmt.Errorf("buffer AWS response: %w", err)
	}

	return written, nil
}

func awsQuery(ctx context.Context, args ...string) ([]byte, error) {
	if !validAWSRequest(args) {
		return nil, fmt.Errorf("unapproved AWS read request: %w", ErrInventory)
	}

	command := exec.CommandContext(ctx, "aws")
	command.Args = append(command.Args, args...)

	var (
		output     boundedBuffer
		diagnostic boundedBuffer
	)

	command.Stdout = &output
	command.Stderr = &diagnostic

	command.WaitDelay = time.Second

	err := command.Run()

	if ctx.Err() != nil {
		return nil, fmt.Errorf("AWS read interrupted: %w", ctx.Err())
	}

	if err != nil {
		if bytes.Contains(diagnostic.Bytes(), []byte("(AccessDenied)")) ||
			bytes.Contains(diagnostic.Bytes(), []byte("(AccessDeniedException)")) ||
			bytes.Contains(diagnostic.Bytes(), []byte("(UnauthorizedOperation)")) {
			return nil, fmt.Errorf("AWS read denied: %w", fs.ErrPermission)
		}

		if exactResourceNotFound(args[0], args[1], diagnostic.Bytes()) {
			return nil, fmt.Errorf("AWS resource not found: %w", errResourceNotFound)
		}

		return nil, fmt.Errorf("AWS CLI read failed: %w", errAWSRead)
	}

	return output.Bytes(), nil
}

func failureReason(err error) string {
	switch {
	case errors.Is(err, errEvidenceLimit):
		return "evidence-limit"
	case errors.Is(err, fs.ErrPermission):
		return "access-denied"
	case errors.Is(err, errResourceNotFound):
		return notFoundReason
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "deadline"
	case errors.Is(err, errAWSRead):
		return "read-failed"
	default:
		return invalidDataReason
	}
}

//nolint:cyclop // Every allow-listed exact read has a distinct provider error contract.
func exactResourceNotFound(service, command string, diagnostic []byte) bool {
	switch [2]string{service, command} {
	case [2]string{eksService, "describe-cluster"}:
		return bytes.Contains(diagnostic, []byte("(ResourceNotFoundException)")) &&
			bytes.Contains(diagnostic, []byte("DescribeCluster operation"))
	case [2]string{eksService, describeNodegroup}:
		return bytes.Contains(diagnostic, []byte("(ResourceNotFoundException)")) &&
			bytes.Contains(diagnostic, []byte("DescribeNodegroup operation"))
	case [2]string{cloudFormationService, describeStacks}:
		return bytes.Contains(diagnostic, []byte("(ValidationError)")) &&
			bytes.Contains(diagnostic, []byte("DescribeStacks operation")) &&
			bytes.Contains(diagnostic, []byte("does not exist"))
	case [2]string{ec2Service, describeInstances}:
		return bytes.Contains(diagnostic, []byte("(InvalidInstanceID.NotFound)")) &&
			bytes.Contains(diagnostic, []byte("DescribeInstances operation"))
	case [2]string{ec2Service, describeFleets}:
		return bytes.Contains(diagnostic, []byte("(InvalidFleetId.NotFound)")) &&
			bytes.Contains(diagnostic, []byte("DescribeFleets operation"))
	case [2]string{ec2Service, describeNatGateways}:
		return (bytes.Contains(diagnostic, []byte("(NatGatewayNotFound)")) ||
			bytes.Contains(diagnostic, []byte("(InvalidNatGatewayID.NotFound)"))) &&
			bytes.Contains(diagnostic, []byte("DescribeNatGateways operation"))
	case [2]string{ec2Service, describeAddresses}:
		return bytes.Contains(diagnostic, []byte("(InvalidAllocationID.NotFound)")) &&
			bytes.Contains(diagnostic, []byte("DescribeAddresses operation"))
	case [2]string{ec2Service, describeNetworkInterfaces}:
		return bytes.Contains(diagnostic, []byte("(InvalidNetworkInterfaceID.NotFound)")) &&
			bytes.Contains(diagnostic, []byte("DescribeNetworkInterfaces operation"))
	case [2]string{ec2Service, describeVolumes}:
		return bytes.Contains(diagnostic, []byte("(InvalidVolume.NotFound)")) &&
			bytes.Contains(diagnostic, []byte("DescribeVolumes operation"))
	case [2]string{ec2Service, describeVPCs}:
		return bytes.Contains(diagnostic, []byte("(InvalidVpcID.NotFound)")) &&
			bytes.Contains(diagnostic, []byte("DescribeVpcs operation"))
	case [2]string{elbService, describeLoadBalancers},
		[2]string{elbv2Service, describeLoadBalancers}:
		return bytes.Contains(diagnostic, []byte("(LoadBalancerNotFound)")) &&
			bytes.Contains(diagnostic, []byte("DescribeLoadBalancers operation"))
	default:
		return false
	}
}

func addRows(
	spec operation,
	rows []json.RawMessage,
	observed *observation,
) (int, error) {
	count := 0

	for _, row := range rows {
		identity, group, include, err := classify(spec, row)
		if err != nil {
			return 0, err
		}

		err = addIdentity(observed, identity)
		if err != nil {
			return 0, err
		}

		if include {
			count++

			if group != "" {
				observed.groups[group]++
			}
		}
	}

	return count, nil
}

func addIdentity(observed *observation, identity string) error {
	if observed.identities[identity] || len(observed.identities) >= maxRecords {
		return fmt.Errorf("repeated or excessive identities: %w", ErrInventory)
	}

	if observed.identityBudget >= 0 {
		encoded, err := json.Marshal(identity)
		if err != nil {
			return fmt.Errorf("invalid baseline identity: %w", ErrInventory)
		}

		nextBytes := observed.identityBytes + len(encoded) + 1
		if nextBytes > observed.identityBudget {
			return fmt.Errorf("%w: %w", ErrInventory, errEvidenceLimit)
		}

		observed.identityBytes = nextBytes
	}

	observed.identities[identity] = true

	return nil
}

func capacityGroup(row json.RawMessage, key string) (string, bool, error) {
	value, err := field(row, key)
	if err != nil {
		return "", false, err
	}

	capacity, err := strconv.ParseUint(string(value), 10, 32)
	if err != nil {
		return "", false, fmt.Errorf("invalid capacity: %w", ErrInventory)
	}

	return strconv.FormatUint(capacity, 10), true, nil
}

func quotaLabel(row json.RawMessage) (string, error) {
	code, err := stringField(row, "QuotaCode")
	if err != nil {
		return "", err
	}

	var label string

	switch code {
	case "L-1216C47A":
		label = "Running On-Demand Standard vCPUs"
	case "L-34B43A08":
		label = "Standard Spot vCPUs"
	default:
		name, nameErr := stringField(row, "QuotaName")
		if nameErr != nil {
			return "", nameErr
		}

		if !strings.Contains(name, "Fleet") {
			return "", nil
		}

		label = "Other fleet limit"
	}

	return label, nil
}

func unexpectedToken(object map[string]json.RawMessage, expected string) bool {
	for _, key := range []string{nextTokenKey, eksTokenKey, nextMarkerKey} {
		if key != expected {
			if _, found := object[key]; found {
				return true
			}
		}
	}

	return false
}

func validAWSRequest(args []string) bool {
	if len(args) != awsArgumentCount {
		return false
	}

	expected := []string{
		args[0],
		args[1],
		"--region",
		args[3],
		"--output",
		"json",
		"--no-paginate",
		"--cli-input-json",
		args[8],
		"--no-cli-pager",
	}
	if !slices.Equal(args, expected) || !json.Valid([]byte(args[8])) {
		return false
	}

	valid, _ := regexp.MatchString(`^[a-z]{2}(-[a-z]+)+-[0-9]+$`, args[3])

	return valid &&
		(slices.ContainsFunc(
			append(operations(), capacityReadOperations()...),
			func(spec operation) bool { return spec.service == args[0] && spec.command == args[1] },
		) || ownershipReadAllowed(args[0], args[1]))
}

func ownershipReadAllowed(service, command string) bool {
	return slices.Contains([][2]string{
		{eksService, "describe-cluster"},
		{eksService, "list-nodegroups"},
		{eksService, describeNodegroup},
		{cloudFormationService, describeStacks},
		{cloudFormationService, "list-stack-resources"},
		{ec2Service, describeVPCs},
	}, [2]string{service, command})
}
