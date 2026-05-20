package kubernetes

import "errors"

// Provider-specific errors.
var (
	// ErrHostClientRequired is returned when a nil Kubernetes client is provided.
	ErrHostClientRequired = errors.New("host cluster Kubernetes client is required")

	// ErrNamespaceNotFound is returned when the cluster namespace does not exist.
	ErrNamespaceNotFound = errors.New("cluster namespace not found")

	// ErrGatewayNotReady is returned when the Gateway has not been assigned an address.
	ErrGatewayNotReady = errors.New("gateway has not been assigned an external address")

	// ErrGatewayClassNotFound is returned when the specified GatewayClass does not exist
	// on the host cluster.
	ErrGatewayClassNotFound = errors.New("specified GatewayClass not found on host cluster")

	// ErrDynamicClientRequired is returned when a dynamic client is needed but nil.
	ErrDynamicClientRequired = errors.New("dynamic client is required for Gateway API resources")

	// ErrLoadBalancerNotReady is returned when a LoadBalancer Service was not assigned an address.
	ErrLoadBalancerNotReady = errors.New(
		"LoadBalancer service was not assigned an external address",
	)

	// ErrNodePortNotAssigned is returned when a NodePort Service has no allocated node port.
	ErrNodePortNotAssigned = errors.New("NodePort service has no allocated node port")

	// ErrNoNodeAddress is returned when no reachable host node address could be determined.
	ErrNoNodeAddress = errors.New("no reachable host node address found for NodePort exposure")

	// ErrDinDNotReady is returned when the DinD pod did not become ready in time.
	ErrDinDNotReady = errors.New("DinD pod did not become ready within timeout")

	// ErrDinDNoIP is returned when the DinD pod has no IP assigned.
	ErrDinDNoIP = errors.New("DinD pod has no IP assigned")

	// ErrUnexpectedAddressFormat is returned when a listener address has an unexpected format.
	ErrUnexpectedAddressFormat = errors.New("unexpected listener address format")

	// ErrPortForwardError is returned when the port-forward error stream receives a non-empty message.
	ErrPortForwardError = errors.New("port-forward error")

	// ErrNamespaceNotOwnedByKSail is returned when a namespace lacks KSail ownership labels.
	ErrNamespaceNotOwnedByKSail = errors.New(
		"namespace does not have KSail ownership labels; refusing deletion",
	)
)
