package kubernetes_test

import (
	"testing"

	kubeprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

//nolint:funlen // Table-driven test with many variants for a pure function.
func TestPreserveImmutableServiceFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		serviceType     corev1.ServiceType
		svcPorts        []corev1.ServicePort
		existingPorts   []corev1.ServicePort
		existingCluster string
		wantClusterIP   string
		wantNodePorts   map[string]int32
	}{
		{
			name:            "copies_cluster_ip_for_clusterip_service",
			serviceType:     corev1.ServiceTypeClusterIP,
			svcPorts:        []corev1.ServicePort{{Name: "api", Port: 6443}},
			existingPorts:   []corev1.ServicePort{{Name: "api", Port: 6443, NodePort: 30001}},
			existingCluster: "10.96.0.1",
			wantClusterIP:   "10.96.0.1",
			wantNodePorts:   map[string]int32{"api": 0}, // NodePort not copied for ClusterIP
		},
		{
			name:            "copies_node_port_for_nodeport_service",
			serviceType:     corev1.ServiceTypeNodePort,
			svcPorts:        []corev1.ServicePort{{Name: "api", Port: 6443}},
			existingPorts:   []corev1.ServicePort{{Name: "api", Port: 6443, NodePort: 30001}},
			existingCluster: "10.96.0.1",
			wantClusterIP:   "10.96.0.1",
			wantNodePorts:   map[string]int32{"api": 30001},
		},
		{
			name:            "copies_node_port_for_loadbalancer_service",
			serviceType:     corev1.ServiceTypeLoadBalancer,
			svcPorts:        []corev1.ServicePort{{Name: "api", Port: 6443}},
			existingPorts:   []corev1.ServicePort{{Name: "api", Port: 6443, NodePort: 30001}},
			existingCluster: "10.96.0.1",
			wantClusterIP:   "10.96.0.1",
			wantNodePorts:   map[string]int32{"api": 30001},
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
			existingCluster: "10.96.0.2",
			wantClusterIP:   "10.96.0.2",
			wantNodePorts:   map[string]int32{"api": 0},
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
			existingCluster: "10.96.0.3",
			wantClusterIP:   "10.96.0.3",
			wantNodePorts:   map[string]int32{"metrics": 30002, "api": 30001},
		},
		{
			name:        "unmatched_port_name_leaves_node_port_zero",
			serviceType: corev1.ServiceTypeNodePort,
			svcPorts:    []corev1.ServicePort{{Name: "api", Port: 6443}},
			existingPorts: []corev1.ServicePort{
				{Name: "other", Port: 8080, NodePort: 30999},
			},
			existingCluster: "10.96.0.4",
			wantClusterIP:   "10.96.0.4",
			wantNodePorts:   map[string]int32{"api": 0},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := &corev1.Service{
				Spec: corev1.ServiceSpec{
					Ports: tc.svcPorts,
				},
			}
			existing := &corev1.Service{
				Spec: corev1.ServiceSpec{
					ClusterIP: tc.existingCluster,
					Ports:     tc.existingPorts,
				},
			}

			kubeprovider.PreserveImmutableServiceFieldsForTest(svc, existing, tc.serviceType)

			assert.Equal(t, tc.wantClusterIP, svc.Spec.ClusterIP, "ClusterIP should be copied from existing")
			for portName, wantNodePort := range tc.wantNodePorts {
				for i := range svc.Spec.Ports {
					if svc.Spec.Ports[i].Name == portName {
						assert.Equal(t, wantNodePort, svc.Spec.Ports[i].NodePort,
							"NodePort for %q should be %d", portName, wantNodePort)
					}
				}
			}
		})
	}
}
