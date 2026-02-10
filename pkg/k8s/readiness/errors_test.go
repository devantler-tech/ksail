package readiness_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/k8s/readiness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrTimeoutExceeded(t *testing.T) {
	t.Parallel()

	require.Error(t, readiness.ErrTimeoutExceeded)
	assert.Equal(t, "timeout exceeded", readiness.ErrTimeoutExceeded.Error())
}
