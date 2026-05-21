package kubernetes

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
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
	// APIServiceName is the name of the Service that targets the nested API server port.
	APIServiceName = "apiserver"

	// GatewayName is the name of the Gateway resource created for API exposure.
	GatewayName = "ksail-apiserver"

	// TCPRouteName is the name of the TCPRoute resource.
	TCPRouteName = "ksail-apiserver"

	// gatewayNameKey is the JSON key for Gateway name in status.
	gatewayNameKey = "name"

	// gatewayReadyPollInterval is the interval between Gateway status checks.
	gatewayReadyPollInterval = 3 * time.Second

	// gatewayReadyTimeout is the maximum time to wait for Gateway address assignment.
	gatewayReadyTimeout = 120 * time.Second

	// lbReadyPollInterval is the interval between LoadBalancer Service status checks.
	lbReadyPollInterval = 3 * time.Second

	// lbReadyTimeout is the maximum time to wait for a LoadBalancer address. Kept short so the
	// NodePort fallback kicks in quickly when the host cluster has no LoadBalancer controller.
	lbReadyTimeout = 30 * time.Second
)

// Exposure kinds reported by ResolveExposure.
const (
	// ExposureGateway means the nested API server is exposed via a Gateway API Gateway + TCPRoute.
	ExposureGateway = "gateway"
	// ExposureLoadBalancer means it is exposed via a LoadBalancer Service.
	ExposureLoadBalancer = "loadbalancer"
	// ExposureNodePort means it is exposed via a NodePort Service.
	ExposureNodePort = "nodeport"
)

// APIExposureSpec describes how to expose a nested cluster's API server on the host cluster.
// It generalizes the exposure machinery beyond the DinD model so it also works for distributions
// that manage their own namespace and backend pods (k3k, vCluster).
type APIExposureSpec struct {
	// ClusterName is the nested cluster name (used for labels).
	ClusterName string
	// Namespace is the host-cluster namespace the exposure resources live in.
	// Defaults to NamespaceName(ClusterName) when empty.
	Namespace string
	// BackendSelector selects the pod(s) backing the API server Service.
	// Defaults to the DinD pod selector when nil.
	BackendSelector map[string]string
	// APIPort is the port the nested API server listens on.
	APIPort int32
	// GatewayClassName, when set, makes the Gateway API the preferred exposure tier.
	GatewayClassName string
	// HostAddress is the host cluster's reachable address (typically derived from the host
	// REST config). Used as a NodePort address fallback when no node ExternalIP is available.
	HostAddress string
	// SkipLoadBalancer, when true, skips the LoadBalancer exposure tier and falls through
	// directly to NodePort. Use this when the host cluster's LB controller (e.g. K3s
	// klipper-lb) would bind the API port on the node, conflicting with the host API server.
	SkipLoadBalancer bool
}

// ExposureResult is the resolved, stable endpoint for a nested cluster's API server.
type ExposureResult struct {
	// Address is the host/IP clients should connect to.
	Address string
	// Port is the port clients should connect to.
	Port int32
	// Kind is one of ExposureGateway, ExposureLoadBalancer, or ExposureNodePort.
	Kind string
}

// ServerURL returns the kubeconfig server URL for the resolved exposure.
func (r *ExposureResult) ServerURL() string {
	return "https://" + net.JoinHostPort(r.Address, strconv.FormatInt(int64(r.Port), 10))
}

// withDefaults fills in namespace and backend selector defaults for the DinD model.
func (s APIExposureSpec) withDefaults() APIExposureSpec {
	if s.Namespace == "" {
		s.Namespace = NamespaceName(s.ClusterName)
	}

	if s.BackendSelector == nil {
		s.BackendSelector = map[string]string{LabelApp: DinDPodName}
	}

	return s
}

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

// ResolveExposure creates a stable, server-side exposure for the nested cluster's API server and
// returns its address. It tries, in order:
//  1. Gateway API (when GatewayClassName is set and a TCPRoute-capable controller assigns an address),
//  2. a LoadBalancer Service (when SkipLoadBalancer is false and the host cluster assigns an external address),
//  3. a NodePort Service (universal last resort).
//
// When GatewayClassName is set the Gateway tier is preferred, but a Gateway failure still falls
// back to LoadBalancer/NodePort (a warning is emitted so the failure is not hidden). A nil dynamic
// client with GatewayClassName set is treated as a wiring error.
//
// The returned address survives the CLI process exit and should be written to the kubeconfig and
// added to the nested API server's certificate SANs.
func (p *Provider) ResolveExposure(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	spec APIExposureSpec,
) (*ExposureResult, error) {
	spec = spec.withDefaults()

	if spec.GatewayClassName != "" {
		if dynamicClient == nil {
			return nil, fmt.Errorf(
				"gateway exposure requested for %q: %w",
				spec.ClusterName,
				ErrDynamicClientRequired,
			)
		}

		result, err := p.exposeViaGateway(ctx, dynamicClient, spec)
		if err == nil {
			return result, nil
		}

		// Surface the Gateway failure (e.g. missing CRDs / GatewayClass) instead of hiding it,
		// then fall back to the LoadBalancer/NodePort tiers.
		_, _ = fmt.Fprintf(
			os.Stderr,
			"warning: Gateway API exposure failed (%v); falling back to LoadBalancer/NodePort\n",
			err,
		)
	}

	if !spec.SkipLoadBalancer {
		result, err := p.exposeViaLoadBalancer(ctx, spec)
		if err == nil {
			return result, nil
		}
	}

	return p.exposeViaNodePort(ctx, spec)
}

// exposeViaGateway ensures a ClusterIP Service, Gateway, and TCPRoute, then waits for the
// Gateway to be assigned an external address.
func (p *Provider) exposeViaGateway(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	spec APIExposureSpec,
) (*ExposureResult, error) {
	_, err := p.ensureService(ctx, spec, corev1.ServiceTypeClusterIP)
	if err != nil {
		return nil, err
	}

	err = p.ensureGateway(ctx, dynamicClient, spec)
	if err != nil {
		return nil, err
	}

	err = p.ensureTCPRoute(ctx, dynamicClient, spec)
	if err != nil {
		return nil, err
	}

	addr, port, err := p.WaitForGateway(ctx, dynamicClient, spec.Namespace)
	if err != nil {
		return nil, err
	}

	return &ExposureResult{Address: addr, Port: port, Kind: ExposureGateway}, nil
}

// exposeViaLoadBalancer ensures a LoadBalancer Service and waits for an external address.
func (p *Provider) exposeViaLoadBalancer(
	ctx context.Context,
	spec APIExposureSpec,
) (*ExposureResult, error) {
	_, err := p.ensureService(ctx, spec, corev1.ServiceTypeLoadBalancer)
	if err != nil {
		return nil, err
	}

	addr, err := p.waitForLoadBalancer(ctx, spec.Namespace)
	if err != nil {
		return nil, err
	}

	return &ExposureResult{Address: addr, Port: spec.APIPort, Kind: ExposureLoadBalancer}, nil
}

// exposeViaNodePort ensures a NodePort Service and resolves a reachable node address.
func (p *Provider) exposeViaNodePort(
	ctx context.Context,
	spec APIExposureSpec,
) (*ExposureResult, error) {
	svc, err := p.ensureService(ctx, spec, corev1.ServiceTypeNodePort)
	if err != nil {
		return nil, err
	}

	if len(svc.Spec.Ports) == 0 || svc.Spec.Ports[0].NodePort == 0 {
		return nil, ErrNodePortNotAssigned
	}

	addr, err := p.pickNodeAddress(ctx, spec.HostAddress)
	if err != nil {
		return nil, err
	}

	return &ExposureResult{
		Address: addr,
		Port:    svc.Spec.Ports[0].NodePort,
		Kind:    ExposureNodePort,
	}, nil
}

// ensureService creates or updates the API server Service with the given type and selector.
func (p *Provider) ensureService(
	ctx context.Context,
	spec APIExposureSpec,
	serviceType corev1.ServiceType,
) (*corev1.Service, error) {
	svc := buildAPIService(spec, serviceType)

	created, err := p.client.CoreV1().
		Services(spec.Namespace).
		Create(ctx, svc, metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		existing, getErr := p.client.CoreV1().
			Services(spec.Namespace).
			Get(ctx, APIServiceName, metav1.GetOptions{})
		if getErr != nil {
			return nil, fmt.Errorf("get existing API server service: %w", getErr)
		}

		svc.ResourceVersion = existing.ResourceVersion
		preserveImmutableServiceFields(svc, existing, serviceType)

		created, err = p.client.CoreV1().
			Services(spec.Namespace).
			Update(ctx, svc, metav1.UpdateOptions{})
	}

	if err != nil {
		return nil, fmt.Errorf("ensure API server service: %w", err)
	}

	return created, nil
}

// preserveImmutableServiceFields copies cluster-assigned, immutable Service networking fields from
// an existing Service onto the desired spec so an Update is not rejected for changing them.
func preserveImmutableServiceFields(
	svc, existing *corev1.Service,
	serviceType corev1.ServiceType,
) {
	// ClusterIP(s) and IP-family settings are immutable / cluster-defaulted (notably on dual-stack
	// clusters); omitting them on update can be rejected as an immutable-field change.
	svc.Spec.ClusterIP = existing.Spec.ClusterIP
	svc.Spec.ClusterIPs = existing.Spec.ClusterIPs
	svc.Spec.IPFamilies = existing.Spec.IPFamilies
	svc.Spec.IPFamilyPolicy = existing.Spec.IPFamilyPolicy

	// Allocated node ports only exist for NodePort/LoadBalancer Services; copying a nodePort onto a
	// ClusterIP Service (e.g. when switching to the Gateway tier) would be rejected by the API.
	if serviceType != corev1.ServiceTypeNodePort && serviceType != corev1.ServiceTypeLoadBalancer {
		return
	}

	for i := range svc.Spec.Ports {
		for _, ep := range existing.Spec.Ports {
			if svc.Spec.Ports[i].Name == ep.Name && ep.NodePort != 0 {
				svc.Spec.Ports[i].NodePort = ep.NodePort
			}
		}
	}
}

func (p *Provider) ensureGateway(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	spec APIExposureSpec,
) error {
	gateway := buildGateway(spec)

	_, err := dynamicClient.Resource(gatewayGVR()).Namespace(spec.Namespace).Create(
		ctx, gateway, metav1.CreateOptions{},
	)
	if errors.IsAlreadyExists(err) {
		existing, getErr := dynamicClient.Resource(gatewayGVR()).Namespace(spec.Namespace).Get(
			ctx, GatewayName, metav1.GetOptions{},
		)
		if getErr != nil {
			return fmt.Errorf("get existing Gateway: %w", getErr)
		}

		gateway.SetResourceVersion(existing.GetResourceVersion())
		_, err = dynamicClient.Resource(gatewayGVR()).Namespace(spec.Namespace).Update(
			ctx, gateway, metav1.UpdateOptions{},
		)
	}

	if err != nil {
		return fmt.Errorf("ensure Gateway: %w", err)
	}

	return nil
}

func (p *Provider) ensureTCPRoute(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	spec APIExposureSpec,
) error {
	route := buildTCPRoute(spec)

	_, err := dynamicClient.Resource(tcpRouteGVR()).Namespace(spec.Namespace).Create(
		ctx, route, metav1.CreateOptions{},
	)
	if errors.IsAlreadyExists(err) {
		existing, getErr := dynamicClient.Resource(tcpRouteGVR()).Namespace(spec.Namespace).Get(
			ctx, TCPRouteName, metav1.GetOptions{},
		)
		if getErr != nil {
			return fmt.Errorf("get existing TCPRoute: %w", getErr)
		}

		route.SetResourceVersion(existing.GetResourceVersion())
		_, err = dynamicClient.Resource(tcpRouteGVR()).Namespace(spec.Namespace).Update(
			ctx, route, metav1.UpdateOptions{},
		)
	}

	if err != nil {
		return fmt.Errorf("ensure TCPRoute: %w", err)
	}

	return nil
}

// WaitForGateway waits for the Gateway in the given namespace to be assigned an external address.
// Returns the address (IP or hostname) and port.
func (p *Provider) WaitForGateway(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	namespace string,
) (string, int32, error) {
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

// waitForLoadBalancer waits for the API server LoadBalancer Service to be assigned an ingress
// address (IP or hostname).
func (p *Provider) waitForLoadBalancer(ctx context.Context, namespace string) (string, error) {
	deadline := time.Now().Add(lbReadyTimeout)

	for time.Now().Before(deadline) {
		svc, err := p.client.CoreV1().
			Services(namespace).
			Get(ctx, APIServiceName, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("get LoadBalancer service: %w", err)
		}

		for _, ingress := range svc.Status.LoadBalancer.Ingress {
			if ingress.IP != "" {
				return ingress.IP, nil
			}

			if ingress.Hostname != "" {
				return ingress.Hostname, nil
			}
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("waiting for LoadBalancer: %w", ctx.Err())
		case <-time.After(lbReadyPollInterval):
		}
	}

	return "", ErrLoadBalancerNotReady
}

// pickNodeAddress chooses a host-reachable node address for NodePort exposure.
// Precedence: a node ExternalIP, then the host derived from the host REST config
// (known-reachable since KSail uses it), then a node InternalIP.
func (p *Provider) pickNodeAddress(ctx context.Context, hostAddress string) (string, error) {
	nodes, listErr := p.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if listErr == nil {
		if addr := firstNodeAddress(nodes.Items, corev1.NodeExternalIP); addr != "" {
			return addr, nil
		}
	}

	if host := hostnameOnly(hostAddress); host != "" {
		// Skip unspecified (wildcard) addresses like 0.0.0.0 or :: — they are valid
		// bind addresses for the host cluster's API server but are not routable from
		// clients that load the nested kubeconfig.
		if ip := net.ParseIP(host); ip == nil || !ip.IsUnspecified() {
			return host, nil
		}
	}

	if listErr == nil {
		if addr := firstNodeAddress(nodes.Items, corev1.NodeInternalIP); addr != "" {
			return addr, nil
		}
	}

	if listErr != nil {
		return "", fmt.Errorf("list nodes for NodePort address: %w", listErr)
	}

	return "", ErrNoNodeAddress
}

// firstNodeAddress returns the first node address of the given type, or "".
func firstNodeAddress(nodes []corev1.Node, addrType corev1.NodeAddressType) string {
	for i := range nodes {
		for _, addr := range nodes[i].Status.Addresses {
			if addr.Type == addrType && addr.Address != "" {
				return addr.Address
			}
		}
	}

	return ""
}

// hostnameOnly extracts the host portion (no scheme, no port) from a host address that may be a
// URL (https://host:port), a host:port pair, or a bare host.
func hostnameOnly(hostAddress string) string {
	if hostAddress == "" {
		return ""
	}

	parsed, err := url.Parse(hostAddress)
	if err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}

	host, _, err := net.SplitHostPort(hostAddress)
	if err == nil && host != "" {
		return host
	}

	return hostAddress
}

// UpdateAPIServiceTargetPort updates the target port of the API server Service so it routes to the
// port the nested API server is actually published on. This is used when that port is only known
// after the cluster is created (e.g. Talos's dynamically-mapped DinD port), while the exposure
// address/port the kubeconfig points at were resolved up-front.
func (p *Provider) UpdateAPIServiceTargetPort(
	ctx context.Context,
	clusterName string,
	targetPort int32,
) error {
	namespace := NamespaceName(clusterName)

	svc, err := p.client.CoreV1().Services(namespace).Get(ctx, APIServiceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get API service: %w", err)
	}

	if len(svc.Spec.Ports) == 0 {
		return fmt.Errorf("update API exposure target port: %w", ErrNoServicePorts)
	}

	svc.Spec.Ports[0].TargetPort = intstr.FromInt32(targetPort)

	_, err = p.client.CoreV1().Services(namespace).Update(ctx, svc, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("update API service target port: %w", err)
	}

	return nil
}

// DeleteAPIExposure removes all API exposure resources (Service, Gateway, TCPRoute) for a
// DinD-model cluster. For distributions that delete their whole namespace (k3k, vCluster) the
// namespace deletion already cascades these resources, so this call is defensive there.
func (p *Provider) DeleteAPIExposure(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	clusterName string,
) error {
	namespace := NamespaceName(clusterName)

	// Delete TCPRoute + Gateway (only present on the Gateway path).
	if dynamicClient != nil {
		err := dynamicClient.Resource(tcpRouteGVR()).Namespace(namespace).Delete(
			ctx, TCPRouteName, metav1.DeleteOptions{},
		)
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete TCPRoute: %w", err)
		}

		err = dynamicClient.Resource(gatewayGVR()).Namespace(namespace).Delete(
			ctx, GatewayName, metav1.DeleteOptions{},
		)
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete Gateway: %w", err)
		}
	}

	err := p.client.CoreV1().Services(namespace).Delete(ctx, APIServiceName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete API service: %w", err)
	}

	return nil
}

func buildAPIService(spec APIExposureSpec, serviceType corev1.ServiceType) *corev1.Service {
	labels := CommonLabels(spec.ClusterName)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:   APIServiceName,
			Labels: labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     serviceType,
			Selector: spec.BackendSelector,
			Ports: []corev1.ServicePort{
				{
					Name:       "https",
					Port:       spec.APIPort,
					TargetPort: intstr.FromInt32(spec.APIPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

func buildGateway(spec APIExposureSpec) *unstructured.Unstructured {
	labels := CommonLabels(spec.ClusterName)

	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]any{
				gatewayNameKey: GatewayName,
				"labels":       toAnyMap(labels),
			},
			"spec": map[string]any{
				"gatewayClassName": spec.GatewayClassName,
				"listeners": []any{
					map[string]any{
						gatewayNameKey: APIServiceName,
						"protocol":     "TCP",
						"port":         int64(spec.APIPort),
					},
				},
			},
		},
	}
}

func buildTCPRoute(spec APIExposureSpec) *unstructured.Unstructured {
	labels := CommonLabels(spec.ClusterName)

	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "gateway.networking.k8s.io/v1alpha2",
			"kind":       "TCPRoute",
			"metadata": map[string]any{
				gatewayNameKey: TCPRouteName,
				"labels":       toAnyMap(labels),
			},
			"spec": map[string]any{
				"parentRefs": []any{
					map[string]any{
						gatewayNameKey: GatewayName,
						"namespace":    spec.Namespace,
					},
				},
				"rules": []any{
					map[string]any{
						"backendRefs": []any{
							map[string]any{
								"name": APIServiceName,
								"port": int64(spec.APIPort),
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
