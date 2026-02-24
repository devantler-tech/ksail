package argocd_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/client/argocd"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// Benchmark helpers for manager creation.

func setupBenchmarkManager(b *testing.B) *argocd.ManagerImpl {
	b.Helper()

	clientset := k8sfake.NewClientset()
	gvr := schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "applications",
	}
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{gvr: "ApplicationList"},
	)

	return argocd.NewManager(clientset, dyn)
}

func setupBenchmarkManagerWithApp(b *testing.B, appName string) *argocd.ManagerImpl {
	b.Helper()

	clientset := k8sfake.NewClientset()
	gvr := schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "applications",
	}
	scheme := runtime.NewScheme()

	app := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":      appName,
			"namespace": "argocd",
		},
		"spec": map[string]any{
			"source": map[string]any{
				"repoURL":        "oci://local-registry:5000/demo",
				"targetRevision": "v1",
				"path":           "k8s",
			},
		},
	}}

	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{gvr: "ApplicationList"},
		app,
	)

	return argocd.NewManager(clientset, dyn)
}

// BenchmarkEnsureOptions measures the performance of EnsureOptions struct initialization.
func BenchmarkEnsureOptions(b *testing.B) {
	b.Run("Minimal", func(b *testing.B) {
		b.ReportAllocs()

		for range b.N {
			_ = argocd.EnsureOptions{
				RepositoryURL:  "oci://local-registry:5000/demo",
				TargetRevision: "v1",
			}
		}
	})

	b.Run("WithApplicationName", func(b *testing.B) {
		b.ReportAllocs()

		for range b.N {
			_ = argocd.EnsureOptions{
				RepositoryURL:   "oci://local-registry:5000/demo",
				ApplicationName: "my-app",
				TargetRevision:  "v1.2.3",
			}
		}
	})

	b.Run("WithAuth", func(b *testing.B) {
		b.ReportAllocs()

		for range b.N {
			_ = argocd.EnsureOptions{
				RepositoryURL:   "oci://local-registry:5000/demo",
				ApplicationName: "my-app",
				TargetRevision:  "v1.2.3",
				Username:        "admin",
				Password:        "secret-password",
			}
		}
	})

	b.Run("Production", func(b *testing.B) {
		b.ReportAllocs()

		for range b.N {
			_ = argocd.EnsureOptions{
				RepositoryURL:   "oci://ghcr.io/devantler-tech/ksail",
				ApplicationName: "production-app",
				TargetRevision:  "v1.2.3",
				SourcePath:      "manifests/production",
				Username:        "deployment-bot",
				Password:        "very-secure-token-with-long-value",
				Insecure:        false,
			}
		}
	})
}

// BenchmarkUpdateTargetRevisionOptions measures the performance of UpdateTargetRevisionOptions struct initialization.
func BenchmarkUpdateTargetRevisionOptions(b *testing.B) {
	b.Run("MinimalUpdate", func(b *testing.B) {
		b.ReportAllocs()

		for range b.N {
			_ = argocd.UpdateTargetRevisionOptions{
				TargetRevision: "v2",
			}
		}
	})

	b.Run("WithHardRefresh", func(b *testing.B) {
		b.ReportAllocs()

		for range b.N {
			_ = argocd.UpdateTargetRevisionOptions{
				ApplicationName: "my-app",
				TargetRevision:  "v2.1.0",
				HardRefresh:     true,
			}
		}
	})
}

// BenchmarkManagerEnsure measures the performance of creating/updating ArgoCD Application and repository secret.
//
//nolint:funlen // Benchmark subtests require individual setup for each scenario.
func BenchmarkManagerEnsure(b *testing.B) {
	b.Run("FirstTimeCreate", func(b *testing.B) {
		ctx := context.Background()

		opts := argocd.EnsureOptions{
			RepositoryURL:   "oci://local-registry:5000/demo",
			ApplicationName: "benchmark-app",
			TargetRevision:  "v1",
		}

		b.ResetTimer()
		b.ReportAllocs()

		for range b.N {
			b.StopTimer()
			// Reset state by creating fresh manager
			mgr := setupBenchmarkManager(b)

			b.StartTimer()

			err := mgr.Ensure(ctx, opts)
			if err != nil {
				b.Fatalf("Ensure failed: %v", err)
			}
		}
	})

	b.Run("UpdateExisting", func(b *testing.B) {
		ctx := context.Background()

		b.ResetTimer()
		b.ReportAllocs()

		for range b.N {
			b.StopTimer()
			// Create initial state
			mgr := setupBenchmarkManager(b)

			err := mgr.Ensure(ctx, argocd.EnsureOptions{
				RepositoryURL:   "oci://local-registry:5000/demo",
				ApplicationName: "benchmark-app",
				TargetRevision:  "v1",
			})
			if err != nil {
				b.Fatalf("Initial ensure failed: %v", err)
			}

			b.StartTimer()

			// Measure update
			err = mgr.Ensure(ctx, argocd.EnsureOptions{
				RepositoryURL:   "oci://local-registry:5000/demo",
				ApplicationName: "benchmark-app",
				TargetRevision:  "v2",
			})
			if err != nil {
				b.Fatalf("Update ensure failed: %v", err)
			}
		}
	})

	b.Run("WithAuthentication", func(b *testing.B) {
		ctx := context.Background()

		opts := argocd.EnsureOptions{
			RepositoryURL:   "oci://ghcr.io/org/repo",
			ApplicationName: "secure-app",
			TargetRevision:  "v1.0.0",
			Username:        "deploy-bot",
			Password:        "test-password",
		}

		b.ResetTimer()
		b.ReportAllocs()

		for range b.N {
			b.StopTimer()
			mgr := setupBenchmarkManager(b)

			b.StartTimer()

			err := mgr.Ensure(ctx, opts)
			if err != nil {
				b.Fatalf("Ensure with auth failed: %v", err)
			}
		}
	})

	b.Run("ProductionConfig", func(b *testing.B) {
		ctx := context.Background()

		opts := argocd.EnsureOptions{
			RepositoryURL:   "oci://registry.production.example.com/apps/backend",
			ApplicationName: "production-backend",
			TargetRevision:  "v3.2.1",
			SourcePath:      "k8s/overlays/production",
			Username:        "argocd-sync-sa",
			Password:        "test-token",
			Insecure:        false,
		}

		b.ResetTimer()
		b.ReportAllocs()

		for range b.N {
			b.StopTimer()
			mgr := setupBenchmarkManager(b)

			b.StartTimer()

			err := mgr.Ensure(ctx, opts)
			if err != nil {
				b.Fatalf("Production ensure failed: %v", err)
			}
		}
	})
}

// BenchmarkManagerUpdateTargetRevision measures the performance of updating Application target revision.
func BenchmarkManagerUpdateTargetRevision(b *testing.B) {
	b.Run("TargetRevisionOnly", func(b *testing.B) {
		ctx := context.Background()

		opts := argocd.UpdateTargetRevisionOptions{
			ApplicationName: "benchmark-app",
			TargetRevision:  "v2",
		}

		b.ResetTimer()
		b.ReportAllocs()

		for range b.N {
			b.StopTimer()
			mgr := setupBenchmarkManagerWithApp(b, "benchmark-app")

			b.StartTimer()

			err := mgr.UpdateTargetRevision(ctx, opts)
			if err != nil {
				b.Fatalf("UpdateTargetRevision failed: %v", err)
			}
		}
	})

	b.Run("WithHardRefresh", func(b *testing.B) {
		ctx := context.Background()

		opts := argocd.UpdateTargetRevisionOptions{
			ApplicationName: "benchmark-app",
			TargetRevision:  "v2.1.0",
			HardRefresh:     true,
		}

		b.ResetTimer()
		b.ReportAllocs()

		for range b.N {
			b.StopTimer()
			mgr := setupBenchmarkManagerWithApp(b, "benchmark-app")

			b.StartTimer()

			err := mgr.UpdateTargetRevision(ctx, opts)
			if err != nil {
				b.Fatalf("UpdateTargetRevision with hard refresh failed: %v", err)
			}
		}
	})

	b.Run("HardRefreshOnly", func(b *testing.B) {
		ctx := context.Background()

		opts := argocd.UpdateTargetRevisionOptions{
			ApplicationName: "benchmark-app",
			HardRefresh:     true,
		}

		b.ResetTimer()
		b.ReportAllocs()

		for range b.N {
			b.StopTimer()
			mgr := setupBenchmarkManagerWithApp(b, "benchmark-app")

			b.StartTimer()

			err := mgr.UpdateTargetRevision(ctx, opts)
			if err != nil {
				b.Fatalf("UpdateTargetRevision hard refresh only failed: %v", err)
			}
		}
	})
}

// BenchmarkNewManager measures the performance of creating a new ArgoCD manager.
func BenchmarkNewManager(b *testing.B) {
	clientset := k8sfake.NewClientset()
	gvr := schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "applications",
	}
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{gvr: "ApplicationList"},
	)

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		_ = argocd.NewManager(clientset, dyn)
	}
}
