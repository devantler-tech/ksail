package hubble

import flowpb "github.com/cilium/cilium/api/v1/flow"

// ExportProtocolOf exposes protocolOf for external-package tests.
func ExportProtocolOf(layer4 *flowpb.Layer4) string {
	return protocolOf(layer4)
}

// ExportRecordFromFlow exposes recordFromFlow for external-package tests.
func ExportRecordFromFlow(observed *flowpb.Flow) FlowRecord {
	return recordFromFlow(observed)
}

// FlowStream exposes the unexported receive seam so tests can drive
// [ExportReceiveFlows] with a fake stream instead of a live relay.
type FlowStream = flowStream

// ExportReceiveFlows exposes receiveFlows for external-package tests.
func ExportReceiveFlows(stream FlowStream, emit func(FlowRecord) error) error {
	return receiveFlows(stream, "test-relay:4245", emit)
}
