package v1alpha1_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestGitOpsEngine_Normalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   v1alpha1.GitOpsEngine
		want v1alpha1.GitOpsEngine
	}{
		{"empty normalizes to None", v1alpha1.GitOpsEngine(""), v1alpha1.GitOpsEngineNone},
		{"None stays None", v1alpha1.GitOpsEngineNone, v1alpha1.GitOpsEngineNone},
		{"Flux unchanged", v1alpha1.GitOpsEngineFlux, v1alpha1.GitOpsEngineFlux},
		{"ArgoCD unchanged", v1alpha1.GitOpsEngineArgoCD, v1alpha1.GitOpsEngineArgoCD},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := testCase.in
			assert.Equal(t, testCase.want, engine.Normalize())
		})
	}
}

func TestGitOpsEngine_IsNone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   v1alpha1.GitOpsEngine
		want bool
	}{
		{"empty is none", v1alpha1.GitOpsEngine(""), true},
		{"None is none", v1alpha1.GitOpsEngineNone, true},
		{"Flux is not none", v1alpha1.GitOpsEngineFlux, false},
		{"ArgoCD is not none", v1alpha1.GitOpsEngineArgoCD, false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := testCase.in
			assert.Equal(t, testCase.want, engine.IsNone())
		})
	}
}
