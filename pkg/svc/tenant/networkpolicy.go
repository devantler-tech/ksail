package tenant

import (
	"fmt"

	"sigs.k8s.io/yaml"
)

const dnsPort = 53

// GenerateNetworkPolicyManifests generates default-deny NetworkPolicies plus
// DNS and intra-namespace allow rules for each namespace. Returns a map with a
// single "networkpolicy.yaml" (multi-doc) entry, or (nil, nil) when
// WithNetworkPolicy is false. The flavor (native vs Cilium) is selected by
// opts.NetworkPolicyEngine.
func GenerateNetworkPolicyManifests(opts Options) (map[string]string, error) {
	if !opts.WithNetworkPolicy {
		return map[string]string{}, nil
	}

	if len(opts.Namespaces) == 0 {
		return nil, fmt.Errorf("%w", ErrNamespaceRequired)
	}

	var (
		content string
		err     error
	)

	switch opts.NetworkPolicyEngine {
	case NetworkPolicyEngineCilium:
		content, err = marshalCiliumNetworkPolicies(opts)
	case NetworkPolicyEngineNative, "":
		content, err = marshalNativeNetworkPolicies(opts)
	default:
		return nil, fmt.Errorf("%w: %q",
			ErrUnsupportedNetworkPolicyEngine, opts.NetworkPolicyEngine)
	}

	if err != nil {
		return nil, err
	}

	return map[string]string{"networkpolicy.yaml": content}, nil
}

func marshalNativeNetworkPolicies(opts Options) (string, error) {
	docs := make([]string, 0, len(opts.Namespaces)*3) //nolint:mnd // 3 policies per namespace

	for _, namespace := range opts.Namespaces {
		for _, policy := range []map[string]any{
			nativeDefaultDeny(namespace),
			nativeAllowDNS(namespace),
			nativeAllowIntraNamespace(namespace),
		} {
			out, err := yaml.Marshal(policy)
			if err != nil {
				return "", fmt.Errorf("marshal network policy: %w", err)
			}

			docs = append(docs, string(out))
		}
	}

	return joinDocs(docs), nil
}

func nativeNetworkPolicy(name, namespace string, spec map[string]any) map[string]any {
	return map[string]any{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "NetworkPolicy",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"labels":    ManagedByLabels(),
		},
		"spec": spec,
	}
}

func nativeDefaultDeny(namespace string) map[string]any {
	return nativeNetworkPolicy("default-deny", namespace, map[string]any{
		"podSelector": map[string]any{},
		"policyTypes": []string{"Ingress", "Egress"},
	})
}

func nativeAllowDNS(namespace string) map[string]any {
	return nativeNetworkPolicy("allow-dns", namespace, map[string]any{
		"podSelector": map[string]any{},
		"policyTypes": []string{"Egress"},
		"egress": []map[string]any{
			{
				"to": []map[string]any{
					{
						"namespaceSelector": map[string]any{
							"matchLabels": map[string]any{
								"kubernetes.io/metadata.name": "kube-system",
							},
						},
					},
				},
				"ports": []map[string]any{
					{"protocol": "UDP", "port": dnsPort},
					{"protocol": "TCP", "port": dnsPort},
				},
			},
		},
	})
}

func nativeAllowIntraNamespace(namespace string) map[string]any {
	return nativeNetworkPolicy("allow-intra-namespace", namespace, map[string]any{
		"podSelector": map[string]any{},
		"policyTypes": []string{"Ingress", "Egress"},
		"ingress": []map[string]any{
			{"from": []map[string]any{{"podSelector": map[string]any{}}}},
		},
		"egress": []map[string]any{
			{"to": []map[string]any{{"podSelector": map[string]any{}}}},
		},
	})
}

func marshalCiliumNetworkPolicies(opts Options) (string, error) {
	docs := make([]string, 0, len(opts.Namespaces))

	for _, namespace := range opts.Namespaces {
		out, err := yaml.Marshal(ciliumNetworkPolicy(namespace))
		if err != nil {
			return "", fmt.Errorf("marshal cilium network policy: %w", err)
		}

		docs = append(docs, string(out))
	}

	return joinDocs(docs), nil
}

// ciliumNetworkPolicy returns a CiliumNetworkPolicy that selects all endpoints
// in the namespace (switching them to default-deny) while allowing
// intra-namespace traffic and DNS to kube-dns. It is intentionally a simple
// starting point that platform teams can extend.
func ciliumNetworkPolicy(namespace string) map[string]any {
	intraNamespace := map[string]any{
		"matchLabels": map[string]any{"k8s:io.kubernetes.pod.namespace": namespace},
	}

	return map[string]any{
		"apiVersion": "cilium.io/v2",
		"kind":       "CiliumNetworkPolicy",
		"metadata": map[string]any{
			"name":      "tenant-isolation",
			"namespace": namespace,
			"labels":    ManagedByLabels(),
		},
		"spec": map[string]any{
			"endpointSelector": map[string]any{},
			"ingress": []map[string]any{
				{"fromEndpoints": []map[string]any{intraNamespace}},
			},
			"egress": []map[string]any{
				{"toEndpoints": []map[string]any{intraNamespace}},
				{
					"toEndpoints": []map[string]any{
						{
							"matchLabels": map[string]any{
								"k8s:io.kubernetes.pod.namespace": "kube-system",
								"k8s:k8s-app":                     "kube-dns",
							},
						},
					},
					"toPorts": []map[string]any{
						{
							"ports": []map[string]any{
								{"port": "53", "protocol": "UDP"},
								{"port": "53", "protocol": "TCP"},
							},
						},
					},
				},
			},
		},
	}
}
