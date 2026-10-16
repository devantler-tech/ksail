package eksinventory

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	ownershipSchemaVersion = 3
	ownershipPageSize      = 100
	nanosecondDigits       = 9
)

var volumeIDPattern = regexp.MustCompile(`^vol-[0-9a-f]{17}$`)

// OwnershipHints are exact resource identities observed through the cluster API.
// They are verified through AWS before they become ownership evidence.
type OwnershipHints struct {
	ClassicLoadBalancers []string
	LoadBalancerARNs     []string
	LoadBalancerHosts    []string
	VolumeIDs            []string
	WorkloadReadComplete bool
}

type ownershipManifest struct {
	SchemaVersion        int                          `json:"schemaVersion"`
	Repository           string                       `json:"repository"`
	AccountID            string                       `json:"accountId"`
	Context              BaselineContext              `json:"context"`
	ClusterIdentity      string                       `json:"clusterIdentity"`
	Baseline             baselineManifest             `json:"baseline"`
	BaselineDigest       string                       `json:"baselineDigest"`
	CaptureCount         int                          `json:"captureCount"`
	CompleteCaptureCount int                          `json:"completeCaptureCount"`
	StartedAt            time.Time                    `json:"startedAt"`
	UpdatedAt            time.Time                    `json:"updatedAt"`
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
}

type ownershipCollector struct {
	ctx       context.Context //nolint:containedctx // One bounded capture owns this context and every child read.
	binding   BaselineContext
	query     Query
	manifest  *ownershipManifest
	resources map[string]ownedResource
}

// CaptureOwnership imports a complete private baseline and atomically publishes
// cumulative exact resource relationships. Existing relationships are never
// removed when later captures observe a detached or deleted resource.
//
//nolint:cyclop,contextcheck,funlen // This is the fail-closed transaction boundary for all capture phases.
func CaptureOwnership(
	ctx context.Context,
	binding BaselineContext,
	baselinePath string,
	destination string,
	hints OwnershipHints,
	output io.Writer,
	query Query,
) error {
	err := binding.Validate()
	if err != nil {
		return err
	}

	if baselinePath == "" || destination == "" || baselinePath == destination {
		return fmt.Errorf("invalid ownership evidence paths: %w", ErrInventory)
	}

	if query == nil {
		query = awsQuery
	}

	baseline, err := importBaseline(baselinePath, binding)
	if err != nil {
		return err
	}

	manifest, replacing, err := loadOwnership(destination, binding, baseline)
	if err != nil {
		return err
	}

	collector := ownershipCollector{
		ctx: ctx, binding: binding, query: query, manifest: manifest,
		resources: make(map[string]ownedResource),
	}
	for _, resource := range manifest.Resources {
		collector.resources[resourceKey(resource)] = resource
	}

	complete := collector.capture(hints)

	if !replacing && manifest.AccountID == "" {
		return fmt.Errorf("ownership capture has no verified account binding: %w", ErrInventory)
	}

	manifest.Resources = collector.sortedResources()

	manifest.CaptureCount++
	if complete {
		manifest.CompleteCaptureCount++
	}

	manifest.UpdatedAt = time.Now().UTC()

	data, err := json.Marshal(manifest)
	if err != nil || len(data) > maxBaselineBytes {
		return fmt.Errorf("ownership evidence limit exceeded: %w", ErrInventory)
	}

	err = publishOwnership(destination, data, replacing)
	if err != nil {
		return err
	}

	err = printOwnership(output, manifest)
	if err != nil {
		return err
	}

	if !complete {
		return fmt.Errorf("ownership capture contains unknown coverage: %w", ErrInventory)
	}

	return nil
}

//nolint:cyclop // Every independent evidence invariant intentionally rejects the import.
func importBaseline(path string, binding BaselineContext) (baselineManifest, error) {
	var baseline baselineManifest

	err := readPrivateEvidence(path, &baseline)
	if err != nil {
		return baseline, err
	}

	if baseline.SchemaVersion != 1 || baseline.Repository != repositoryName ||
		baseline.Context != binding || baseline.StartedAt.IsZero() || baseline.CompletedAt.IsZero() ||
		baseline.CompletedAt.Before(
			baseline.StartedAt,
		) || len(baseline.Observations) != len(operations()) {
		return baseline, fmt.Errorf("baseline binding or schema mismatch: %w", ErrInventory)
	}

	seen := make(map[string]bool)

	expected := make(map[string]bool, len(operations()))
	for _, spec := range operations() {
		expected[spec.service+"\x00"+spec.command] = true
	}

	for _, row := range baseline.Observations {
		key := row.Service + "\x00" + row.Operation
		if !expected[key] || seen[key] || !row.Complete || row.Identities == nil ||
			row.ReportCount == nil || row.Reason != "" {
			return baseline, fmt.Errorf("baseline is incomplete: %w", ErrInventory)
		}

		seen[key] = true
	}

	return baseline, nil
}

//nolint:cyclop // Existing evidence must satisfy every schema, binding, and integrity invariant.
func loadOwnership(
	path string,
	binding BaselineContext,
	baseline baselineManifest,
) (*ownershipManifest, bool, error) {
	digest, err := evidenceDigest(baseline)
	if err != nil {
		return nil, false, err
	}

	_, err = os.Lstat(path)
	if os.IsNotExist(err) {
		return &ownershipManifest{
			SchemaVersion:  ownershipSchemaVersion,
			Repository:     repositoryName,
			Context:        binding,
			Baseline:       baseline,
			BaselineDigest: digest,
			StartedAt:      time.Now().UTC(),
			Coverage:       make(map[string]ownershipCoverage),
		}, false, nil
	}

	if err != nil {
		return nil, false, fmt.Errorf("inspect ownership evidence: %w", ErrInventory)
	}

	var manifest ownershipManifest

	err = readPrivateEvidence(path, &manifest)
	if err != nil {
		return nil, false, err
	}

	validMetadata := manifest.SchemaVersion == ownershipSchemaVersion &&
		manifest.Repository == repositoryName && manifest.Context == binding &&
		manifest.AccountID != "" && manifest.CaptureCount > 0 &&
		manifest.CompleteCaptureCount >= 0 &&
		manifest.CompleteCaptureCount <= manifest.CaptureCount &&
		!manifest.StartedAt.IsZero() &&
		manifest.Baseline.SchemaVersion == baseline.SchemaVersion &&
		manifest.Baseline.Context == baseline.Context && manifest.BaselineDigest == digest
	if !validMetadata {
		return nil, false, fmt.Errorf("ownership binding or schema mismatch: %w", ErrInventory)
	}

	manifestBaselineDigest, digestErr := evidenceDigest(manifest.Baseline)
	if digestErr != nil || manifestBaselineDigest != digest ||
		validateOwnershipManifest(&manifest) != nil {
		return nil, false, fmt.Errorf(
			"ownership evidence was altered or is invalid: %w",
			ErrInventory,
		)
	}

	return &manifest, true, nil
}

//nolint:cyclop // Graph validation rejects each malformed relationship independently.
func validateOwnershipManifest(manifest *ownershipManifest) error {
	accountValid, _ := regexp.MatchString(`^[0-9]{12}$`, manifest.AccountID)
	expectedCluster := "arn:aws:eks:" + manifest.Context.Region + ":" + manifest.AccountID +
		":cluster/" + manifest.Context.ClusterName + "|"
	clusterCoverage, hasClusterCoverage := manifest.Coverage["cluster"]

	clusterValid := strings.HasPrefix(manifest.ClusterIdentity, expectedCluster)
	if manifest.ClusterIdentity == "" {
		clusterValid = hasClusterCoverage && !clusterCoverage.ReadComplete
	}

	if !accountValid || !clusterValid || manifest.UpdatedAt.Before(manifest.StartedAt) {
		return ErrInventory
	}

	knownKinds := map[string]bool{
		addressKind: true, autoScalingGroupKind: true, classicLoadBalancerKind: true,
		fleetKind: true, instanceKind: true, loadBalancerKind: true, natGatewayKind: true,
		networkInterfaceKind: true, nodegroupKind: true, stackKind: true, volumeKind: true,
		vpcKind: true,
	}
	identities := make(map[string]bool)

	triples := make(map[string]bool)
	for _, resource := range manifest.Resources {
		if !knownKinds[resource.Kind] || !safeEvidenceString(resource.Identity) ||
			!safeEvidenceString(resource.Parent) || triples[resourceKey(resource)] {
			return ErrInventory
		}

		triples[resourceKey(resource)] = true
		identities[resource.Kind+"\x00"+resource.Identity] = true
	}

	for _, resource := range manifest.Resources {
		parentKind, parentIdentity, found := strings.Cut(resource.Parent, ":")
		if !found || parentIdentity == "" {
			return ErrInventory
		}

		switch parentKind {
		case "cluster":
			if parentIdentity != manifest.ClusterIdentity {
				return ErrInventory
			}
		case "run":
			if parentIdentity != manifest.Context.RunID+"/"+manifest.Context.RunAttempt {
				return ErrInventory
			}
		default:
			if !identities[parentKind+"\x00"+parentIdentity] {
				return ErrInventory
			}
		}
	}

	return nil
}

func safeEvidenceString(value string) bool {
	return value != "" && len(value) <= 4096 && !strings.ContainsAny(value, "\r\n\x00")
}

func evidenceDigest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode evidence digest: %w", ErrInventory)
	}

	return fmt.Sprintf("sha256:%x", sha256.Sum256(data)), nil
}

//nolint:cyclop,funlen,gocritic // Each family records independent completeness and ownership state.
func (collector *ownershipCollector) capture(hints OwnershipHints) bool {
	complete := true

	accountID, err := collector.callerAccount()
	if err != nil {
		collector.cover("account", false, "unknown", err)

		return false
	}

	if collector.manifest.AccountID != "" && collector.manifest.AccountID != accountID {
		collector.cover("account", false, "mismatch", ErrInventory)

		return false
	}

	collector.manifest.AccountID = accountID
	collector.cover("account", true, "verified", nil)

	clusterIdentity, err := collector.clusterIdentity(accountID)
	if err != nil {
		collector.cover("cluster", false, "unknown", err)

		complete = false
	} else {
		if collector.manifest.ClusterIdentity != "" &&
			collector.manifest.ClusterIdentity != clusterIdentity {
			collector.cover("cluster", false, "identity-mismatch", ErrInventory)

			return false
		}

		collector.manifest.ClusterIdentity = clusterIdentity
		collector.cover("cluster", true, "exact-identity", nil)
	}

	parent := "run:" + collector.binding.RunID + "/" + collector.binding.RunAttempt
	if collector.manifest.ClusterIdentity != "" {
		parent = "cluster:" + collector.manifest.ClusterIdentity
	}

	err = collector.captureStacks(accountID, parent)
	if err != nil {
		collector.cover("cloudformation", false, "partial", err)

		complete = false
	} else {
		collector.cover("cloudformation", true, exactRelationships, nil)
	}

	if clusterIdentity != "" {
		err = collector.captureCompute(clusterIdentity)
		if err != nil {
			collector.cover("compute", false, "partial", err)

			complete = false
		} else {
			collector.cover("compute", true, exactRelationships, nil)
		}
	}

	err = collector.captureNAT()
	if err != nil {
		collector.cover("network", false, "partial", err)

		complete = false
	} else {
		collector.cover("network", true, exactRelationships, nil)
	}

	err = collector.captureFleets()
	if err != nil {
		collector.cover(instantFleetsFamily, false, "partial", err)

		complete = false
	} else {
		collector.cover(instantFleetsFamily, true, "exact-identities", nil)
	}

	err = collector.captureHints(parent, hints)
	if err != nil {
		collector.cover("workloads", false, "partial", err)

		complete = false
	} else if !hints.WorkloadReadComplete {
		collector.cover("workloads", true, "unknown", nil)
	} else {
		collector.cover("workloads", true, "exact-identities", nil)
	}

	return complete
}

func (collector *ownershipCollector) callerAccount() (string, error) {
	payload, err := collector.readSingle("sts", "get-caller-identity", map[string]any{})
	if err != nil {
		return "", err
	}

	account, err := stringField(payload, "Account")
	if err != nil {
		return "", err
	}

	valid, _ := regexp.MatchString(`^[0-9]{12}$`, account)
	if !valid {
		return "", fmt.Errorf("invalid caller account: %w", ErrInventory)
	}

	arn, err := stringField(payload, "Arn")
	if err != nil || !strings.Contains(arn, ":"+account+":") {
		return "", fmt.Errorf("caller identity mismatch: %w", ErrInventory)
	}

	return account, nil
}

func (collector *ownershipCollector) clusterIdentity(accountID string) (string, error) {
	payload, err := collector.readSingle("eks", "describe-cluster", map[string]any{
		"name": collector.binding.ClusterName,
	})
	if err != nil {
		return "", err
	}

	cluster, err := field(payload, "cluster")
	if err != nil {
		return "", err
	}

	arn, err := stringField(cluster, "arn")
	if err != nil {
		return "", err
	}

	prefix := "arn:aws:eks:" + collector.binding.Region + ":" + accountID + ":cluster/"
	if arn != prefix+collector.binding.ClusterName {
		return "", fmt.Errorf("unexpected cluster identity: %w", ErrInventory)
	}

	created, err := field(cluster, "createdAt")
	if err != nil {
		return "", err
	}

	createdIdentity, err := clusterCreationIdentity(created)
	if err != nil {
		return "", err
	}

	return arn + "|" + createdIdentity, nil
}

func clusterCreationIdentity(value json.RawMessage) (string, error) {
	var timestamp string
	if json.Unmarshal(value, &timestamp) == nil {
		createdAt, err := time.Parse(time.RFC3339Nano, timestamp)
		if err != nil {
			return "", fmt.Errorf("invalid cluster creation identity: %w", ErrInventory)
		}

		return createdAt.UTC().Format(time.RFC3339Nano), nil
	}

	secondsText, fractionText, _ := strings.Cut(string(value), ".")
	if len(fractionText) > nanosecondDigits {
		return "", fmt.Errorf("invalid cluster creation identity: %w", ErrInventory)
	}

	seconds, err := strconv.ParseInt(secondsText, 10, 64)
	if err != nil || seconds <= 0 || seconds > 253402300799 {
		return "", fmt.Errorf("invalid cluster creation identity: %w", ErrInventory)
	}

	fractionText += strings.Repeat("0", nanosecondDigits-len(fractionText))

	nanoseconds, err := strconv.ParseInt(fractionText, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid cluster creation identity: %w", ErrInventory)
	}

	createdAt := time.Unix(seconds, nanoseconds).UTC()

	return createdAt.Format(time.RFC3339Nano), nil
}

func (collector *ownershipCollector) captureStacks(accountID, parent string) error {
	name := "eksctl-" + collector.binding.ClusterName + "-cluster"

	payload, err := collector.readSingle("cloudformation", describeStacks, map[string]any{
		"StackName": name,
	})
	if err != nil {
		return err
	}

	rows, err := records(payload, []string{"Stacks"})
	if err != nil || len(rows) != 1 {
		return fmt.Errorf("root stack lookup incomplete: %w", ErrInventory)
	}

	stackID, err := stringField(rows[0], "StackId")
	if err != nil || !strings.Contains(stackID, ":"+accountID+":stack/") {
		return fmt.Errorf("invalid root stack identity: %w", ErrInventory)
	}

	collector.add(stackKind, stackID, parent)

	return collector.captureStackResources(stackID, make(map[string]bool))
}

//nolint:cyclop // Provider rows are validated field by field before entering the graph.
func (collector *ownershipCollector) captureStackResources(
	stackID string,
	seen map[string]bool,
) error {
	if seen[stackID] {
		return fmt.Errorf("nested stack cycle: %w", ErrInventory)
	}

	seen[stackID] = true

	rows, err := collector.readAll(
		"cloudformation", "list-stack-resources", map[string]any{"StackName": stackID},
		"StackResourceSummaries", "NextToken", "NextToken",
	)
	if err != nil {
		return err
	}

	for _, row := range rows {
		_, fieldErr := stringField(row, "LogicalResourceId")
		if fieldErr != nil {
			return fieldErr
		}

		resourceType, fieldErr := stringField(row, "ResourceType")
		if fieldErr != nil {
			return fieldErr
		}

		_, fieldErr = stringField(row, "ResourceStatus")
		if fieldErr != nil {
			return fieldErr
		}

		physical, fieldErr := optionalStringField(row, "PhysicalResourceId")
		if fieldErr != nil {
			return fieldErr
		}

		if physical == "" {
			continue
		}

		kind := cloudFormationKind(resourceType)
		if kind == "" {
			continue
		}

		collector.add(kind, physical, "stack:"+stackID)

		if kind == stackKind {
			err = collector.captureStackResources(physical, seen)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

//nolint:cyclop,funlen // The relationship walk validates every provider boundary before continuing.
func (collector *ownershipCollector) captureCompute(clusterIdentity string) error {
	names, err := collector.readAll(
		eksService,
		"list-nodegroups",
		map[string]any{
			"clusterName": collector.binding.ClusterName,
			"maxResults":  ownershipPageSize,
		},
		"nodegroups",
		"nextToken",
		"nextToken",
	)
	if err != nil {
		return err
	}

	for _, rawName := range names {
		name, valueErr := stringValue(rawName)
		if valueErr != nil {
			return valueErr
		}

		payload, readErr := collector.readSingle(eksService, describeNodegroup, map[string]any{
			"clusterName": collector.binding.ClusterName, "nodegroupName": name,
		})
		if readErr != nil {
			return readErr
		}

		nodegroup, fieldErr := field(payload, nodegroupKind)
		if fieldErr != nil {
			return fieldErr
		}

		arn, fieldErr := stringField(nodegroup, "nodegroupArn")
		if fieldErr != nil ||
			!strings.Contains(arn, "/"+collector.binding.ClusterName+"/"+name+"/") {
			return fmt.Errorf("invalid nodegroup identity: %w", ErrInventory)
		}

		collector.add(nodegroupKind, arn, "cluster:"+clusterIdentity)

		err = collector.capturePhysicalStack(
			collector.binding.ClusterName+"/"+name,
			"nodegroup:"+arn,
		)
		if err != nil {
			return err
		}

		resources, fieldErr := field(nodegroup, "resources")
		if fieldErr != nil {
			return fieldErr
		}

		groups, fieldErr := records(resources, []string{"autoScalingGroups"})
		if fieldErr != nil {
			return fieldErr
		}

		for _, group := range groups {
			groupName, nameErr := stringField(group, "name")
			if nameErr != nil {
				return nameErr
			}

			err = collector.captureAutoScalingGroup(groupName, arn)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (collector *ownershipCollector) capturePhysicalStack(physicalID, parent string) error {
	payload, err := collector.readSingle(
		"cloudformation",
		"describe-stack-resources",
		map[string]any{
			"PhysicalResourceId": physicalID,
		},
	)
	if err != nil {
		return err
	}

	rows, err := records(payload, []string{"StackResources"})
	if err != nil || len(rows) == 0 {
		return fmt.Errorf("physical stack lookup incomplete: %w", ErrInventory)
	}

	stackID := ""
	matches := 0

	for _, row := range rows {
		actual, fieldErr := optionalStringField(row, "PhysicalResourceId")
		if fieldErr != nil {
			return fieldErr
		}

		if actual != physicalID {
			continue
		}

		matches++

		stackID, fieldErr = stringField(row, "StackId")
		if fieldErr != nil {
			return fieldErr
		}
	}

	prefix := "arn:aws:cloudformation:" + collector.binding.Region + ":" +
		collector.manifest.AccountID + ":stack/"
	if matches != 1 || !strings.HasPrefix(stackID, prefix) {
		return fmt.Errorf("physical stack identity mismatch: %w", ErrInventory)
	}

	collector.add(stackKind, stackID, parent)

	return collector.captureStackResources(stackID, make(map[string]bool))
}

//nolint:cyclop // Lookup, identity binding, and child extraction are separate fail-closed checks.
func (collector *ownershipCollector) captureAutoScalingGroup(name, parent string) error {
	payload, err := collector.readSingle(
		autoScalingService,
		describeAutoScalingGroups,
		map[string]any{
			"AutoScalingGroupNames": []string{name},
		},
	)
	if err != nil {
		return err
	}

	groups, err := records(payload, []string{autoScalingGroupsCollection})
	if err != nil || len(groups) != 1 {
		return fmt.Errorf("autoscaling group lookup incomplete: %w", ErrInventory)
	}

	arn, err := stringField(groups[0], "AutoScalingGroupARN")
	if err != nil {
		return err
	}

	actualName, err := stringField(groups[0], "AutoScalingGroupName")
	if err != nil || actualName != name {
		return fmt.Errorf("autoscaling identity mismatch: %w", ErrInventory)
	}

	collector.add(autoScalingGroupKind, arn, "nodegroup:"+parent)

	instances, err := records(groups[0], []string{"Instances"})
	if err != nil {
		return err
	}

	ids := make([]string, 0, len(instances))
	for _, instance := range instances {
		identity, fieldErr := stringField(instance, instanceIDField)
		if fieldErr != nil {
			return fieldErr
		}

		ids = append(ids, identity)
	}

	if len(ids) == 0 {
		return nil
	}

	return collector.captureInstances(ids, arn)
}

//nolint:cyclop // Every nested instance relationship is validated before it is retained.
func (collector *ownershipCollector) captureInstances(ids []string, parent string) error {
	rows, err := collector.readExactRows(
		"ec2",
		"describe-instances",
		map[string]any{"InstanceIds": ids, includeManagedResources: true},
		reservationsInstances,
	)
	if err != nil {
		return err
	}

	if len(rows) != len(ids) {
		return fmt.Errorf("instance lookup incomplete: %w", ErrInventory)
	}

	for _, row := range rows {
		identity, fieldErr := stringField(row, instanceIDField)
		if fieldErr != nil || !slices.Contains(ids, identity) {
			return fmt.Errorf("instance identity mismatch: %w", ErrInventory)
		}

		collector.add(instanceKind, identity, autoScalingGroupKind+":"+parent)

		interfaces, fieldErr := records(row, []string{networkInterfacesCollection})
		if fieldErr != nil {
			return fieldErr
		}

		for _, network := range interfaces {
			value, valueErr := stringField(network, networkInterfaceIDField)
			if valueErr != nil {
				return valueErr
			}

			collector.add("network-interface", value, "instance:"+identity)
		}

		mappings, fieldErr := records(row, []string{"BlockDeviceMappings"})
		if fieldErr != nil {
			return fieldErr
		}

		for _, mapping := range mappings {
			value, valueErr := stringField(mapping, "Ebs.VolumeId")
			if valueErr != nil {
				return valueErr
			}

			collector.add(volumeKind, value, instanceKind+":"+identity)
		}
	}

	return nil
}

//nolint:cyclop // Optional allocation and interface links require independent validation.
func (collector *ownershipCollector) captureNAT() error {
	ids := collector.identities(natGatewayKind)
	if len(ids) == 0 {
		return nil
	}

	rows, err := collector.readExactRows(
		"ec2", describeNatGateways, map[string]any{"NatGatewayIds": ids},
		natGatewaysCollection,
	)
	if err != nil {
		return err
	}

	for _, row := range rows {
		identity, fieldErr := stringField(row, natGatewayIDField)
		if fieldErr != nil || !slices.Contains(ids, identity) {
			return fmt.Errorf("NAT identity mismatch: %w", ErrInventory)
		}

		addresses, fieldErr := records(row, []string{"NatGatewayAddresses"})
		if fieldErr != nil {
			return fieldErr
		}

		for _, address := range addresses {
			allocation, valueErr := optionalStringField(address, "AllocationId")
			if valueErr != nil {
				return valueErr
			}

			if allocation != "" {
				collector.add(addressKind, allocation, natGatewayKind+":"+identity)
			}

			network, valueErr := optionalStringField(address, networkInterfaceIDField)
			if valueErr != nil {
				return valueErr
			}

			if network != "" {
				collector.add(networkInterfaceKind, network, natGatewayKind+":"+identity)
			}
		}
	}

	return nil
}

func (collector *ownershipCollector) captureFleets() error {
	ids := collector.identities(fleetKind)
	if len(ids) == 0 {
		return nil
	}

	rows, err := collector.readExactRows(
		"ec2", describeFleets, map[string]any{"FleetIds": ids},
		"Fleets",
	)
	if err != nil {
		return err
	}

	seen := make(map[string]bool)

	for _, row := range rows {
		identity, fieldErr := stringField(row, "FleetId")
		if fieldErr != nil || !slices.Contains(ids, identity) || seen[identity] {
			return fmt.Errorf("fleet identity mismatch: %w", ErrInventory)
		}

		seen[identity] = true
	}

	if len(seen) != len(ids) {
		return fmt.Errorf("fleet lookup incomplete: %w", ErrInventory)
	}

	return nil
}

//nolint:cyclop,funlen // Each supplied identity family has distinct syntax and provider checks.
func (collector *ownershipCollector) captureHints(parent string, hints OwnershipHints) error {
	for _, hint := range hints.ClassicLoadBalancers {
		if hint == "" || len(hint) > 255 || strings.ContainsAny(hint, "\r\n\x00") {
			return fmt.Errorf("invalid classic load balancer hint: %w", ErrInventory)
		}
	}

	for _, hint := range hints.LoadBalancerARNs {
		if !strings.HasPrefix(
			hint,
			"arn:aws:elasticloadbalancing:"+collector.binding.Region+":"+collector.manifest.AccountID+":",
		) {
			return fmt.Errorf("invalid load balancer hint: %w", ErrInventory)
		}
	}

	for _, hint := range hints.LoadBalancerHosts {
		valid, _ := regexp.MatchString(
			`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?\.(?:`+
				regexp.QuoteMeta(collector.binding.Region)+`\.elb|elb\.`+
				regexp.QuoteMeta(collector.binding.Region)+`)\.amazonaws\.com$`,
			hint,
		)
		if !valid {
			return fmt.Errorf("invalid load balancer host hint: %w", ErrInventory)
		}
	}

	for _, hint := range hints.VolumeIDs {
		if !volumeIDPattern.MatchString(hint) {
			return fmt.Errorf("invalid volume hint: %w", ErrInventory)
		}
	}

	err := collector.verifyHints(elbService, describeLoadBalancers, "LoadBalancerNames",
		classicLoadBalancersCollection, classicLoadBalancerNameField, hints.ClassicLoadBalancers)
	if err != nil {
		return err
	}

	err = collector.verifyHints(elbv2Service, describeLoadBalancers, "LoadBalancerArns",
		loadBalancersCollection, loadBalancerARNField, hints.LoadBalancerARNs)
	if err != nil {
		return err
	}

	err = collector.verifyHints("ec2", describeVolumes, "VolumeIds",
		"Volumes", "VolumeId", hints.VolumeIDs)
	if err != nil {
		return err
	}

	err = collector.captureLoadBalancerHosts(parent, hints.LoadBalancerHosts)
	if err != nil {
		return err
	}

	for _, hint := range hints.ClassicLoadBalancers {
		collector.add(classicLoadBalancerKind, hint, parent)
	}

	for _, hint := range hints.LoadBalancerARNs {
		collector.add(loadBalancerKind, hint, parent)
	}

	for _, hint := range hints.VolumeIDs {
		collector.add(volumeKind, hint, parent)
	}

	return nil
}

func (collector *ownershipCollector) captureLoadBalancerHosts(parent string, hints []string) error {
	if len(hints) == 0 {
		return nil
	}

	matched := make(map[string]bool)

	for _, spec := range []struct {
		service, collection, identity, kind string
	}{
		{elbService, classicLoadBalancersCollection, classicLoadBalancerNameField, classicLoadBalancerKind},
		{elbv2Service, loadBalancersCollection, loadBalancerARNField, loadBalancerKind},
	} {
		rows, err := collector.readAll(
			spec.service, describeLoadBalancers, map[string]any{pageSizeKey: ownershipPageSize},
			spec.collection, markerKey, "NextMarker",
		)
		if err != nil {
			return err
		}

		for _, row := range rows {
			host, fieldErr := stringField(row, "DNSName")
			if fieldErr != nil {
				return fieldErr
			}

			if !slices.Contains(hints, host) {
				continue
			}

			if matched[host] {
				return fmt.Errorf("ambiguous workload load balancer: %w", ErrInventory)
			}

			identity, fieldErr := stringField(row, spec.identity)
			if fieldErr != nil {
				return fieldErr
			}

			collector.add(spec.kind, identity, parent)

			matched[host] = true
		}
	}

	if len(matched) != len(hints) {
		return fmt.Errorf("workload load balancer lookup incomplete: %w", ErrInventory)
	}

	return nil
}

func (collector *ownershipCollector) verifyHints(
	service, command, inputKey, collection, identityKey string,
	hints []string,
) error {
	if len(hints) == 0 {
		return nil
	}

	input := map[string]any{inputKey: hints}
	if service == "ec2" {
		input[includeManagedResources] = true
	}

	rows, err := collector.readExactRows(service, command, input, collection)
	if err != nil {
		return err
	}

	seen := make(map[string]bool)

	for _, row := range rows {
		identity, fieldErr := stringField(row, identityKey)
		if fieldErr != nil || !slices.Contains(hints, identity) || seen[identity] {
			return fmt.Errorf("hint identity mismatch: %w", ErrInventory)
		}

		seen[identity] = true
	}

	if len(seen) != len(hints) {
		return fmt.Errorf("hint lookup incomplete: %w", ErrInventory)
	}

	return nil
}

func (collector *ownershipCollector) readAll(
	service, command string,
	input map[string]any,
	collection, inputToken, outputToken string,
) ([]json.RawMessage, error) {
	var result []json.RawMessage

	tokens := make(map[string]bool)

	request := cloneInput(input)
	for range maxPages {
		payload, err := collector.readPage(service, command, request)
		if err != nil {
			return nil, err
		}

		rows, err := records(payload, strings.Split(collection, "."))
		if err != nil {
			return nil, err
		}

		if len(result)+len(rows) > maxRecords {
			return nil, fmt.Errorf("ownership record limit exceeded: %w", ErrInventory)
		}

		result = append(result, rows...)

		next, err := nextToken(payload, outputToken)
		if err != nil {
			return nil, err
		}

		if next == "" {
			return result, nil
		}

		if tokens[next] {
			return nil, fmt.Errorf("repeated ownership pagination token: %w", ErrInventory)
		}

		tokens[next] = true
		request[inputToken] = next
	}

	return nil, fmt.Errorf("ownership pagination limit exceeded: %w", ErrInventory)
}

func (collector *ownershipCollector) readSingle(
	service, command string,
	input map[string]any,
) (json.RawMessage, error) {
	payload, err := collector.readPage(service, command, input)
	if err != nil {
		return nil, err
	}

	_, err = nextToken(payload, "")
	if err != nil {
		return nil, err
	}

	return payload, nil
}

func (collector *ownershipCollector) readExactRows(
	service, command string,
	input map[string]any,
	collection string,
) ([]json.RawMessage, error) {
	payload, err := collector.readSingle(service, command, input)
	if err != nil {
		return nil, err
	}

	rows, err := records(payload, strings.Split(collection, "."))
	if err != nil {
		return nil, err
	}

	if len(rows) > maxRecords {
		return nil, fmt.Errorf("ownership record limit exceeded: %w", ErrInventory)
	}

	return rows, nil
}

func (collector *ownershipCollector) readPage(
	service, command string,
	input map[string]any,
) (json.RawMessage, error) {
	err := collector.ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("ownership deadline: %w", err)
	}

	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode ownership request: %w", ErrInventory)
	}

	callCtx, cancel := context.WithTimeout(collector.ctx, callTimeout)
	defer cancel()

	data, err := collector.query(
		callCtx, service, command, "--region", collector.binding.Region, "--output", "json",
		"--no-paginate", "--cli-input-json", string(encoded), "--no-cli-pager",
	)
	if err != nil {
		return nil, fmt.Errorf("ownership AWS read failed: %w", err)
	}

	if callCtx.Err() != nil {
		return nil, fmt.Errorf("ownership AWS read exceeded deadline: %w", callCtx.Err())
	}

	if len(data) > maxResponseBytes || !json.Valid(data) {
		return nil, fmt.Errorf("invalid ownership AWS response: %w", ErrInventory)
	}

	var object map[string]json.RawMessage

	err = json.Unmarshal(data, &object)
	if err != nil || object == nil {
		return nil, fmt.Errorf("missing ownership response object: %w", ErrInventory)
	}

	return json.RawMessage(data), nil
}

func (collector *ownershipCollector) add(kind, identity, parent string) {
	resource := ownedResource{Kind: kind, Identity: identity, Parent: parent}
	collector.resources[resourceKey(resource)] = resource
}

func (collector *ownershipCollector) identities(kind string) []string {
	set := make(map[string]bool)

	for _, resource := range collector.resources {
		if resource.Kind == kind {
			set[resource.Identity] = true
		}
	}

	result := make([]string, 0, len(set))
	for identity := range set {
		result = append(result, identity)
	}

	sort.Strings(result)

	return result
}

func (collector *ownershipCollector) sortedResources() []ownedResource {
	result := make([]ownedResource, 0, len(collector.resources))
	for _, resource := range collector.resources {
		result = append(result, resource)
	}

	sort.Slice(result, func(left, right int) bool {
		if result[left].Kind != result[right].Kind {
			return result[left].Kind < result[right].Kind
		}

		if result[left].Identity != result[right].Identity {
			return result[left].Identity < result[right].Identity
		}

		return result[left].Parent < result[right].Parent
	})

	return result
}

func (collector *ownershipCollector) cover(
	family string,
	readComplete bool,
	ownership string,
	err error,
) {
	coverage := ownershipCoverage{ReadComplete: readComplete, OwnershipCoverage: ownership}
	if err != nil {
		coverage.Reason = failureReason(err)
	}

	collector.manifest.Coverage[family] = coverage
}

func printOwnership(output io.Writer, manifest *ownershipManifest) error {
	_, err := fmt.Fprintf(
		output,
		"EKS cumulative ownership capture (%s)\n",
		manifest.Context.Region,
	)
	if err != nil {
		return fmt.Errorf("write ownership summary: %w", err)
	}

	families := make([]string, 0, len(manifest.Coverage))
	for family := range manifest.Coverage {
		families = append(families, family)
	}

	sort.Strings(families)

	for _, family := range families {
		coverage := manifest.Coverage[family]

		state := "unknown"
		if coverage.ReadComplete {
			state = "complete"
		}

		_, err = fmt.Fprintf(
			output, "%s: read=%s ownership=%s recorded=%d\n",
			family, state, coverage.OwnershipCoverage, countFamily(manifest.Resources, family),
		)
		if err != nil {
			return fmt.Errorf("write ownership family: %w", err)
		}
	}

	_, err = fmt.Fprintln(
		output,
		"Private identifiers retained in the owner-only local manifest; teardown remains unverified.",
	)
	if err != nil {
		return fmt.Errorf("write ownership retention: %w", err)
	}

	return nil
}

//nolint:cyclop,lll // An explicit family-to-kind table keeps public counts deterministic.
func countFamily(resources []ownedResource, family string) int {
	count := 0

	for _, resource := range resources {
		if resource.Kind == family ||
			(family == "cloudformation" && resource.Kind == stackKind) ||
			(family == "compute" && slices.Contains([]string{nodegroupKind, autoScalingGroupKind, instanceKind, volumeKind}, resource.Kind)) ||
			(family == "network" && slices.Contains([]string{natGatewayKind, addressKind, networkInterfaceKind, vpcKind}, resource.Kind)) ||
			(family == "workloads" && slices.Contains([]string{classicLoadBalancerKind, loadBalancerKind, volumeKind}, resource.Kind)) ||
			(family == instantFleetsFamily && resource.Kind == fleetKind) {
			count++
		}
	}

	return count
}

func cloudFormationKind(resourceType string) string {
	return map[string]string{
		"AWS::CloudFormation::Stack":                stackKind,
		"AWS::AutoScaling::AutoScalingGroup":        autoScalingGroupKind,
		"AWS::EC2::Instance":                        instanceKind,
		"AWS::EC2::NatGateway":                      natGatewayKind,
		"AWS::EC2::EIP":                             "address",
		"AWS::EC2::NetworkInterface":                "network-interface",
		"AWS::EC2::Volume":                          volumeKind,
		"AWS::EC2::VPC":                             vpcKind,
		"AWS::EC2::EC2Fleet":                        fleetKind,
		"AWS::EC2::SpotFleet":                       fleetKind,
		"AWS::ElasticLoadBalancing::LoadBalancer":   classicLoadBalancerKind,
		"AWS::ElasticLoadBalancingV2::LoadBalancer": loadBalancerKind,
	}[resourceType]
}

func optionalStringField(data json.RawMessage, path string) (string, error) {
	value, err := field(data, path)
	if err != nil {
		if errors.Is(err, ErrInventory) {
			return "", nil
		}

		return "", err
	}

	if strings.TrimSpace(string(value)) == "null" {
		return "", nil
	}

	return stringValue(value)
}

func resourceKey(resource ownedResource) string {
	return resource.Kind + "\x00" + resource.Identity + "\x00" + resource.Parent
}

func cloneInput(input map[string]any) map[string]any {
	result := make(map[string]any, len(input)+1)
	maps.Copy(result, input)

	return result
}
