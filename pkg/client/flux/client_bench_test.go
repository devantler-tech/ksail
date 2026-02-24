//nolint:funlen // Table-driven benchmarks with scenarios are naturally long.
package flux_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/flux"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	meta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// setupTestKubeconfig creates a temporary kubeconfig file for testing.
// Returns the path to the temp file and a cleanup function.
func setupTestKubeconfig(b *testing.B) (string, func()) {
	b.Helper()

	tmpDir := b.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	kubeconfigContent := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: test-token
`

	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	if err != nil {
		b.Fatalf("Failed to write kubeconfig: %v", err)
	}

	return kubeconfigPath, func() {
		// Cleanup is handled by b.TempDir()
	}
}

// BenchmarkClient_CreateCreateCommand benchmarks the creation of the Flux create command tree.
// This measures the overhead of Cobra command initialization.
func BenchmarkClient_CreateCreateCommand(b *testing.B) {
	kubeconfigPath, cleanup := setupTestKubeconfig(b)
	defer cleanup()

	ioStreams := genericiooptions.IOStreams{
		In:     &bytes.Buffer{},
		Out:    &bytes.Buffer{},
		ErrOut: &bytes.Buffer{},
	}

	fluxClient := flux.NewClient(ioStreams, kubeconfigPath)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		cmd := fluxClient.CreateCreateCommand(kubeconfigPath)
		if cmd == nil {
			b.Fatal("CreateCreateCommand returned nil")
		}
	}
}

// BenchmarkGitRepository_Creation benchmarks GitRepository struct creation.
func BenchmarkGitRepository_Creation(b *testing.B) {
	scenarios := []struct {
		name string
		repo *sourcev1.GitRepository
	}{
		{
			name: "Minimal",
			repo: &sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-repo",
					Namespace: "flux-system",
				},
				Spec: sourcev1.GitRepositorySpec{
					URL:      "https://github.com/example/repo",
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
			},
		},
		{
			name: "WithReference",
			repo: &sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-repo",
					Namespace: "flux-system",
				},
				Spec: sourcev1.GitRepositorySpec{
					URL: "https://github.com/example/repo",
					Reference: &sourcev1.GitRepositoryRef{
						Branch: "main",
						Tag:    "v1.0.0",
						Commit: "abc123def456",
					},
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
			},
		},
		{
			name: "Production",
			repo: &sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "production-repo",
					Namespace: "flux-system",
					Labels: map[string]string{
						"app":         "production",
						"environment": "prod",
						"team":        "platform",
					},
				},
				Spec: sourcev1.GitRepositorySpec{
					URL: "https://github.com/example/production-repo",
					Reference: &sourcev1.GitRepositoryRef{
						Branch: "main",
					},
					Interval: metav1.Duration{Duration: time.Minute},
					SecretRef: &meta.LocalObjectReference{
						Name: "git-credentials",
					},
					Suspend: false,
					Timeout: &metav1.Duration{Duration: 5 * time.Minute},
				},
			},
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				repo := scenario.repo.DeepCopy()
				if repo == nil {
					b.Fatal("DeepCopy failed")
				}
			}
		})
	}
}

// BenchmarkHelmRepository_Creation benchmarks HelmRepository struct creation.
func BenchmarkHelmRepository_Creation(b *testing.B) {
	scenarios := []struct {
		name string
		repo *sourcev1.HelmRepository
	}{
		{
			name: "Minimal",
			repo: &sourcev1.HelmRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-helm-repo",
					Namespace: "flux-system",
				},
				Spec: sourcev1.HelmRepositorySpec{
					URL:      "https://charts.example.com",
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
			},
		},
		{
			name: "Production",
			repo: &sourcev1.HelmRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "production-helm-repo",
					Namespace: "flux-system",
					Labels: map[string]string{
						"app":  "production",
						"type": "helm",
					},
				},
				Spec: sourcev1.HelmRepositorySpec{
					URL:      "https://charts.example.com",
					Interval: metav1.Duration{Duration: 5 * time.Minute},
					SecretRef: &meta.LocalObjectReference{
						Name: "helm-credentials",
					},
					Timeout: &metav1.Duration{Duration: 5 * time.Minute},
				},
			},
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				repo := scenario.repo.DeepCopy()
				if repo == nil {
					b.Fatal("DeepCopy failed")
				}
			}
		})
	}
}

// BenchmarkOCIRepository_Creation benchmarks OCIRepository struct creation.
func BenchmarkOCIRepository_Creation(b *testing.B) {
	scenarios := []struct {
		name string
		repo *sourcev1.OCIRepository
	}{
		{
			name: "Minimal",
			repo: &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-oci-repo",
					Namespace: "flux-system",
				},
				Spec: sourcev1.OCIRepositorySpec{
					URL:      "oci://ghcr.io/example/repo",
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
			},
		},
		{
			name: "WithReference",
			repo: &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-oci-repo",
					Namespace: "flux-system",
				},
				Spec: sourcev1.OCIRepositorySpec{
					URL: "oci://ghcr.io/example/repo",
					Reference: &sourcev1.OCIRepositoryRef{
						Tag:    "v1.0.0",
						SemVer: ">=1.0.0 <2.0.0",
					},
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
			},
		},
		{
			name: "Production",
			repo: &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "production-oci-repo",
					Namespace: "flux-system",
					Labels: map[string]string{
						"app":         "production",
						"environment": "prod",
					},
				},
				Spec: sourcev1.OCIRepositorySpec{
					URL:      "oci://ghcr.io/example/production-repo",
					Provider: "generic",
					Reference: &sourcev1.OCIRepositoryRef{
						Tag: "latest",
					},
					Interval: metav1.Duration{Duration: time.Minute},
					SecretRef: &meta.LocalObjectReference{
						Name: "oci-credentials",
					},
					Timeout: &metav1.Duration{Duration: 5 * time.Minute},
				},
			},
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				repo := scenario.repo.DeepCopy()
				if repo == nil {
					b.Fatal("DeepCopy failed")
				}
			}
		})
	}
}

// BenchmarkKustomization_Creation benchmarks Kustomization struct creation.
func BenchmarkKustomization_Creation(b *testing.B) {
	scenarios := []struct {
		name          string
		kustomization *kustomizev1.Kustomization
	}{
		{
			name: "Minimal",
			kustomization: &kustomizev1.Kustomization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-kustomization",
					Namespace: "flux-system",
				},
				Spec: kustomizev1.KustomizationSpec{
					SourceRef: kustomizev1.CrossNamespaceSourceReference{
						Kind: "GitRepository",
						Name: "test-repo",
					},
					Path:     "./",
					Prune:    true,
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
			},
		},
		{
			name: "Production",
			kustomization: &kustomizev1.Kustomization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "production-kustomization",
					Namespace: "flux-system",
					Labels: map[string]string{
						"app":         "production",
						"environment": "prod",
					},
				},
				Spec: kustomizev1.KustomizationSpec{
					SourceRef: kustomizev1.CrossNamespaceSourceReference{
						Kind:      "GitRepository",
						Name:      "production-repo",
						Namespace: "flux-system",
					},
					Path:            "./kustomize/production",
					Prune:           true,
					Interval:        metav1.Duration{Duration: time.Minute},
					TargetNamespace: "production",
					Wait:            true,
					Timeout:         &metav1.Duration{Duration: 10 * time.Minute},
				},
			},
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				k := scenario.kustomization.DeepCopy()
				if k == nil {
					b.Fatal("DeepCopy failed")
				}
			}
		})
	}
}

// BenchmarkHelmRelease_Creation benchmarks HelmRelease struct creation.
func BenchmarkHelmRelease_Creation(b *testing.B) {
	scenarios := []struct {
		name    string
		release *helmv2.HelmRelease
	}{
		{
			name: "Minimal",
			release: &helmv2.HelmRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-release",
					Namespace: "default",
				},
				Spec: helmv2.HelmReleaseSpec{
					Chart: &helmv2.HelmChartTemplate{
						Spec: helmv2.HelmChartTemplateSpec{
							Chart: "nginx",
							SourceRef: helmv2.CrossNamespaceObjectReference{
								Kind: "HelmRepository",
								Name: "test-helm-repo",
							},
						},
					},
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
			},
		},
		{
			name: "Production",
			release: &helmv2.HelmRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "production-release",
					Namespace: "production",
					Labels: map[string]string{
						"app":         "webapp",
						"environment": "prod",
					},
				},
				Spec: helmv2.HelmReleaseSpec{
					Chart: &helmv2.HelmChartTemplate{
						Spec: helmv2.HelmChartTemplateSpec{
							Chart:   "webapp",
							Version: "1.2.3",
							SourceRef: helmv2.CrossNamespaceObjectReference{
								Kind:      "HelmRepository",
								Name:      "production-helm-repo",
								Namespace: "flux-system",
							},
						},
					},
					Interval: metav1.Duration{Duration: time.Minute},
					Install: &helmv2.Install{
						CreateNamespace: true,
						Timeout:         &metav1.Duration{Duration: 10 * time.Minute},
					},
					Upgrade: &helmv2.Upgrade{
						Timeout: &metav1.Duration{Duration: 10 * time.Minute},
					},
				},
			},
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				release := scenario.release.DeepCopy()
				if release == nil {
					b.Fatal("DeepCopy failed")
				}
			}
		})
	}
}

// BenchmarkCopySpec benchmarks the internal copySpec function with all Flux resource types.
func BenchmarkCopySpec(b *testing.B) {
	scenarios := []struct {
		name   string
		newSrc func() client.Object
		newDst func() client.Object
	}{
		{
			name: "GitRepository",
			newSrc: func() client.Object {
				return &sourcev1.GitRepository{
					Spec: sourcev1.GitRepositorySpec{
						URL:      "https://github.com/example/repo",
						Interval: metav1.Duration{Duration: 5 * time.Minute},
					},
				}
			},
			newDst: func() client.Object { return &sourcev1.GitRepository{} },
		},
		{
			name: "HelmRepository",
			newSrc: func() client.Object {
				return &sourcev1.HelmRepository{
					Spec: sourcev1.HelmRepositorySpec{
						URL:      "https://charts.example.com",
						Interval: metav1.Duration{Duration: 5 * time.Minute},
					},
				}
			},
			newDst: func() client.Object { return &sourcev1.HelmRepository{} },
		},
		{
			name: "OCIRepository",
			newSrc: func() client.Object {
				return &sourcev1.OCIRepository{
					Spec: sourcev1.OCIRepositorySpec{
						URL:      "oci://ghcr.io/example/repo",
						Interval: metav1.Duration{Duration: 5 * time.Minute},
					},
				}
			},
			newDst: func() client.Object { return &sourcev1.OCIRepository{} },
		},
		{
			name: "Kustomization",
			newSrc: func() client.Object {
				return &kustomizev1.Kustomization{
					Spec: kustomizev1.KustomizationSpec{
						SourceRef: kustomizev1.CrossNamespaceSourceReference{
							Kind: "GitRepository",
							Name: "test-repo",
						},
						Path:     "./",
						Prune:    true,
						Interval: metav1.Duration{Duration: 5 * time.Minute},
					},
				}
			},
			newDst: func() client.Object { return &kustomizev1.Kustomization{} },
		},
		{
			name: "HelmRelease",
			newSrc: func() client.Object {
				return &helmv2.HelmRelease{
					Spec: helmv2.HelmReleaseSpec{
						Chart: &helmv2.HelmChartTemplate{
							Spec: helmv2.HelmChartTemplateSpec{
								Chart: "nginx",
								SourceRef: helmv2.CrossNamespaceObjectReference{
									Kind: "HelmRepository",
									Name: "test-helm-repo",
								},
							},
						},
						Interval: metav1.Duration{Duration: 5 * time.Minute},
					},
				}
			},
			newDst: func() client.Object { return &helmv2.HelmRelease{} },
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				src := scenario.newSrc()
				dst := scenario.newDst()

				err := flux.CopySpec(src, dst)
				if err != nil {
					b.Fatalf("CopySpec failed: %v", err)
				}
			}
		})
	}
}
