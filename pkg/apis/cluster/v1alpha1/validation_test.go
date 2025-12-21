package v1alpha1_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitOpsEngineSet_AcceptsArgoCD(t *testing.T) {
	t.Parallel()

	var engine v1alpha1.GitOpsEngine
	require.NoError(t, engine.Set("ArgoCD"))
	assert.Equal(t, v1alpha1.GitOpsEngine("ArgoCD"), engine)
}

func TestGitOpsEngineSet_InvalidListsValidOptions(t *testing.T) {
	t.Parallel()

	var engine v1alpha1.GitOpsEngine

	err := engine.Set("invalid")
	require.Error(t, err)

	require.ErrorIs(t, err, v1alpha1.ErrInvalidGitOpsEngine)
	assert.Contains(t, err.Error(), "None")
	assert.Contains(t, err.Error(), "Flux")
	assert.Contains(t, err.Error(), "ArgoCD")
}
