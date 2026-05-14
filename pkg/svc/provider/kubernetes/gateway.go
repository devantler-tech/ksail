package kubernetes

import (
	"context"
	"fmt"
	"net"
	"strconv"
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

// gatewayGVR returns the GroupVersionResource for Gateway API Gateway objects.
func gatewayGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1",
		Resource: "gateways",
	}
}

// tcpRouteGVR returns the GroupVersionResource for Gateway API TCPRoute objects.
func tcpRouteGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1alpha2",
		Resource: "tcproutes",
	}
}

// EnsureAPIExposure creates or updates the Kubernetes Service, Gateway, and TCPRoute to expose
// the nested cluster's API server. If gatewayClassName is empty, only the Service
// is created and instructions for manual port-forward are logged.
func (p *Provider) EnsureAPIExposure(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	clusterName string,
	apiPort int32,
	gatewayClassName string,
) error {
	namespace := NamespaceName(clusterName)

	// Create or update the ClusterIP Service targeting the DinD pod's API server port
	svc := buildAPIService(clusterName, apiPort)

	_, err := p.client.CoreV1().Services(namespace).Create(ctx, svc, metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		existing, getErr := p.client.CoreV1().Services(namespace).Get(ctx, APIServiceName, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("get existing API server service: %w", getErr)
		}
		svc.ResourceVersion = existing.ResourceVersion
		svc.Spec.ClusterIP = existing.Spec.ClusterIP // ClusterIP is immutable; preserve it
		_, err = p.client.CoreV1().Services(namespace).Update(ctx, svc, metav1.UpdateOptions{})
	}

	if err != nil {
		return fmt.Errorf("ensure API server service: %w", err)
	}

	if gatewayClassName == "" {
		// No gateway controller — user must port-forward manually
		return nil
	}

	if dynamicClient == nil {
		return fmt.Errorf("ensure API exposure: %w", ErrDynamicClientRequired)
	}

	// Create or update Gateway
	gateway := buildGateway(clusterName, gatewayClassName, apiPort)

	_, err = dynamicClient.Resource(gatewayGVR()).Namespace(namespace).Create(
		ctx, gateway, metav1.CreateOptions{},
	)
	if errors.IsAlreadyExists(err) {
		existing, getErr := dynamicClient.Resource(gatewayGVR()).Namespace(namespace).Get(
			ctx, GatewayName, metav1.GetOptions{},
		)
		if getErr != nil {
			return fmt.Errorf("get existing Gateway: %w", getErr)
		}
		gateway.SetResourceVersion(existing.GetResourceVersion())
		_, err = dynamicClient.Resource(gatewayGVR()).Namespace(namespace).Update(
			ctx, gateway, metav1.UpdateOptions{},
		)
	}

	if err != nil {
		return fmt.Errorf("ensure Gateway: %w", err)
	}

	// Create or update TCPRoute
	route := buildTCPRoute(clusterName, apiPort)

	_, err = dynamicClient.Resource(tcpRouteGVR()).Namespace(namespace).Create(
		ctx, route, metav1.CreateOptions{},
	)
	if errors.IsAlreadyExists(err) {
		existing, getErr := dynamicClient.Resource(tcpRouteGVR()).Namespace(namespace).Get(
			ctx, TCPRouteName, metav1.GetOptions{},
		)
		if getErr != nil {
			return fmt.Errorf("get existing TCPRoute: %w", getErr)
		}
		route.SetResourceVersion(existing.GetResourceVersion())
		_, err = dynamicClient.Resource(tcpRouteGVR()).Namespace(namespace).Update(
			ctx, route, metav1.UpdateOptions{},
		)
	}

	if err != nil {
		return fmt.Errorf("ensure TCPRoute: %w", err)
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
	namespace := NamespaceName(clusterName)
	deadline := time.Now().Add(gatewayReadyTimeout)

	for time.Now().Before(deadline) {
		gateway, err := dynamicClient.Resource(gatewayGVR()).Namespace(namespace).Get(
			ctx, GatewayName, metav1.GetOptions{},
		)
		if err != nil {
			return "", 0, fmt.Errorf("get Gateway: %w", err)
		}

		addr, port, found := extractGatewayAddress(gateway)
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
	namespace := NamespaceName(clusterName)

	if gatewayClassName != "" && dynamicClient != nil {
		addr, port, err := p.WaitForGateway(ctx, dynamicClient, clusterName)
		if err == nil {
			return "https://" + net.JoinHostPort(addr, strconv.FormatInt(int64(port), 10)), nil
		}
		// Fall through to Service endpoint on Gateway failure
	}

	// Return the ClusterIP Service address for port-forward
	svc, err := p.client.CoreV1().Services(namespace).Get(ctx, APIServiceName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get API service: %w", err)
	}

	return "https://" + net.JoinHostPort(
		svc.Spec.ClusterIP, strconv.FormatInt(int64(svc.Spec.Ports[0].Port), 10),
	), nil
}

// DeleteAPIExposure removes all API exposure resources (Service, Gateway, TCPRoute).
func (p *Provider) DeleteAPIExposure(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	clusterName string,
) error {
	namespace := NamespaceName(clusterName)

	// Delete TCPRoute
	if dynamicClient != nil {
		err := dynamicClient.Resource(tcpRouteGVR()).Namespace(namespace).Delete(
			ctx, TCPRouteName, metav1.DeleteOptions{},
		)
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete TCPRoute: %w", err)
		}

		// Delete Gateway
		err = dynamicClient.Resource(gatewayGVR()).Namespace(namespace).Delete(
			ctx, GatewayName, metav1.DeleteOptions{},
		)
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete Gateway: %w", err)
		}
	}

	// Delete API Service
	err := p.client.CoreV1().Services(namespace).Delete(ctx, APIServiceName, metav1.DeleteOptions{})
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
func extractGatewayAddress(gateway *unstructured.Unstructured) (string, int32, bool) {
	addr, found := extractGatewayAddressValue(gateway)
	if !found {
		return "", 0, false
	}

	port := extractGatewayPort(gateway)

	return addr, port, true
}

// extractGatewayAddressValue extracts the address value from the Gateway status.
func extractGatewayAddressValue(gateway *unstructured.Unstructured) (string, bool) {
	status, found, _ := unstructured.NestedMap(gateway.Object, "status")
	if !found {
		return "", false
	}

	addresses, found, _ := unstructured.NestedSlice(status, "addresses")
	if !found || len(addresses) == 0 {
		return "", false
	}

	addrMap, isMap := addresses[0].(map[string]any)
	if !isMap {
		return "", false
	}

	value, isString := addrMap["value"].(string)
	if !isString || value == "" {
		return "", false
	}

	return value, true
}

// extractGatewayPort extracts the port from the Gateway status listeners.
func extractGatewayPort(gateway *unstructured.Unstructured) int32 {
	status, found, _ := unstructured.NestedMap(gateway.Object, "status")
	if !found {
		return DinDAPIServerPort
	}

	listeners, found, _ := unstructured.NestedSlice(status, "listeners")
	if !found || len(listeners) == 0 {
		return DinDAPIServerPort
	}

	listenerMap, isMap := listeners[0].(map[string]any)
	if !isMap {
		return DinDAPIServerPort
	}

	var port int64
	switch v := listenerMap["port"].(type) {
	case int64:
		port = v
	case float64:
		port = int64(v)
	default:
		return DinDAPIServerPort
	}

	if port < 1 || port > 65535 {
		return DinDAPIServerPort
	}

	return int32(port)
}

// toAnyMap converts a string map to an any map for unstructured objects.
func toAnyMap(m map[string]string) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}

	return result
}
