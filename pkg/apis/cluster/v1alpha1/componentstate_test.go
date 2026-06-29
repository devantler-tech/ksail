package v1alpha1_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestComponentState_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Ready", v1alpha1.ComponentStateReady.String())
	assert.Equal(t, "Failed", v1alpha1.ComponentStateFailed.String())
}

func TestComponentState_ValidValues(t *testing.T) {
	t.Parallel()

	values := v1alpha1.ComponentStateReady.ValidValues()

	assert.ElementsMatch(t, []string{
		"Ready",
		"Failed",
	}, values)
}
