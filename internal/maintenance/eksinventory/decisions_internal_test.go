package eksinventory

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// rowsCollector returns a collector whose every read answers response and records the decoded
// request input in requests.
func rowsCollector(
	t *testing.T,
	response string,
	requests *[]map[string]any,
) *ownershipCollector {
	t.Helper()

	return &ownershipCollector{
		ctx:       t.Context(),
		binding:   BaselineContext{Region: "eu-central-1", ClusterName: "rows"},
		resources: make(map[string]ownedResource),
		query: func(_ context.Context, args ...string) ([]byte, error) {
			var input map[string]any

			err := json.Unmarshal([]byte(args[8]), &input)
			if err != nil {
				t.Fatal(err)
			}

			*requests = append(*requests, input)

			return []byte(response), nil
		},
	}
}

// TestCaptureFleetsChecksRowsOfASuccessfulRead pins that a successful fleet read is still checked
// against the requested fleets: a missing or foreign row is refused.
func TestCaptureFleetsChecksRowsOfASuccessfulRead(t *testing.T) {
	t.Parallel()

	for name, response := range map[string]string{
		"missing row": `{"Fleets":[]}`,
		"foreign row": `{"Fleets":[{"FleetId":"fleet-0ther"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var requests []map[string]any

			collector := rowsCollector(t, response, &requests)
			collector.add(fleetKind, "fleet-0123456789abcdef0", "stack:root")

			err := collector.captureFleets()
			if !errors.Is(err, ErrInventory) {
				t.Fatalf("fleet rows were not checked: %v", err)
			}
		})
	}
}

// TestVerifyHintsChecksRowsOfASuccessfulRead pins that a successful hint read is checked against
// the requested hints, and that only EC2 reads ask for managed resources.
func TestVerifyHintsChecksRowsOfASuccessfulRead(t *testing.T) {
	t.Parallel()

	t.Run("missing row is refused", func(t *testing.T) {
		t.Parallel()

		var requests []map[string]any

		err := rowsCollector(t, `{"Volumes":[]}`, &requests).verifyHints(
			"ec2", describeVolumes, "VolumeIds", "Volumes", "VolumeId", []string{"vol-1"},
		)
		if !errors.Is(err, ErrInventory) {
			t.Fatalf("hint rows were not checked: %v", err)
		}
	})

	t.Run("ec2 read requests managed resources", func(t *testing.T) {
		t.Parallel()

		var requests []map[string]any

		err := rowsCollector(t, `{"Volumes":[{"VolumeId":"vol-1"}]}`, &requests).verifyHints(
			"ec2", describeVolumes, "VolumeIds", "Volumes", "VolumeId", []string{"vol-1"},
		)
		if err != nil {
			t.Fatal(err)
		}

		if len(requests) != 1 || requests[0][includeManagedResources] != true {
			t.Fatalf("EC2 read did not request managed resources: %#v", requests)
		}
	})

	t.Run("other reads do not request managed resources", func(t *testing.T) {
		t.Parallel()

		var requests []map[string]any

		err := rowsCollector(
			t, `{"LoadBalancerDescriptions":[{"LoadBalancerName":"classic"}]}`, &requests,
		).verifyHints(
			elbService, describeLoadBalancers, "LoadBalancerNames", "LoadBalancerDescriptions",
			"LoadBalancerName", []string{"classic"},
		)
		if err != nil {
			t.Fatal(err)
		}

		if _, found := requests[0][includeManagedResources]; found {
			t.Fatalf("non-EC2 read requested managed resources: %#v", requests)
		}
	})
}

// TestUniqueJSONRejectsTrailingData pins that a valid first value does not hide trailing data.
func TestUniqueJSONRejectsTrailingData(t *testing.T) {
	t.Parallel()

	err := uniqueJSON([]byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("single object refused: %v", err)
	}

	err = uniqueJSON([]byte(`{"a":1} {"a":2}`))
	if !errors.Is(err, ErrInventory) {
		t.Fatalf("trailing object accepted: %v", err)
	}
}

// TestGroupingIgnoresOnlyTerminatedInstances pins that a terminated instance is not reported as a
// leftover, while a running one is.
func TestGroupingIgnoresOnlyTerminatedInstances(t *testing.T) {
	t.Parallel()

	instances := operationByKey(ec2Service, describeInstances)
	if instances == nil || instances.kind != instanceKind {
		t.Fatalf("instance operation missing: %#v", instances)
	}

	group, include, err := grouping(
		*instances, json.RawMessage(`{"State":{"Name":"terminated"},"InstanceType":"t3.large"}`),
	)
	if err != nil || include || group != "" {
		t.Fatalf("terminated instance reported: %q %v %v", group, include, err)
	}

	group, include, err = grouping(
		*instances, json.RawMessage(`{"State":{"Name":"running"},"InstanceType":"t3.large"}`),
	)
	if err != nil || !include || group != "running t3.large" {
		t.Fatalf("running instance not reported: %q %v %v", group, include, err)
	}
}

// TestQuotaLabelForUnknownCodes pins how a quota row with an unknown code is labelled: refused
// without a name, kept only when the name is a fleet limit.
func TestQuotaLabelForUnknownCodes(t *testing.T) {
	t.Parallel()

	_, err := quotaLabel(json.RawMessage(`{"QuotaCode":"L-UNKNOWN"}`))
	if !errors.Is(err, ErrInventory) {
		t.Fatalf("nameless unknown quota accepted: %v", err)
	}

	label, err := quotaLabel(
		json.RawMessage(`{"QuotaCode":"L-UNKNOWN","QuotaName":"EC2 Fleet requests"}`),
	)
	if err != nil || label != "Other fleet limit" {
		t.Fatalf("fleet quota mislabelled: %q %v", label, err)
	}

	label, err = quotaLabel(json.RawMessage(`{"QuotaCode":"L-UNKNOWN","QuotaName":"Elastic IPs"}`))
	if err != nil || label != "" {
		t.Fatalf("unrelated quota kept: %q %v", label, err)
	}
}

// TestIncompleteObservationCountCountsEachIncompleteRead pins the reported number of incomplete
// observations.
func TestIncompleteObservationCountCountsEachIncompleteRead(t *testing.T) {
	t.Parallel()

	count, reason := incompleteObservationCount([]baselineObservation{
		{Complete: false, Reason: "access-denied"},
		{Complete: true},
		{Complete: false, Reason: "access-denied"},
	})
	if count != 2 || reason != "access-denied" {
		t.Fatalf("incomplete observations miscounted: %d %q", count, reason)
	}
}

// TestPublicTokenCharacterBoundaries pins every edge of the public token alphabet.
func TestPublicTokenCharacterBoundaries(t *testing.T) {
	t.Parallel()

	for _, token := range []string{"a", "z", "0", "9", "-", ":", "az09-:"} {
		if !publicToken(token) {
			t.Errorf("public token %q refused", token)
		}
	}

	for _, token := range []string{"`", "{", "/", ";", "A", "_", "a b"} {
		if publicToken(token) {
			t.Errorf("non-public token %q accepted", token)
		}
	}
}

// TestCountFamilyMapsEachFamilyToItsKinds pins the family-to-kind pairs of the public counts.
func TestCountFamilyMapsEachFamilyToItsKinds(t *testing.T) {
	t.Parallel()

	resources := make([]ownedResource, 0, 14)
	for _, kind := range []string{
		stackKind, stackKind, nodegroupKind, autoScalingGroupKind, instanceKind, volumeKind,
		natGatewayKind, addressKind, networkInterfaceKind, vpcKind,
		classicLoadBalancerKind, loadBalancerKind, fleetKind, fleetKind,
	} {
		resources = append(resources, ownedResource{Kind: kind})
	}

	for family, want := range map[string]int{
		"cloudformation":    2,
		"compute":           4,
		"network":           4,
		"workloads":         3,
		instantFleetsFamily: 2,
	} {
		if got := countFamily(resources, family); got != want {
			t.Errorf("family %s counted %d, want %d", family, got, want)
		}
	}
}
