package di_test

import (
	"testing"

	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/stretchr/testify/require"
)

func TestNewRuntime(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()

	require.NotNil(t, rt, "expected runtime to be created")
}

func TestNewRuntime_ProvidesTimer(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()

	err := rt.Invoke(func(injector runtime.Injector) error {
		tmr, resolveErr := runtime.ResolveTimer(injector)
		require.NoError(t, resolveErr, "expected timer to be resolved")
		require.NotNil(t, tmr, "expected timer to be non-nil")

		return nil
	})

	require.NoError(t, err, "expected invoke to succeed")
}

func TestNewRuntime_ProvidesClusterProvisionerFactory(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()

	err := rt.Invoke(func(injector runtime.Injector) error {
		factory, resolveErr := runtime.ResolveClusterProvisionerFactory(injector)
		require.NoError(t, resolveErr, "expected factory to be resolved")
		require.NotNil(t, factory, "expected factory to be non-nil")

		return nil
	})

	require.NoError(t, err, "expected invoke to succeed")
}
