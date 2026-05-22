package kubernetes_test

import (
	"testing"

	kubeprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func ipFamilyPolicyPtr(p corev1.IPFamilyPolicy) *corev1.IPFamilyPolicy { return new(p) }

//nolint:funlen // Table-driven test with many variants for a pure function.
func TestPreserveImmutableServiceFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		serviceType            corev1.ServiceType
		svcPorts               []corev1.ServicePort
		existingPorts          []corev1.ServicePort
		existingClusterIP      string
		existingClusterIPs     []string
		existingIPFamilies     []corev1.IPFamily
		existingIPFamilyPolicy *corev1.IPFamilyPolicy
		wantClusterIP          string
		wantClusterIPs         []string
		wantIPFamilies         []corev1.IPFamily
		wantIPFamilyPolicy     *corev1.IPFamilyPolicy
		wantNodePorts          map[string]int32
	}{
		{
			name:              "copies_cluster_ip_for_clusterip_service",
			serviceType:       corev1.ServiceTypeClusterIP,
			svcPorts:          []corev1.ServicePort{{Name: "api", Port: 6443}},
			existingPorts:     []corev1.ServicePort{{Name: "api", Port: 6443, NodePort: 30001}},
			existingClusterIP: "10.96.0.1",
			wantClusterIP:     "10.96.0.1",
			wantNodePorts:     map[string]int32{"api": 0}, // NodePort not copied for ClusterIP
		},
		{
			name:              "copies_node_port_for_nodeport_service",
			serviceType:       corev1.ServiceTypeNodePort,
			svcPorts:          []corev1.ServicePort{{Name: "api", Port: 6443}},
			existingPorts:     []corev1.ServicePort{{Name: "api", Port: 6443, NodePort: 30001}},
			existingClusterIP: "10.96.0.1",
			wantClusterIP:     "10.96.0.1",
			wantNodePorts:     map[string]int32{"api": 30001},
		},
		{
			name:              "copies_node_port_for_loadbalancer_service",
			serviceType:       corev1.ServiceTypeLoadBalancer,
			svcPorts:          []corev1.ServicePort{{Name: "api", Port: 6443}},
			existingPorts:     []corev1.ServicePort{{Name: "api", Port: 6443, NodePort: 30001}},
			existingClusterIP: "10.96.0.1",
			wantClusterIP:     "10.96.0.1",
			wantNodePorts:     map[string]int32{"api": 30001},
		},
		{
			name:        "does_not_copy_node_port_when_existing_is_zero",
			serviceType: corev1.ServiceTypeNodePort,
			svcPorts:    []corev1.ServicePort{{Name: "api", Port: 6443}},
			existingPorts: []corev1.ServicePort{{
				Name:     "api",
				Port:     6443,
				NodePort: 0, // zero means not yet allocated
			}},
			existingClusterIP: "10.96.0.2",
			wantClusterIP:     "10.96.0.2",
			wantNodePorts:     map[string]int32{"api": 0},
		},
		{
			name:        "matches_port_by_name_not_by_index",
			serviceType: corev1.ServiceTypeNodePort,
			svcPorts: []corev1.ServicePort{
				{Name: "metrics", Port: 9090},
				{Name: "api", Port: 6443},
			},
			existingPorts: []corev1.ServicePort{
				{Name: "api", Port: 6443, NodePort: 30001},
				{Name: "metrics", Port: 9090, NodePort: 30002},
			},
			existingClusterIP: "10.96.0.3",
			wantClusterIP:     "10.96.0.3",
			wantNodePorts:     map[string]int32{"metrics": 30002, "api": 30001},
		},
		{
			name:        "unmatched_port_name_leaves_node_port_zero",
			serviceType: corev1.ServiceTypeNodePort,
			svcPorts:    []corev1.ServicePort{{Name: "api", Port: 6443}},
			existingPorts: []corev1.ServicePort{
				{Name: "other", Port: 8080, NodePort: 30999},
			},
			existingClusterIP: "10.96.0.4",
			wantClusterIP:     "10.96.0.4",
			wantNodePorts:     map[string]int32{"api": 0},
		},
		{
			name:                   "copies_cluster_ips_ip_families_and_policy_for_dual_stack",
			serviceType:            corev1.ServiceTypeClusterIP,
			svcPorts:               []corev1.ServicePort{{Name: "api", Port: 6443}},
			existingPorts:          []corev1.ServicePort{{Name: "api", Port: 6443}},
			existingClusterIP:      "10.96.0.5",
			existingClusterIPs:     []string{"10.96.0.5", "fd00::1"},
			existingIPFamilies:     []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			existingIPFamilyPolicy: ipFamilyPolicyPtr(corev1.IPFamilyPolicyRequireDualStack),
			wantClusterIP:          "10.96.0.5",
			wantClusterIPs:         []string{"10.96.0.5", "fd00::1"},
			wantIPFamilies:         []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			wantIPFamilyPolicy:     ipFamilyPolicyPtr(corev1.IPFamilyPolicyRequireDualStack),
			wantNodePorts:          map[string]int32{},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			svc := &corev1.Service{
				Spec: corev1.ServiceSpec{
					Ports: testCase.svcPorts,
				},
			}
			existing := &corev1.Service{
				Spec: corev1.ServiceSpec{
					ClusterIP:      testCase.existingClusterIP,
					ClusterIPs:     testCase.existingClusterIPs,
					IPFamilies:     testCase.existingIPFamilies,
					IPFamilyPolicy: testCase.existingIPFamilyPolicy,
					Ports:          testCase.existingPorts,
				},
			}

			kubeprovider.PreserveImmutableServiceFieldsForTest(svc, existing, testCase.serviceType)

			assert.Equal(
				t,
				testCase.wantClusterIP,
				svc.Spec.ClusterIP,
				"ClusterIP should be copied from existing",
			)
			assert.Equal(
				t,
				testCase.wantClusterIPs,
				svc.Spec.ClusterIPs,
				"ClusterIPs should be copied from existing",
			)
			assert.Equal(
				t,
				testCase.wantIPFamilies,
				svc.Spec.IPFamilies,
				"IPFamilies should be copied from existing",
			)
			assert.Equal(
				t,
				testCase.wantIPFamilyPolicy,
				svc.Spec.IPFamilyPolicy,
				"IPFamilyPolicy should be copied from existing",
			)

			for portName, wantNodePort := range testCase.wantNodePorts {
				found := false

				for i := range svc.Spec.Ports {
					if svc.Spec.Ports[i].Name == portName {
						assert.Equal(t, wantNodePort, svc.Spec.Ports[i].NodePort,
							"NodePort for %q should be %d", portName, wantNodePort)

						found = true

						break
					}
				}

				if !found {
					assert.Fail(t, "port not found in service spec",
						"expected port %q to be present", portName)
				}
			}
		})
	}
}
