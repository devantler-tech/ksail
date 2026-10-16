package eksinventory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
)

const (
	verificationAbsent    = "absent"
	verificationRemaining = "remaining"
	verificationUnknown   = "unknown"
)

type resourceVerification struct {
	state  string
	reason string
	count  int
}

type kindVerification struct {
	absent    int
	remaining int
	unknown   int
	reason    string
}

// VerifyOwnership consumes one complete private run-owned manifest and reads
// every recorded identity after cleanup. It also compares a fresh complete
// regional census with the pre-create baseline, so an uncorrelated survivor is
// unknown rather than silently ignored. Only aggregate states are printed.
//
//nolint:cyclop,funlen,contextcheck // One bounded collector owns and propagates the supplied context.
func VerifyOwnership(
	ctx context.Context,
	binding BaselineContext,
	manifestPath string,
	output io.Writer,
	query Query,
) error {
	if binding.Validate() != nil || manifestPath == "" {
		return printVerificationFailure(output, invalidDataReason)
	}

	var manifest ownershipManifest

	err := readPrivateEvidence(manifestPath, &manifest)
	if err != nil || manifest.Context != binding || validateOwnershipEvidence(&manifest) != nil {
		return printVerificationFailure(output, invalidDataReason)
	}

	if query == nil {
		query = awsQuery
	}

	collector := ownershipCollector{ctx: ctx, binding: binding, query: query, manifest: &manifest}

	accountID, err := collector.callerAccount()
	if err != nil {
		return printVerificationFailure(output, failureReason(err))
	}

	if accountID != manifest.AccountID {
		return printVerificationFailure(output, "identity-mismatch")
	}

	results := make(map[string]*kindVerification)
	cluster := verifyCluster(&collector, manifest.ClusterIdentity)
	addVerification(results, "cluster", cluster)

	for _, resource := range manifest.Resources {
		observed := verifyOwnedResource(&collector, resource)
		addVerification(results, resource.Kind, observed)

		if resource.Kind == vpcKind && observed.state == verificationRemaining {
			addVerification(
				results,
				"vpc-census",
				verifyOwnedVPCCensus(&collector, resource.Identity, manifest.Resources),
			)
		}
	}

	census := verifyRegionalCensus(ctx, binding, &manifest, query)
	addVerification(results, "regional-census", census)

	verdict := verificationAbsent

	for _, result := range results {
		if result.unknown > 0 {
			verdict = verificationUnknown

			break
		}

		if result.remaining > 0 {
			verdict = verificationRemaining
		}
	}

	err = printVerification(output, binding.Region, results, verdict)
	if err != nil {
		return err
	}

	if verdict != verificationAbsent {
		return fmt.Errorf("EKS cleanup is %s: %w", verdict, ErrInventory)
	}

	return nil
}

//nolint:cyclop // Every evidence binding and completeness invariant fails independently.
func validateOwnershipEvidence(manifest *ownershipManifest) error {
	if manifest.SchemaVersion != ownershipSchemaVersion || manifest.Repository != repositoryName ||
		manifest.CaptureCount <= 0 || manifest.CompleteCaptureCount <= 0 ||
		manifest.CompleteCaptureCount > manifest.CaptureCount || validateOwnershipManifest(manifest) != nil {
		return ErrInventory
	}

	digest, err := evidenceDigest(manifest.Baseline)
	if err != nil || digest != manifest.BaselineDigest ||
		manifest.Baseline.Context != manifest.Context ||
		len(manifest.Baseline.Observations) != len(operations()) {
		return ErrInventory
	}

	expected := map[string]string{
		"account":           "verified",
		"cluster":           "exact-identity",
		"cloudformation":    exactRelationships,
		"compute":           exactRelationships,
		"network":           exactRelationships,
		instantFleetsFamily: "exact-identities",
		"workloads":         "exact-identities",
	}
	if len(manifest.Coverage) != len(expected) {
		return ErrInventory
	}

	for family, ownership := range expected {
		coverage, found := manifest.Coverage[family]
		if !found || !coverage.ReadComplete || coverage.OwnershipCoverage != ownership ||
			coverage.Reason != "" {
			return ErrInventory
		}
	}

	for _, observation := range manifest.Baseline.Observations {
		if !observation.Complete || observation.Identities == nil ||
			observation.ReportCount == nil || observation.Reason != "" {
			return ErrInventory
		}
	}

	return nil
}

func verifyCluster(collector *ownershipCollector, expected string) resourceVerification {
	actual, err := collector.clusterIdentity(collector.manifest.AccountID)
	if err != nil {
		if failureReason(err) == notFoundReason {
			return resourceVerification{state: verificationAbsent}
		}

		return resourceVerification{state: verificationUnknown, reason: failureReason(err)}
	}

	if actual != expected {
		return resourceVerification{state: verificationUnknown, reason: "identity-replaced"}
	}

	return resourceVerification{state: verificationRemaining}
}

//nolint:cyclop,funlen // Each resource API has distinct exact-input and terminal-state rules.
func verifyOwnedResource(
	collector *ownershipCollector,
	resource ownedResource,
) resourceVerification {
	var (
		rows       []json.RawMessage
		err        error
		identity   string
		state      string
		collection string
	)

	input := make(map[string]any)

	var service, command, identityField, stateFieldName string

	absentStates := []string(nil)

	switch resource.Kind {
	case stackKind:
		service, command, collection = cloudFormationService, describeStacks, "Stacks"
		input["StackName"] = resource.Identity
		identityField, stateFieldName = "StackId", "StackStatus"
		absentStates = []string{"DELETE_COMPLETE"}
	case nodegroupKind:
		clusterName, nodegroupName, valid := nodegroupIdentityParts(
			resource.Identity,
			collector.binding.Region,
			collector.manifest.AccountID,
		)
		if !valid || clusterName != collector.binding.ClusterName {
			return resourceVerification{
				state:  verificationUnknown,
				reason: invalidDataReason,
			}
		}

		service, command, collection = eksService, describeNodegroup, nodegroupKind
		input["clusterName"], input["nodegroupName"] = clusterName, nodegroupName
		identityField = "nodegroupArn"
	case autoScalingGroupKind:
		name, found := strings.CutPrefix(
			resource.Identity,
			autoScalingGroupNamePrefix(resource.Identity),
		)
		if !found || name == "" {
			return resourceVerification{
				state:  verificationUnknown,
				reason: invalidDataReason,
			}
		}

		service, command, collection = "autoscaling", "describe-auto-scaling-groups", autoScalingGroupsCollection
		input["AutoScalingGroupNames"] = []string{name}
		identityField = "AutoScalingGroupARN"
	case instanceKind:
		service, command, collection = ec2Service, describeInstances, reservationsInstances
		input["InstanceIds"], input[includeManagedResources] = []string{resource.Identity}, true
		identityField, stateFieldName = instanceIDField, "State.Name"
		absentStates = []string{terminatedState}
	case fleetKind:
		service, command, collection = ec2Service, describeFleets, "Fleets"
		input["FleetIds"] = []string{resource.Identity}
		identityField, stateFieldName = "FleetId", "FleetState"
		absentStates = []string{deletedState, failedState}
	case natGatewayKind:
		service, command, collection = ec2Service, describeNatGateways, natGatewaysCollection
		input["NatGatewayIds"] = []string{resource.Identity}
		identityField, stateFieldName = natGatewayIDField, stateField
		absentStates = []string{deletedState, failedState}
	case addressKind:
		service, command, collection = ec2Service, describeAddresses, "Addresses"
		input["AllocationIds"] = []string{resource.Identity}
		identityField = "AllocationId"
	case networkInterfaceKind:
		service, command, collection = ec2Service, describeNetworkInterfaces, networkInterfacesCollection
		input["NetworkInterfaceIds"], input[includeManagedResources] = []string{
			resource.Identity,
		}, true
		identityField = networkInterfaceIDField
	case volumeKind:
		service, command, collection = ec2Service, describeVolumes, "Volumes"
		input["VolumeIds"], input[includeManagedResources] = []string{resource.Identity}, true
		identityField, stateFieldName = "VolumeId", stateField
		absentStates = []string{deletedState}
	case vpcKind:
		service, command, collection = ec2Service, describeVPCs, "Vpcs"
		input["VpcIds"] = []string{resource.Identity}
		identityField, stateFieldName = vpcIDField, stateField
		absentStates = []string{deletedState}
	case classicLoadBalancerKind:
		service, command, collection = elbService, describeLoadBalancers, classicLoadBalancersCollection
		input["LoadBalancerNames"] = []string{resource.Identity}
		identityField = classicLoadBalancerNameField
	case loadBalancerKind:
		service, command, collection = elbv2Service, describeLoadBalancers, loadBalancersCollection
		input["LoadBalancerArns"] = []string{resource.Identity}
		identityField = loadBalancerARNField
	default:
		return resourceVerification{
			state:  verificationUnknown,
			reason: invalidDataReason,
		}
	}

	if resource.Kind == nodegroupKind {
		var payload json.RawMessage

		payload, err = collector.readSingle(service, command, input)
		if err == nil {
			var row json.RawMessage

			row, err = field(payload, collection)
			if err == nil {
				rows = []json.RawMessage{row}
			}
		}
	} else {
		rows, err = collector.readExactRows(service, command, input, collection)
	}

	if err != nil {
		if failureReason(err) == notFoundReason {
			return resourceVerification{state: verificationAbsent}
		}

		return resourceVerification{state: verificationUnknown, reason: failureReason(err)}
	}

	if len(rows) == 0 {
		return resourceVerification{state: verificationAbsent}
	}

	if len(rows) != 1 {
		return resourceVerification{
			state:  verificationUnknown,
			reason: invalidDataReason,
		}
	}

	identity, err = stringField(rows[0], identityField)
	if err != nil || identity != resource.Identity {
		return resourceVerification{state: verificationUnknown, reason: "identity-mismatch"}
	}

	if stateFieldName != "" {
		state, err = stringField(rows[0], stateFieldName)
		if err != nil {
			return resourceVerification{
				state:  verificationUnknown,
				reason: invalidDataReason,
			}
		}

		if slices.Contains(absentStates, state) {
			return resourceVerification{state: verificationAbsent}
		}
	}

	return resourceVerification{state: verificationRemaining}
}

//nolint:funlen // Each API has different pagination and VPC field names.
func verifyOwnedVPCCensus(
	collector *ownershipCollector,
	vpcID string,
	resources []ownedResource,
) resourceVerification {
	type censusRead struct {
		service, command, collection, inputToken, outputToken, identity, vpc string
		input                                                                map[string]any
	}

	filters := []map[string]any{{"Name": "vpc-id", "Values": []string{vpcID}}}
	reads := []censusRead{
		{
			ec2Service,
			describeInstances,
			reservationsInstances,
			nextTokenKey,
			nextTokenKey,
			instanceIDField,
			vpcIDField,
			map[string]any{
				"Filters":               filters,
				maxResultsKey:           ownershipPageSize,
				includeManagedResources: true,
			},
		},
		{
			ec2Service, describeNatGateways, natGatewaysCollection, nextTokenKey,
			nextTokenKey, natGatewayIDField, vpcIDField,
			map[string]any{"Filter": filters, maxResultsKey: ownershipPageSize},
		},
		{
			ec2Service,
			describeNetworkInterfaces,
			networkInterfacesCollection,
			nextTokenKey,
			nextTokenKey,
			networkInterfaceIDField,
			vpcIDField,
			map[string]any{
				"Filters":               filters,
				maxResultsKey:           ownershipPageSize,
				includeManagedResources: true,
			},
		},
		{
			elbService, describeLoadBalancers, classicLoadBalancersCollection, markerKey,
			nextMarkerKey, classicLoadBalancerNameField, "VPCId",
			map[string]any{pageSizeKey: ownershipPageSize},
		},
		{
			elbv2Service, describeLoadBalancers, loadBalancersCollection, markerKey,
			nextMarkerKey, loadBalancerARNField, vpcIDField,
			map[string]any{pageSizeKey: ownershipPageSize},
		},
	}

	owned := make(map[string]bool, len(resources))
	for _, resource := range resources {
		owned[resource.Identity] = true
	}

	unknown := 0

	for _, read := range reads {
		rows, err := collector.readAll(
			read.service, read.command, read.input, read.collection,
			read.inputToken, read.outputToken,
		)
		if err != nil {
			return resourceVerification{state: verificationUnknown, reason: failureReason(err)}
		}

		for _, row := range rows {
			rowVPC, fieldErr := stringField(row, read.vpc)
			if fieldErr != nil {
				return resourceVerification{
					state: verificationUnknown, reason: invalidDataReason,
				}
			}

			if rowVPC != vpcID {
				continue
			}

			identity, fieldErr := stringField(row, read.identity)
			if fieldErr != nil {
				return resourceVerification{
					state: verificationUnknown, reason: invalidDataReason,
				}
			}

			if !owned[identity] {
				unknown++
			}
		}
	}

	if unknown > 0 {
		return resourceVerification{
			state: verificationUnknown, reason: "unattributed-vpc-resource", count: unknown,
		}
	}

	return resourceVerification{state: verificationAbsent}
}

func autoScalingGroupNamePrefix(identity string) string {
	marker := ":autoScalingGroupName/"

	index := strings.LastIndex(identity, marker)
	if index < 0 {
		return identity
	}

	return identity[:index+len(marker)]
}

func nodegroupIdentityParts(identity, region, accountID string) (string, string, bool) {
	prefix := "arn:aws:eks:" + region + ":" + accountID + ":nodegroup/"

	remainder, found := strings.CutPrefix(identity, prefix)
	if !found {
		return "", "", false
	}

	parts := strings.Split(remainder, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}

	return parts[0], parts[1], true
}

//nolint:cyclop // Every observation must preserve complete baseline/current pairing.
func verifyRegionalCensus(
	ctx context.Context,
	binding BaselineContext,
	manifest *ownershipManifest,
	query Query,
) resourceVerification {
	current, complete := collectBaseline(ctx, binding, query)
	if !complete {
		unknown, reason := incompleteObservationCount(current.Observations)

		return resourceVerification{state: verificationUnknown, reason: reason, count: unknown}
	}

	baseline := make(map[string]baselineObservation, len(manifest.Baseline.Observations))
	for _, observation := range manifest.Baseline.Observations {
		baseline[observation.Service+"\x00"+observation.Operation] = observation
	}

	owned := make(map[string]bool, len(manifest.Resources)+1)

	owned[manifest.Context.ClusterName] = true
	for _, resource := range manifest.Resources {
		owned[resource.Identity] = true
	}

	unknown := 0

	for _, observation := range current.Observations {
		spec := operationByKey(observation.Service, observation.Operation)
		if spec == nil || spec.kind == quotaKind {
			continue
		}

		before, found := baseline[observation.Service+"\x00"+observation.Operation]
		if !found || before.Identities == nil || observation.Identities == nil {
			unknown++

			continue
		}

		previous := make(map[string]bool, len(*before.Identities))
		for _, identity := range *before.Identities {
			previous[identity] = true
		}

		for _, identity := range *observation.Identities {
			if !previous[identity] && !owned[identity] {
				unknown++
			}
		}
	}

	if unknown > 0 {
		return resourceVerification{
			state: verificationUnknown, reason: "unattributed-new-resource", count: unknown,
		}
	}

	return resourceVerification{state: verificationAbsent}
}

func incompleteObservationCount(observations []baselineObservation) (int, string) {
	count := 0
	reasons := make(map[string]bool)

	for _, observation := range observations {
		if observation.Complete {
			continue
		}

		count++
		reasons[observation.Reason] = true
	}

	if count == 0 {
		return 1, invalidDataReason
	}

	values := make([]string, 0, len(reasons))
	for reason := range reasons {
		values = append(values, reason)
	}

	sort.Strings(values)

	if len(values) != 1 || values[0] == "" {
		return count, invalidDataReason
	}

	return count, values[0]
}

func operationByKey(service, command string) *operation {
	for _, candidate := range operations() {
		if candidate.service == service && candidate.command == command {
			candidateCopy := candidate

			return &candidateCopy
		}
	}

	return nil
}

func addVerification(
	results map[string]*kindVerification,
	kind string,
	observed resourceVerification,
) {
	result := results[kind]
	if result == nil {
		result = &kindVerification{}
		results[kind] = result
	}

	count := observed.count
	if count <= 0 {
		count = 1
	}

	switch observed.state {
	case verificationAbsent:
		result.absent += count
	case verificationRemaining:
		result.remaining += count
	default:
		result.unknown += count
		if result.reason == "" {
			result.reason = observed.reason
		}
	}
}

func printVerification(
	output io.Writer,
	region string,
	results map[string]*kindVerification,
	verdict string,
) error {
	_, err := fmt.Fprintf(
		output,
		"EKS run-owned cleanup verification (%s)\n",
		region,
	)
	if err != nil {
		return fmt.Errorf("write cleanup verification: %w", err)
	}

	kinds := make([]string, 0, len(results))
	for kind := range results {
		kinds = append(kinds, kind)
	}

	sort.Strings(kinds)

	for _, kind := range kinds {
		result := results[kind]

		state, count, reason := verificationAbsent, result.absent, ""
		if result.remaining > 0 {
			state, count = verificationRemaining, result.remaining
		}

		if result.unknown > 0 {
			state, count, reason = verificationUnknown, result.unknown, result.reason
		}

		if reason == "" {
			_, err = fmt.Fprintf(
				output,
				"%s: state=%s count=%d\n",
				kind,
				state,
				count,
			)
			if err != nil {
				return fmt.Errorf("write cleanup resource verdict: %w", err)
			}
		} else {
			_, err = fmt.Fprintf(
				output, "%s: state=%s count=%d reason=%s\n", kind, state, count, reason,
			)
			if err != nil {
				return fmt.Errorf("write cleanup resource verdict: %w", err)
			}
		}
	}

	_, err = fmt.Fprintf(output, "cleanup-verdict=%s\n", verdict)
	if err != nil {
		return fmt.Errorf("write cleanup verdict: %w", err)
	}

	return nil
}

func printVerificationFailure(output io.Writer, reason string) error {
	_, err := fmt.Fprintf(
		output,
		"EKS run-owned cleanup verification\ncleanup-verdict=unknown reason=%s\n",
		reason,
	)
	if err != nil {
		return fmt.Errorf("write cleanup verification failure: %w", err)
	}

	return fmt.Errorf("EKS cleanup is unknown: %w", ErrInventory)
}
