package kubernetes

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

// ExtractGatewayPortForTest exposes extractGatewayPort for unit testing.
var ExtractGatewayPortForTest = extractGatewayPort

// ExtractGatewayAddressValueForTest exposes extractGatewayAddressValue for unit testing.
var ExtractGatewayAddressValueForTest = func(gw *unstructured.Unstructured) (string, bool) {
	return extractGatewayAddressValue(gw)
}

// ExtractGatewayAddressForTest exposes extractGatewayAddress for unit testing.
var ExtractGatewayAddressForTest = func(gw *unstructured.Unstructured) (string, int32, bool) {
	return extractGatewayAddress(gw)
}
