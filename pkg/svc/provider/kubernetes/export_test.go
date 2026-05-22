package kubernetes

// ExtractGatewayPortForTest exposes extractGatewayPort for unit testing.
var ExtractGatewayPortForTest = extractGatewayPort //nolint:gochecknoglobals

// ExtractGatewayAddressValueForTest exposes extractGatewayAddressValue for unit testing.
var ExtractGatewayAddressValueForTest = extractGatewayAddressValue //nolint:gochecknoglobals

// ExtractGatewayAddressForTest exposes extractGatewayAddress for unit testing.
var ExtractGatewayAddressForTest = extractGatewayAddress //nolint:gochecknoglobals

// HostnameOnlyForTest exposes hostnameOnly for unit testing.
var HostnameOnlyForTest = hostnameOnly //nolint:gochecknoglobals

// PickNodeAddressForTest exposes pickNodeAddress for unit testing.
var PickNodeAddressForTest = (*Provider).pickNodeAddress //nolint:gochecknoglobals

// ExposeViaNodePortForTest exposes exposeViaNodePort for unit testing.
var ExposeViaNodePortForTest = (*Provider).exposeViaNodePort //nolint:gochecknoglobals

// ExposeViaLoadBalancerForTest exposes exposeViaLoadBalancer for unit testing.
var ExposeViaLoadBalancerForTest = (*Provider).exposeViaLoadBalancer //nolint:gochecknoglobals

// PreserveImmutableServiceFieldsForTest exposes preserveImmutableServiceFields for unit testing.
var PreserveImmutableServiceFieldsForTest = preserveImmutableServiceFields //nolint:gochecknoglobals

// IsLoopbackAddressForTest exposes isLoopbackAddress for unit testing.
var IsLoopbackAddressForTest = isLoopbackAddress //nolint:gochecknoglobals
