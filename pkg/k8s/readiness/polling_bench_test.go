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

	for _, sc := range scenarios {
		b.Run(sc.name, func(b *testing.B) {
			b.ReportAllocs()

			// Create fake resources outside the benchmark loop
			resources := make([]readiness.Check, 0, sc.resourceCount)
			clientObjects := make([]runtime.Object, 0, sc.resourceCount)

			for j := 0; j < sc.resourceCount; j++ {
				deploymentName := fmt.Sprintf("test-deployment-%d", j)
				deployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      deploymentName,
						Namespace: "test-ns",
					},
					Status: appsv1.DeploymentStatus{
						Replicas:          1,
						UpdatedReplicas:   1,
						AvailableReplicas: 1,
					},
				}

				clientObjects = append(clientObjects, deployment)
				resources = append(resources, readiness.Check{
					Type:      "deployment",
					Namespace: "test-ns",
					Name:      deploymentName,
				})
			}

			client := fake.NewClientset(clientObjects...)
			ctx := context.Background()
			timeout := 30 * time.Second

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
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

	for _, sc := range scenarios {
		b.Run(sc.name, func(b *testing.B) {
			b.ReportAllocs()

			// Create fake resources outside the benchmark loop
			resources := make([]readiness.Check, 0, sc.deployments+sc.daemonsets)
			clientObjects := make([]runtime.Object, 0, sc.deployments+sc.daemonsets)

			for j := 0; j < sc.deployments; j++ {
				deploymentName := fmt.Sprintf("test-deployment-%d", j)
				deployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      deploymentName,
						Namespace: "test-ns",
					},
					Status: appsv1.DeploymentStatus{
						Replicas:          1,
						UpdatedReplicas:   1,
						AvailableReplicas: 1,
					},
				}

				clientObjects = append(clientObjects, deployment)
				resources = append(resources, readiness.Check{
					Type:      "deployment",
					Namespace: "test-ns",
					Name:      deploymentName,
				})
			}

			for j := 0; j < sc.daemonsets; j++ {
				daemonsetName := fmt.Sprintf("test-daemonset-%d", j)
				daemonset := &appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      daemonsetName,
						Namespace: "kube-system",
					},
					Status: appsv1.DaemonSetStatus{
						DesiredNumberScheduled: 1,
						NumberUnavailable:      0,
						UpdatedNumberScheduled: 1,
					},
				}

				clientObjects = append(clientObjects, daemonset)
				resources = append(resources, readiness.Check{
					Type:      "daemonset",
					Namespace: "kube-system",
					Name:      daemonsetName,
				})
			}

			client := fake.NewClientset(clientObjects...)
			ctx := context.Background()
			timeout := 30 * time.Second

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
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

	for i := 0; i < b.N; i++ {
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

	for i := 0; i < b.N; i++ {
		err := readiness.PollForReadiness(ctx, deadline, pollFunc)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}
