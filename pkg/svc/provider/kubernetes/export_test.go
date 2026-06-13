package kubernetes

import "context"

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

// FirstNodeAddressForTest exposes firstNodeAddress for unit testing.
var FirstNodeAddressForTest = firstNodeAddress //nolint:gochecknoglobals

// ToAnyMapForTest exposes toAnyMap for unit testing.
var ToAnyMapForTest = toAnyMap //nolint:gochecknoglobals

// MapWaitErrorForTest exposes mapWaitError for unit testing.
var MapWaitErrorForTest = mapWaitError //nolint:gochecknoglobals

// NewLocalListenerForTest exposes newLocalListener for unit testing. It returns the
// bound port and a close function.
func NewLocalListenerForTest(ctx context.Context, localPort int) (int, func() error, error) {
	l, err := newLocalListener(ctx, localPort)
	if err != nil {
		return 0, nil, err
	}

	return l.Port, l.Listener.Close, nil
}
