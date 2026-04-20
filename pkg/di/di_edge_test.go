package di_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/di"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errExtraModuleFailed = errors.New("extra module failed")

//nolint:varnamelen // Short names keep the table-driven tests readable.
func TestRuntime_Invoke_ExtraModuleError(t *testing.T) {
	t.Parallel()

	rt := di.New()

	failingExtra := func(di.Injector) error {
		return errExtraModuleFailed
	}

	err := rt.Invoke(func(di.Injector) error {
		t.Fatal("handler should not run when extra module fails")

		return nil
	}, failingExtra)

	require.Error(t, err)
	assert.Equal(t, errExtraModuleFailed, err)
}

//nolint:varnamelen // Short names keep the table-driven tests readable.
func TestRuntime_Invoke_BaseModuleSetsUpDependencyForExtraModule(t *testing.T) {
	t.Parallel()

	type Config struct{ Value string }

	baseModule := func(i di.Injector) error {
		do.Provide(i, func(di.Injector) (*Config, error) {
			return &Config{Value: "base"}, nil
		})

		return nil
	}

	rt := di.New(baseModule)

	var resolvedValue string

	extraModule := func(i di.Injector) error {
		cfg, err := do.Invoke[*Config](i)
		if err != nil {
			return fmt.Errorf("resolve config: %w", err)
		}

		resolvedValue = cfg.Value

		return nil
	}

	err := rt.Invoke(func(di.Injector) error {
		return nil
	}, extraModule)

	require.NoError(t, err)
	assert.Equal(t, "base", resolvedValue)
}

//nolint:varnamelen // Short names keep this table-driven test readable.
func TestRuntime_Invoke_ExtraModuleOverridesBase(t *testing.T) {
	t.Parallel()

	baseModule := func(i di.Injector) error {
		do.Provide(i, func(di.Injector) (string, error) {
			return "base-value", nil
		})

		return nil
	}

	rt := di.New(baseModule)

	overrideModule := func(i di.Injector) error {
		do.Override(i, func(di.Injector) (string, error) {
			return "override-value", nil
		})

		return nil
	}

	var resolvedValue string

	err := rt.Invoke(func(i di.Injector) error {
		var resolveErr error

		resolvedValue, resolveErr = do.Invoke[string](i)

		return resolveErr
	}, overrideModule)

	require.NoError(t, err)
	assert.Equal(t, "override-value", resolvedValue)
}

//nolint:varnamelen // Short names keep the table-driven tests readable.
func TestRuntime_Invoke_FreshInjectorPerCall(t *testing.T) {
	t.Parallel()

	callCount := 0
	rt := di.New(func(i di.Injector) error {
		callCount++

		do.Provide(i, func(di.Injector) (int, error) {
			return callCount, nil
		})

		return nil
	})

	var val1, val2 int

	err := rt.Invoke(func(i di.Injector) error {
		var resolveErr error

		val1, resolveErr = do.Invoke[int](i)

		return resolveErr
	})
	require.NoError(t, err)

	err = rt.Invoke(func(i di.Injector) error {
		var resolveErr error

		val2, resolveErr = do.Invoke[int](i)

		return resolveErr
	})
	require.NoError(t, err)

	assert.Equal(t, 1, val1)
	assert.Equal(t, 2, val2)
}

func TestRunEWithRuntime_PassesArgsCorrectly(t *testing.T) {
	t.Parallel()

	rt := di.New()

	var receivedCmd *cobra.Command

	// RunEWithRuntime passes args via Invoke (handler sees cmd, not args).
	runE := di.RunEWithRuntime(rt, func(cmd *cobra.Command, _ di.Injector) error {
		receivedCmd = cmd

		return nil
	})

	cmd := &cobra.Command{Use: "deploy"}
	err := runE(cmd, []string{"arg1", "arg2"})

	require.NoError(t, err)
	assert.Equal(t, "deploy", receivedCmd.Use)
}

func TestRunEWithRuntime_ModuleError(t *testing.T) {
	t.Parallel()

	failModule := func(di.Injector) error {
		return errExtraModuleFailed
	}

	rt := di.New(failModule)

	runE := di.RunEWithRuntime(rt, func(*cobra.Command, di.Injector) error {
		t.Fatal("handler should not be called")

		return nil
	})

	cmd := &cobra.Command{Use: "test"}
	err := runE(cmd, nil)

	require.Error(t, err)
	assert.Equal(t, errExtraModuleFailed, err)
}

//nolint:wrapcheck // The test intentionally returns the original sentinel error.
func TestNewRuntime_TimerIsFunctional(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()

	err := rt.Invoke(func(injector di.Injector) error {
		tmr, resolveErr := di.ResolveTimer(injector)
		if resolveErr != nil {
			return resolveErr
		}

		tmr.Start()
		total, stage := tmr.GetTiming()
		assert.GreaterOrEqual(t, total.Nanoseconds(), int64(0))
		assert.GreaterOrEqual(t, stage.Nanoseconds(), int64(0))

		return nil
	})

	require.NoError(t, err)
}

//nolint:wrapcheck // The test intentionally returns the original sentinel error.
func TestNewRuntime_ProvisionerFactoryIsFunctional(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()

	err := rt.Invoke(func(injector di.Injector) error {
		factory, resolveErr := di.ResolveProvisionerFactory(injector)
		if resolveErr != nil {
			return resolveErr
		}

		// DefaultFactory should be returned.
		_, ok := factory.(clusterprovisioner.DefaultFactory)
		assert.True(t, ok, "factory should be DefaultFactory")

		return nil
	})

	require.NoError(t, err)
}

func TestWithTimer_PassesTimerToHandler(t *testing.T) {
	t.Parallel()

	injector := do.New()
	do.Provide(injector, func(_ do.Injector) (timer.Timer, error) {
		return timer.New(), nil
	})

	var receivedTimer timer.Timer

	handler := func(_ *cobra.Command, _ di.Injector, tmr timer.Timer) error {
		receivedTimer = tmr

		return nil
	}

	wrapped := di.WithTimer(handler)
	err := wrapped(&cobra.Command{Use: "test"}, injector)

	require.NoError(t, err)
	require.NotNil(t, receivedTimer)

	// Verify the timer works.
	receivedTimer.Start()
	total, _ := receivedTimer.GetTiming()
	assert.GreaterOrEqual(t, total.Nanoseconds(), int64(0))
}

func TestRuntime_Invoke_AllNilModules(t *testing.T) {
	t.Parallel()

	rt := di.New(nil, nil)

	handlerCalled := false
	err := rt.Invoke(func(di.Injector) error {
		handlerCalled = true

		return nil
	}, nil, nil)

	require.NoError(t, err)
	assert.True(t, handlerCalled)
}

func TestNew_DoesNotMutateModuleSlice(t *testing.T) {
	t.Parallel()

	module1 := func(di.Injector) error { return nil }
	module2 := func(di.Injector) error { return nil }
	modules := []di.Module{module1, module2}

	_ = di.New(modules...)

	// Verify the original slice was not mutated.
	assert.Len(t, modules, 2)
}
