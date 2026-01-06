package di_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/di"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Error variable for test cases.
var errHandlerExecutionFailed = errors.New("handler execution failed")

func TestResolveTimer_Success(t *testing.T) {
	t.Parallel()

	// Create an injector with a timer registered
	injector := do.New()
	do.Provide(injector, func(_ do.Injector) (timer.Timer, error) {
		return timer.New(), nil
	})

	resolvedTimer, err := di.ResolveTimer(injector)

	require.NoError(t, err)
	require.NotNil(t, resolvedTimer, "ResolveTimer should return a non-nil timer")

	// Verify the timer is functional by calling Start
	resolvedTimer.Start()
	total, stage := resolvedTimer.GetTiming()
	assert.GreaterOrEqual(t, total.Nanoseconds(), int64(0), "Total time should be non-negative")
	assert.GreaterOrEqual(t, stage.Nanoseconds(), int64(0), "Stage time should be non-negative")
}

func TestResolveTimer_Error(t *testing.T) {
	t.Parallel()

	// Create an empty injector (no timer registered)
	injector := do.New()

	resolvedTimer, err := di.ResolveTimer(injector)

	require.Error(t, err)
	assert.Nil(t, resolvedTimer)
	assert.Contains(t, err.Error(), "resolve timer dependency")
}

func TestResolveClusterProvisionerFactory_Success(t *testing.T) {
	t.Parallel()

	// Create an injector with a factory registered
	injector := do.New()
	expectedFactory := &clusterprovisioner.DefaultFactory{}

	do.Provide(injector, func(_ do.Injector) (clusterprovisioner.Factory, error) {
		return expectedFactory, nil
	})

	factory, err := di.ResolveClusterProvisionerFactory(injector)

	require.NoError(t, err)
	require.NotNil(t, factory, "ResolveClusterProvisionerFactory should return a non-nil factory")
}

func TestResolveClusterProvisionerFactory_Error(t *testing.T) {
	t.Parallel()

	// Create an empty injector (no factory registered)
	injector := do.New()

	factory, err := di.ResolveClusterProvisionerFactory(injector)

	require.Error(t, err)
	assert.Nil(t, factory)
	assert.Contains(t, err.Error(), "resolve provisioner factory dependency")
}

func TestWithTimer_Success(t *testing.T) {
	t.Parallel()

	// Create an injector with a timer registered
	injector := do.New()
	do.Provide(injector, func(_ do.Injector) (timer.Timer, error) {
		return timer.New(), nil
	})

	handlerCalled := false
	handler := func(_ *cobra.Command, _ di.Injector, tmr timer.Timer) error {
		handlerCalled = true

		tmr.Start()

		return nil
	}

	wrappedHandler := di.WithTimer(handler)
	err := wrappedHandler(&cobra.Command{}, injector)

	require.NoError(t, err)
	assert.True(t, handlerCalled, "Handler should have been called")
}

func TestWithTimer_HandlerError(t *testing.T) {
	t.Parallel()

	// Create an injector with a timer registered
	injector := do.New()
	do.Provide(injector, func(_ do.Injector) (timer.Timer, error) {
		return timer.New(), nil
	})

	handler := func(_ *cobra.Command, _ di.Injector, _ timer.Timer) error {
		return fmt.Errorf("handler failed: %w", errHandlerExecutionFailed)
	}

	wrappedHandler := di.WithTimer(handler)
	err := wrappedHandler(&cobra.Command{}, injector)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "handler execution failed")
}

func TestWithTimer_TimerResolveError(t *testing.T) {
	t.Parallel()

	// Create an empty injector (no timer registered)
	injector := do.New()

	handler := func(_ *cobra.Command, _ di.Injector, _ timer.Timer) error {
		return nil
	}

	wrappedHandler := di.WithTimer(handler)
	err := wrappedHandler(&cobra.Command{}, injector)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve timer dependency")
}
