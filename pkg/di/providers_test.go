package di_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/stretchr/testify/require"
)

func TestNewRuntime(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()

	require.NotNil(t, rt, "expected runtime to be created")
}

func TestNewRuntime_ProvidesTimer(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()

	err := rt.Invoke(func(injector di.Injector) error {
		tmr, resolveErr := di.ResolveTimer(injector)
		require.NoError(t, resolveErr, "expected timer to be resolved")
		require.NotNil(t, tmr, "expected timer to be non-nil")

		return nil
	})

	require.NoError(t, err, "expected invoke to succeed")
}

func TestNewRuntime_ProvidesProvisionerFactory(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()

	err := rt.Invoke(func(injector di.Injector) error {
		factory, resolveErr := di.ResolveProvisionerFactory(injector)
		require.NoError(t, resolveErr, "expected factory to be resolved")
		require.NotNil(t, factory, "expected factory to be non-nil")

		return nil
	})

	require.NoError(t, err, "expected invoke to succeed")
}
