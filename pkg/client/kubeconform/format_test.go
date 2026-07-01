package kubeconform_test

import (
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/yannh/kubeconform/pkg/resource"
	"github.com/yannh/kubeconform/pkg/validator"
)

// errBoom is a static sentinel used as the failure detail in FormatFailure tests
// (err113 requires a static error value rather than an inline errors.New).
var errBoom = errors.New("boom")

// TestFormatFailure exercises each identity branch of the failure formatter:
// a namespaced resource (Kind/Namespace/Name), a cluster-scoped resource
// (Kind/Name), and a document with no usable signature (detail only); plus the
// attribution branches that append " (from <source>)" when the resource's identity
// is present in the attribution map (and leave the message unchanged otherwise).
func TestFormatFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		bytes       string
		attribution map[string]string
		want        string
	}{
		{
			name: "namespaced resource is prefixed with Kind/Namespace/Name",
			bytes: `apiVersion: v1
kind: ConfigMap
metadata:
  name: web
  namespace: apps
`,
			want: "ConfigMap/apps/web: boom",
		},
		{
			name: "cluster-scoped resource is prefixed with Kind/Name",
			bytes: `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: viewer
`,
			want: "ClusterRole/viewer: boom",
		},
		{
			name: "document missing kind falls back to the bare detail",
			bytes: `apiVersion: v1
metadata:
  name: orphan
`,
			want: "boom",
		},
		{
			name: "matching attribution appends the source layer",
			bytes: `apiVersion: v1
kind: ConfigMap
metadata:
  name: web
  namespace: apps
`,
			attribution: map[string]string{"ConfigMap/apps/web": "HelmRelease flux-system/app"},
			want:        "ConfigMap/apps/web: boom (from HelmRelease flux-system/app)",
		},
		{
			name: "attribution for a different resource is not applied",
			bytes: `apiVersion: v1
kind: ConfigMap
metadata:
  name: web
  namespace: apps
`,
			attribution: map[string]string{
				"ConfigMap/other/thing": "HelmRelease flux-system/other",
			},
			want: "ConfigMap/apps/web: boom",
		},
		{
			name: "attribution is not applied to a resource without a usable signature",
			bytes: `apiVersion: v1
metadata:
  name: orphan
`,
			attribution: map[string]string{"ConfigMap/apps/web": "HelmRelease flux-system/app"},
			want:        "boom",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			res := validator.Result{
				Resource: resource.Resource{Bytes: []byte(testCase.bytes)},
				Err:      errBoom,
				Status:   validator.Error,
			}

			got := kubeconform.FormatFailure(res, testCase.attribution)
			if got != testCase.want {
				t.Fatalf("FormatFailure() = %q, want %q", got, testCase.want)
			}
		})
	}
}
