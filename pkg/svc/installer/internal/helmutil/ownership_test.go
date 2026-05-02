package helmutil_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
	"github.com/stretchr/testify/assert"
)

func TestIsGitOpsManaged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		labels         map[string]string
		wantController string
		wantManaged    bool
	}{
		{
			name:           "nil labels",
			labels:         nil,
			wantController: "",
			wantManaged:    false,
		},
		{
			name:           "empty labels",
			labels:         map[string]string{},
			wantController: "",
			wantManaged:    false,
		},
		{
			name: "standard helm labels only",
			labels: map[string]string{
				"name":    "cert-manager",
				"owner":   "helm",
				"version": "1",
				"status":  "deployed",
			},
			wantController: "",
			wantManaged:    false,
		},
		{
			name: "flux managed",
			labels: map[string]string{
				"name":                            "cert-manager",
				"owner":                           "helm",
				"helm.toolkit.fluxcd.io/name":      "cert-manager",
				"helm.toolkit.fluxcd.io/namespace": "flux-system",
			},
			wantController: "Flux",
			wantManaged:    true,
		},
		{
			name: "flux name label only",
			labels: map[string]string{
				"helm.toolkit.fluxcd.io/name": "my-release",
			},
			wantController: "Flux",
			wantManaged:    true,
		},
		{
			name: "argocd managed",
			labels: map[string]string{
				"name":                          "cert-manager",
				"owner":                         "helm",
				"argocd.argoproj.io/managed-by": "argocd",
			},
			wantController: "ArgoCD",
			wantManaged:    true,
		},
		{
			name: "flux takes precedence over argocd",
			labels: map[string]string{
				"helm.toolkit.fluxcd.io/name":   "cert-manager",
				"argocd.argoproj.io/managed-by": "argocd",
			},
			wantController: "Flux",
			wantManaged:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			controller, managed := helmutil.IsGitOpsManaged(tt.labels)

			assert.Equal(t, tt.wantController, controller)
			assert.Equal(t, tt.wantManaged, managed)
		})
	}
}
