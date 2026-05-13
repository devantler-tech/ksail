package kubernetes

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
)

const (
	// APIServiceName is the name of the Service that targets the nested API server port on the DinD pod.
	APIServiceName = "apiserver"

	// GatewayName is the name of the Gateway resource created for API exposure.
	GatewayName = "ksail-apiserver"

	// TCPRouteName is the name of the TCPRoute resource.
	TCPRouteName = "ksail-apiserver"

	// gatewayReadyPollInterval is the interval between Gateway status checks.
	gatewayReadyPollInterval = 3 * time.Second

	// gatewayReadyTimeout is the maximum time to wait for Gateway address assignment.
	gatewayReadyTimeout = 120 * time.Second
)

// Gateway API GroupVersionResources.
var (
	gatewayGVR = schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1",
		Resource: "gateways",
	}
	tcpRouteGVR = schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1alpha2",
		Resource: "tcproutes",
	}
)

// EnsureAPIExposure creates the Kubernetes Service, Gateway, and TCPRoute to expose
// the nested cluster's API server. If gatewayClassName is empty, only the Service
// is created and instructions for manual port-forward are logged.
func (p *Provider) EnsureAPIExposure(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	clusterName string,
	apiPort int32,
	gatewayClassName string,
) error {
	ns := NamespaceName(clusterName)

	// Create a ClusterIP Service targeting the DinD pod's API server port
	svc := buildAPIService(clusterName, apiPort)

	_, err := p.client.CoreV1().Services(ns).Create(ctx, svc, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("create API server service: %w", err)
	}

	if gatewayClassName == "" {
		// No gateway controller — user must port-forward manually
		return nil
	}

	if dynamicClient == nil {
		return fmt.Errorf("dynamic client is required for Gateway API resources")
	}

	// Create Gateway
	gw := buildGateway(clusterName, gatewayClassName, apiPort)

	_, err = dynamicClient.Resource(gatewayGVR).Namespace(ns).Create(
		ctx, gw, metav1.CreateOptions{},
	)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("create Gateway: %w", err)
	}

	// Create TCPRoute
	route := buildTCPRoute(clusterName, apiPort)

	_, err = dynamicClient.Resource(tcpRouteGVR).Namespace(ns).Create(
		ctx, route, metav1.CreateOptions{},
	)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("create TCPRoute: %w", err)
	}

	return nil
}

// WaitForGateway waits for the Gateway to be assigned an external address.
// Returns the address (IP or hostname) and port.
func (p *Provider) WaitForGateway(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	clusterName string,
) (string, int32, error) {
	ns := NamespaceName(clusterName)
	deadline := time.Now().Add(gatewayReadyTimeout)

	for time.Now().Before(deadline) {
		gw, err := dynamicClient.Resource(gatewayGVR).Namespace(ns).Get(
			ctx, GatewayName, metav1.GetOptions{},
		)
		if err != nil {
			return "", 0, fmt.Errorf("get Gateway: %w", err)
		}

		addr, port, found := extractGatewayAddress(gw)
		if found {
			return addr, port, nil
		}

		select {
		case <-ctx.Done():
			return "", 0, fmt.Errorf("waiting for Gateway: %w", ctx.Err())
		case <-time.After(gatewayReadyPollInterval):
		}
	}

	return "", 0, ErrGatewayNotReady
}

// GetAPIEndpoint returns the API server endpoint for the nested cluster.
// If a Gateway is configured, it returns the Gateway's external address.
// Otherwise, it returns the ClusterIP Service endpoint for port-forward use.
func (p *Provider) GetAPIEndpoint(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	clusterName string,
	gatewayClassName string,
) (string, error) {
	ns := NamespaceName(clusterName)

	if gatewayClassName != "" && dynamicClient != nil {
		addr, port, err := p.WaitForGateway(ctx, dynamicClient, clusterName)
		if err == nil {
			return fmt.Sprintf("https://%s:%d", addr, port), nil
		}
		// Fall through to Service endpoint on Gateway failure
	}

	// Return the ClusterIP Service address for port-forward
	svc, err := p.client.CoreV1().Services(ns).Get(ctx, APIServiceName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get API service: %w", err)
	}

	return fmt.Sprintf("https://%s:%d", svc.Spec.ClusterIP, DinDAPIServerPort), nil
}

// DeleteAPIExposure removes all API exposure resources (Service, Gateway, TCPRoute).
func (p *Provider) DeleteAPIExposure(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	clusterName string,
) error {
	ns := NamespaceName(clusterName)

	// Delete TCPRoute
	if dynamicClient != nil {
		err := dynamicClient.Resource(tcpRouteGVR).Namespace(ns).Delete(
			ctx, TCPRouteName, metav1.DeleteOptions{},
		)
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete TCPRoute: %w", err)
		}

		// Delete Gateway
		err = dynamicClient.Resource(gatewayGVR).Namespace(ns).Delete(
			ctx, GatewayName, metav1.DeleteOptions{},
		)
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete Gateway: %w", err)
		}
	}

	// Delete API Service
	err := p.client.CoreV1().Services(ns).Delete(ctx, APIServiceName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete API service: %w", err)
	}

	return nil
}

func buildAPIService(clusterName string, apiPort int32) *corev1.Service {
	labels := CommonLabels(clusterName)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:   APIServiceName,
			Labels: labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				LabelApp: DinDPodName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "https",
					Port:       apiPort,
					TargetPort: intstr.FromInt32(apiPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

func buildGateway(clusterName, gatewayClassName string, apiPort int32) *unstructured.Unstructured {
	labels := CommonLabels(clusterName)

	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]any{
				"name":   GatewayName,
				"labels": toAnyMap(labels),
			},
			"spec": map[string]any{
				"gatewayClassName": gatewayClassName,
				"listeners": []any{
					map[string]any{
						"name":     "apiserver",
						"protocol": "TCP",
						"port":     int64(apiPort),
					},
				},
			},
		},
	}
}

func buildTCPRoute(clusterName string, apiPort int32) *unstructured.Unstructured {
	labels := CommonLabels(clusterName)
	ns := NamespaceName(clusterName)

	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "gateway.networking.k8s.io/v1alpha2",
			"kind":       "TCPRoute",
			"metadata": map[string]any{
				"name":   TCPRouteName,
				"labels": toAnyMap(labels),
			},
			"spec": map[string]any{
				"parentRefs": []any{
					map[string]any{
						"name":      GatewayName,
						"namespace": ns,
					},
				},
				"rules": []any{
					map[string]any{
						"backendRefs": []any{
							map[string]any{
								"name": APIServiceName,
								"port": int64(apiPort),
							},
						},
					},
				},
			},
		},
	}
}

// extractGatewayAddress extracts the first address and port from a Gateway's status.
func extractGatewayAddress(gw *unstructured.Unstructured) (string, int32, bool) {
	status, found, _ := unstructured.NestedMap(gw.Object, "status")
	if !found {
		return "", 0, false
	}

	addresses, found, _ := unstructured.NestedSlice(status, "addresses")
	if !found || len(addresses) == 0 {
		return "", 0, false
	}

	addrMap, ok := addresses[0].(map[string]any)
	if !ok {
		return "", 0, false
	}

	value, ok := addrMap["value"].(string)
	if !ok || value == "" {
		return "", 0, false
	}

	// Get port from listeners
	listeners, found, _ := unstructured.NestedSlice(status, "listeners")
	if !found || len(listeners) == 0 {
		return value, DinDAPIServerPort, true
	}

	listenerMap, ok := listeners[0].(map[string]any)
	if !ok {
		return value, DinDAPIServerPort, true
	}

	port, ok := listenerMap["port"].(int64)
	if !ok {
		return value, DinDAPIServerPort, true
	}

	return value, int32(port), true
}

// toAnyMap converts a string map to an any map for unstructured objects.
func toAnyMap(m map[string]string) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}

	return result
}
