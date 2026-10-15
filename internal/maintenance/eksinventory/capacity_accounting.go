package eksinventory

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
)

type (
	capacityInstance    struct{ kind, state, reservation string }
	capacityReservation struct {
		kind, state      string
		total, available int
	}
)

func capacityInstances(rows []json.RawMessage, account string) ([]capacityInstance, error) {
	var instances []capacityInstance

	seen := map[string]bool{}

	for _, row := range rows {
		owner, err := stringField(row, "OwnerId")
		if err != nil || owner != account {
			return nil, ErrInventory
		}

		children, err := records(row, []string{"Instances"})
		if err != nil {
			return nil, err
		}

		for _, child := range children {
			_, err := capacityUniqueIdentity(child, "InstanceId", seen)
			if err != nil {
				return nil, err
			}

			instance, include, err := capacityInstanceRecord(child)
			if err != nil {
				return nil, err
			}

			if include {
				instances = append(instances, instance)
			}
		}
	}

	return instances, nil
}

func capacityInstanceRecord(row json.RawMessage) (capacityInstance, bool, error) {
	state, err := stringField(row, "State.Name")
	if err != nil {
		return capacityInstance{}, false, err
	}

	switch state {
	case "terminated", "stopped", "stopping":
		return capacityInstance{}, false, nil
	case capacityRunning, capacityPending:
	default:
		return capacityInstance{}, false, ErrInventory
	}

	return capacityLiveInstance(row, state)
}

func capacityLiveInstance(row json.RawMessage, state string) (capacityInstance, bool, error) {
	lifecycle, err := capacityOptionalString(row, "InstanceLifecycle")
	if err != nil {
		return capacityInstance{}, false, err
	}

	if lifecycle == "spot" {
		return capacityInstance{}, false, nil
	}

	if lifecycle != "" {
		return capacityInstance{}, false, ErrInventory
	}

	tenancy, err := stringField(row, "Placement.Tenancy")
	if err != nil || tenancy != capacityDefault {
		return capacityInstance{}, false, ErrInventory
	}

	kind, err := stringField(row, "InstanceType")
	if err != nil || !standardType(kind) {
		return capacityInstance{}, false, ErrInventory
	}

	reservation, err := capacityOptionalString(row, "CapacityReservationId")
	if err != nil {
		return capacityInstance{}, false, err
	}

	return capacityInstance{kind: kind, state: state, reservation: reservation}, true, nil
}

func capacityReservations(
	rows []json.RawMessage,
	account string,
) (map[string]capacityReservation, error) {
	reservations := map[string]capacityReservation{}
	seen := map[string]bool{}

	for _, row := range rows {
		identity, err := stringField(row, "CapacityReservationId")
		if err != nil || seen[identity] {
			return nil, ErrInventory
		}

		seen[identity] = true

		reservation, include, err := capacityReservationRecord(row, account)
		if err != nil {
			return nil, err
		}

		if include {
			reservations[identity] = reservation
		}
	}

	return reservations, nil
}

func capacityReservationRecord(
	row json.RawMessage,
	account string,
) (capacityReservation, bool, error) {
	state, err := stringField(row, "State")
	if err != nil {
		return capacityReservation{}, false, err
	}

	switch state {
	case "expired", "cancelled", "failed", "unsupported", "cancelling":
		return capacityReservation{}, false, nil
	case "assessing", "scheduled", capacityPending, "active", "delayed":
	default:
		return capacityReservation{}, false, ErrInventory
	}

	kind, err := capacityReservationKind(row, account)
	if err != nil {
		return capacityReservation{}, false, err
	}

	total, err := capacityInteger(row, "TotalInstanceCount", 1)
	if err != nil || total > maxRecords {
		return capacityReservation{}, false, ErrInventory
	}

	available, err := capacityInteger(row, "AvailableInstanceCount", 0)
	if err != nil || available > total {
		return capacityReservation{}, false, ErrInventory
	}

	return capacityReservation{
		kind:      kind,
		state:     state,
		total:     total,
		available: available,
	}, true, nil
}

func capacityReservationKind(row json.RawMessage, account string) (string, error) {
	owner, err := stringField(row, "OwnerId")
	if err != nil || owner != account {
		return "", ErrInventory
	}

	kind, err := stringField(row, "InstanceType")
	if err != nil || !standardType(kind) {
		return "", ErrInventory
	}

	err = capacityOrdinaryReservation(row)

	return kind, err
}

func capacityUniqueIdentity(row json.RawMessage, key string, seen map[string]bool) (string, error) {
	identity, err := stringField(row, key)
	if err != nil || seen[identity] || len(seen) >= maxRecords {
		return "", ErrInventory
	}

	seen[identity] = true

	return identity, nil
}

func capacityOrdinaryReservation(row json.RawMessage) error {
	reservationType, err := stringField(row, "ReservationType")
	if err != nil || reservationType != capacityDefault {
		return ErrInventory
	}

	tenancy, err := stringField(row, "Tenancy")
	if err != nil || tenancy != capacityDefault {
		return ErrInventory
	}

	return capacityReservationVariant(row)
}

func capacityReservationVariant(row json.RawMessage) error {
	var object map[string]json.RawMessage

	err := json.Unmarshal(row, &object)
	if err != nil {
		return ErrInventory
	}

	for _, key := range []string{"InterruptionInfo", "InterruptibleCapacityAllocation", "CapacityBlockId", "OutpostArn"} {
		if _, exists := object[key]; exists {
			return ErrInventory
		}
	}

	raw, exists := object["Interruptible"]
	if !exists {
		return nil
	}

	var value *bool

	err = json.Unmarshal(raw, &value)
	if err != nil || value == nil || *value {
		return ErrInventory
	}

	return nil
}

func capacityUsage(
	instances []capacityInstance,
	reservations map[string]capacityReservation,
	cpus map[string]int,
) (int, int, error) {
	usage, pending := 0, 0
	occupants := map[string]int{}

	for _, reservation := range reservations {
		usage += reservation.total * cpus[reservation.kind]
	}

	for _, instance := range instances {
		err := capacityAssociation(instance, reservations, occupants)
		if err != nil {
			return 0, 0, err
		}

		if instance.state == capacityPending {
			pending += cpus[instance.kind]

			continue
		}

		if instance.reservation == "" {
			usage += cpus[instance.kind]
		}
	}

	return usage, pending, nil
}

func capacityAssociation(
	instance capacityInstance,
	reservations map[string]capacityReservation,
	occupants map[string]int,
) error {
	if instance.reservation == "" {
		return nil
	}

	reservation, exists := reservations[instance.reservation]
	if !exists || reservation.kind != instance.kind || reservation.state != "active" {
		return ErrInventory
	}

	if instance.state == capacityRunning {
		occupants[instance.reservation]++
		if occupants[instance.reservation] > reservation.total-reservation.available {
			return ErrInventory
		}
	}

	return nil
}

func capacityOptionalString(row json.RawMessage, key string) (string, error) {
	var object map[string]json.RawMessage

	err := json.Unmarshal(row, &object)
	if err != nil || object == nil {
		return "", ErrInventory
	}

	value, exists := object[key]
	if !exists {
		return "", nil
	}

	var text string

	err = json.Unmarshal(value, &text)
	if err != nil || text == "" {
		return "", ErrInventory
	}

	return text, nil
}

// uniqueJSON rejects duplicate keys and excessive nesting before accounting.
// encoding/json otherwise silently lets the last contradictory field win.
func uniqueJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	err := uniqueJSONValue(decoder, 0)
	if err != nil {
		return err
	}

	_, err = decoder.Token()
	if !errors.Is(err, io.EOF) {
		return ErrInventory
	}

	return nil
}

func uniqueJSONValue(decoder *json.Decoder, depth int) error {
	if depth > maxJSONDepth {
		return ErrInventory
	}

	token, err := decoder.Token()
	if err != nil {
		return ErrInventory
	}

	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	if !slices.Contains([]json.Delim{'{', '['}, delimiter) {
		return ErrInventory
	}

	err = uniqueJSONChildren(decoder, delimiter, depth)
	if err != nil {
		return err
	}

	closing, err := decoder.Token()
	if err != nil {
		return ErrInventory
	}

	expected := json.Delim(']')
	if delimiter == '{' {
		expected = json.Delim('}')
	}

	if closing != expected {
		return ErrInventory
	}

	return nil
}

func uniqueJSONChildren(decoder *json.Decoder, delimiter json.Delim, depth int) error {
	keys := map[string]bool{}

	for decoder.More() {
		if delimiter == '{' {
			keyToken, err := decoder.Token()
			if err != nil {
				return ErrInventory
			}

			key, valid := keyToken.(string)
			if !valid || keys[key] {
				return ErrInventory
			}

			keys[key] = true
		}

		err := uniqueJSONValue(decoder, depth+1)
		if err != nil {
			return err
		}
	}

	return nil
}
