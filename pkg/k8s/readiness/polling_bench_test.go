package readiness_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/k8s/readiness"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// createDeployments creates a list of ready deployment objects and readiness checks for benchmarking.
func createDeployments(count int, namespace string) ([]runtime.Object, []readiness.Check) {
	objects := make([]runtime.Object, 0, count)
	checks := make([]readiness.Check, 0, count)

	for i := range count {
		name := fmt.Sprintf("test-deployment-%d", i)
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Status: appsv1.DeploymentStatus{
				Replicas:          1,
				UpdatedReplicas:   1,
				AvailableReplicas: 1,
			},
		}

		objects = append(objects, deployment)
		checks = append(checks, readiness.Check{
			Type:      "deployment",
			Namespace: namespace,
			Name:      name,
		})
	}

	return objects, checks
}

// createDaemonSets creates a list of ready daemonset objects and readiness checks for benchmarking.
func createDaemonSets(count int, namespace string) ([]runtime.Object, []readiness.Check) {
	objects := make([]runtime.Object, 0, count)
	checks := make([]readiness.Check, 0, count)

	for i := range count {
		name := fmt.Sprintf("test-daemonset-%d", i)
		daemonset := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Status: appsv1.DaemonSetStatus{
				DesiredNumberScheduled: 1,
				NumberUnavailable:      0,
				UpdatedNumberScheduled: 1,
			},
		}

		objects = append(objects, daemonset)
		checks = append(checks, readiness.Check{
			Type:      "daemonset",
			Namespace: namespace,
			Name:      name,
		})
	}

	return objects, checks
}

// BenchmarkWaitForMultipleResources_Sequential benchmarks the current sequential
// implementation of multi-resource polling.
//
// This benchmark establishes a baseline for comparison with potential parallel
// implementations. It measures both execution time and memory allocations.
func BenchmarkWaitForMultipleResources_Sequential(b *testing.B) {
	scenarios := []struct {
		name          string
		resourceCount int
	}{
		{"1_resource", 1},
		{"5_resources", 5},
		{"10_resources", 10},
		{"20_resources", 20},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.ReportAllocs()

			// Create fake resources outside the benchmark loop
			clientObjects, resources := createDeployments(scenario.resourceCount, "test-ns")

			client := fake.NewClientset(clientObjects...)
			ctx := context.Background()
			timeout := 30 * time.Second

			b.ResetTimer()

			for range b.N {
				err := readiness.WaitForMultipleResources(ctx, client, resources, timeout)
				if err != nil {
					b.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// BenchmarkWaitForMultipleResources_MixedTypes benchmarks polling with mixed
// resource types (deployments and daemonsets).
//
// This benchmark tests realistic scenarios where CNI and other components
// include both deployment and daemonset resources.
func BenchmarkWaitForMultipleResources_MixedTypes(b *testing.B) {
	scenarios := []struct {
		name        string
		deployments int
		daemonsets  int
	}{
		{"2d_2ds", 2, 2},
		{"5d_5ds", 5, 5},
		{"10d_10ds", 10, 10},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.ReportAllocs()

			// Create fake resources outside the benchmark loop
			deployObjects, deployChecks := createDeployments(scenario.deployments, "test-ns")
			daemonObjects, daemonChecks := createDaemonSets(scenario.daemonsets, "kube-system")

			clientObjects := make([]runtime.Object, 0, len(deployObjects)+len(daemonObjects))
			clientObjects = append(clientObjects, deployObjects...)
			clientObjects = append(clientObjects, daemonObjects...)

			resources := make([]readiness.Check, 0, len(deployChecks)+len(daemonChecks))
			resources = append(resources, deployChecks...)
			resources = append(resources, daemonChecks...)

			client := fake.NewClientset(clientObjects...)
			ctx := context.Background()
			timeout := 30 * time.Second

			b.ResetTimer()

			for range b.N {
				err := readiness.WaitForMultipleResources(ctx, client, resources, timeout)
				if err != nil {
					b.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// BenchmarkWaitForMultipleResources_RealWorldCNI benchmarks a realistic CNI
// installation scenario with typical Cilium components.
//
// This simulates the actual workload when installing a CNI like Cilium which
// includes operator deployments and agent daemonsets.
func BenchmarkWaitForMultipleResources_RealWorldCNI(b *testing.B) {
	b.ReportAllocs()

	// Typical Cilium installation has:
	// - cilium-operator deployment
	// - cilium-agent daemonset
	// - coredns deployment (updated after CNI)
	resources := []readiness.Check{
		{Type: "deployment", Namespace: "kube-system", Name: "cilium-operator"},
		{Type: "daemonset", Namespace: "kube-system", Name: "cilium"},
		{Type: "deployment", Namespace: "kube-system", Name: "coredns"},
	}

	clientObjects := []runtime.Object{
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cilium-operator",
				Namespace: "kube-system",
			},
			Status: appsv1.DeploymentStatus{
				Replicas:          2,
				UpdatedReplicas:   2,
				AvailableReplicas: 2,
			},
		},
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cilium",
				Namespace: "kube-system",
			},
			Status: appsv1.DaemonSetStatus{
				DesiredNumberScheduled: 3, // 3 nodes
				NumberUnavailable:      0,
				UpdatedNumberScheduled: 3,
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "coredns",
				Namespace: "kube-system",
			},
			Status: appsv1.DeploymentStatus{
				Replicas:          2,
				UpdatedReplicas:   2,
				AvailableReplicas: 2,
			},
		},
	}

	client := fake.NewClientset(clientObjects...)
	ctx := context.Background()
	timeout := 30 * time.Second

	b.ResetTimer()

	for range b.N {
		err := readiness.WaitForMultipleResources(ctx, client, resources, timeout)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// BenchmarkPollForReadiness_SingleCheck benchmarks the base polling function
// with a simple ready check.
//
// This benchmark measures the overhead of the polling mechanism itself,
// providing a baseline for understanding the cost of each polling operation.
func BenchmarkPollForReadiness_SingleCheck(b *testing.B) {
	b.ReportAllocs()

	ctx := context.Background()
	deadline := 5 * time.Second

	// Create a poll function that returns ready immediately
	pollFunc := func(_ context.Context) (bool, error) {
		return true, nil
	}

	b.ResetTimer()

	for range b.N {
		err := readiness.PollForReadiness(ctx, deadline, pollFunc)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}
