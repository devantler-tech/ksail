package operator_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/operator"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOperatorCmd(t *testing.T) {
	t.Parallel()

	cmd := operator.NewOperatorCmd(di.NewRuntime())

	assert.Equal(t, "operator", cmd.Use)
	assert.True(t, cmd.Hidden, "operator command should be hidden")
	assert.Equal(t, "true", cmd.Annotations[annotations.AnnotationExclude],
		"operator command must be excluded from AI tool generation")

	for _, name := range []string{
		"api-bind-address",
		"read-only",
		"leader-elect",
		"metrics-bind-address",
		"health-probe-bind-address",
		"oidc-issuer-url",
		"oidc-client-id",
		"oidc-redirect-url",
		"oidc-scopes",
	} {
		require.NotNil(t, cmd.Flags().Lookup(name), "missing flag --%s", name)
	}
}
