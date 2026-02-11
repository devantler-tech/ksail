package k8s_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrKubeconfigPathEmpty(t *testing.T) {
	t.Parallel()

	require.Error(t, k8s.ErrKubeconfigPathEmpty)
	assert.Equal(t, "kubeconfig path is empty", k8s.ErrKubeconfigPathEmpty.Error())
}
