package hubble

import (
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
)

// Endpoint identifies one side of an observed network flow.
type Endpoint struct {
	Namespace string `json:"namespace,omitempty"`
	Pod       string `json:"pod,omitempty"`
}

// FlowRecord is a distribution-agnostic projection of a single Hubble flow. It
// carries only the fields the `ksail workload network` command renders, so the
// command and its tests never have to depend on the full Hubble protobuf type.
type FlowRecord struct {
	// Time is nil when Hubble did not report a timestamp, so an unknown time
	// stays absent in JSON instead of serializing as the year-0001 zero value.
	Time        *time.Time `json:"time,omitempty"`
	Verdict     string     `json:"verdict"`
	Protocol    string     `json:"protocol,omitempty"`
	Source      Endpoint   `json:"source"`
	Destination Endpoint   `json:"destination"`
}

// recordFromFlow projects a Hubble protobuf flow into a [FlowRecord]. It is
// defensive against nil sub-messages because Hubble omits fields it cannot
// populate (for example, drops that never reached an L4 header).
func recordFromFlow(observed *flowpb.Flow) FlowRecord {
	record := FlowRecord{
		Verdict:  observed.GetVerdict().String(),
		Protocol: protocolOf(observed.GetL4()),
		Source: Endpoint{
			Namespace: observed.GetSource().GetNamespace(),
			Pod:       observed.GetSource().GetPodName(),
		},
		Destination: Endpoint{
			Namespace: observed.GetDestination().GetNamespace(),
			Pod:       observed.GetDestination().GetPodName(),
		},
	}

	if ts := observed.GetTime(); ts != nil {
		when := ts.AsTime()
		record.Time = &when
	}

	return record
}

// protocolOf returns the upper-cased L4 protocol name of a Hubble layer-4
// message, or "" when the protocol is absent or unrecognized.
func protocolOf(layer4 *flowpb.Layer4) string {
	switch {
	case layer4 == nil:
		return ""
	case layer4.GetTCP() != nil:
		return "TCP"
	case layer4.GetUDP() != nil:
		return "UDP"
	case layer4.GetICMPv4() != nil:
		return "ICMPv4"
	case layer4.GetICMPv6() != nil:
		return "ICMPv6"
	case layer4.GetSCTP() != nil:
		return "SCTP"
	default:
		return ""
	}
}
