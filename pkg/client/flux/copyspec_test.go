package flux_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/flux"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCopySpec_GitRepository(t *testing.T) {
	t.Parallel()

	src := &sourcev1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "test-git"},
		Spec: sourcev1.GitRepositorySpec{
			URL: "https://github.com/example/repo",
		},
	}
	dst := &sourcev1.GitRepository{}

	err := flux.CopySpec(src, dst)

	require.NoError(t, err)
	assert.Equal(t, "https://github.com/example/repo", dst.Spec.URL)
}

func TestCopySpec_HelmRepository(t *testing.T) {
	t.Parallel()

	src := &sourcev1.HelmRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "test-helm-repo"},
		Spec: sourcev1.HelmRepositorySpec{
			URL: "https://charts.example.com",
		},
	}
	dst := &sourcev1.HelmRepository{}

	err := flux.CopySpec(src, dst)

	require.NoError(t, err)
	assert.Equal(t, "https://charts.example.com", dst.Spec.URL)
}

func TestCopySpec_OCIRepository(t *testing.T) {
	t.Parallel()

	src := &sourcev1.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "test-oci"},
		Spec: sourcev1.OCIRepositorySpec{
			URL: "oci://ghcr.io/org/repo",
		},
	}
	dst := &sourcev1.OCIRepository{}

	err := flux.CopySpec(src, dst)

	require.NoError(t, err)
	assert.Equal(t, "oci://ghcr.io/org/repo", dst.Spec.URL)
}

func TestCopySpec_Kustomization(t *testing.T) {
	t.Parallel()

	src := &kustomizev1.Kustomization{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ks"},
		Spec: kustomizev1.KustomizationSpec{
			Path:  "./deploy",
			Prune: true,
		},
	}
	dst := &kustomizev1.Kustomization{}

	err := flux.CopySpec(src, dst)

	require.NoError(t, err)
	assert.Equal(t, "./deploy", dst.Spec.Path)
	assert.True(t, dst.Spec.Prune)
}

func TestCopySpec_HelmRelease(t *testing.T) {
	t.Parallel()

	src := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{Name: "test-hr"},
		Spec: helmv2.HelmReleaseSpec{
			TargetNamespace: "production",
		},
	}
	dst := &helmv2.HelmRelease{}

	err := flux.CopySpec(src, dst)

	require.NoError(t, err)
	assert.Equal(t, "production", dst.Spec.TargetNamespace)
}

func TestCopySpec_UnsupportedType(t *testing.T) {
	t.Parallel()

	// Use a generic client.Object that isn't one of the supported types
	src := &unsupportedObject{}
	dst := &unsupportedObject{}

	err := flux.CopySpec(src, dst)

	require.Error(t, err)
	assert.ErrorIs(t, err, flux.ErrUnsupportedResourceType)
}

func TestCopySpec_TypeMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  client.Object
		dst  client.Object
	}{
		{
			name: "GitRepository src to HelmRepository dst",
			src: &sourcev1.GitRepository{
				Spec: sourcev1.GitRepositorySpec{URL: "https://example.com"},
			},
			dst: &sourcev1.HelmRepository{},
		},
		{
			name: "HelmRepository src to OCIRepository dst",
			src: &sourcev1.HelmRepository{
				Spec: sourcev1.HelmRepositorySpec{URL: "https://charts.example.com"},
			},
			dst: &sourcev1.OCIRepository{},
		},
		{
			name: "OCIRepository src to GitRepository dst",
			src: &sourcev1.OCIRepository{
				Spec: sourcev1.OCIRepositorySpec{URL: "oci://ghcr.io/org"},
			},
			dst: &sourcev1.GitRepository{},
		},
		{
			name: "Kustomization src to HelmRelease dst",
			src: &kustomizev1.Kustomization{
				Spec: kustomizev1.KustomizationSpec{Path: "./"},
			},
			dst: &helmv2.HelmRelease{},
		},
		{
			name: "HelmRelease src to Kustomization dst",
			src: &helmv2.HelmRelease{
				Spec: helmv2.HelmReleaseSpec{TargetNamespace: "prod"},
			},
			dst: &kustomizev1.Kustomization{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := flux.CopySpec(tt.src, tt.dst)

			require.Error(t, err)
			assert.ErrorIs(t, err, flux.ErrTypeMismatch)
		})
	}
}

// unsupportedObject is a dummy client.Object used for unsupported type tests.
type unsupportedObject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
}

func (o *unsupportedObject) DeepCopyObject() runtime.Object {
	return &unsupportedObject{
		TypeMeta:   o.TypeMeta,
		ObjectMeta: *o.DeepCopy(),
	}
}
