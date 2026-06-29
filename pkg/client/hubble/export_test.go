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
