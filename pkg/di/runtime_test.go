package di_test

import (
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errHandler = errors.New("handler error")
	errModule  = errors.New("module error")
)

func TestNew_EmptyModules(t *testing.T) {
	t.Parallel()

	rt := di.New()

	require.NotNil(t, rt)
}

func TestNew_WithModules(t *testing.T) {
	t.Parallel()

	called := false
	module := func(_ di.Injector) error {
		called = true

		return nil
	}

	runtime := di.New(module)
	require.NotNil(t, runtime)

	err := runtime.Invoke(func(di.Injector) error {
		return nil
	})

	require.NoError(t, err)
	assert.True(t, called, "module should be invoked")
}

func TestRuntime_Invoke_Success(t *testing.T) {
	t.Parallel()

	rt := di.New()

	handlerCalled := false
	err := rt.Invoke(func(di.Injector) error {
		handlerCalled = true

		return nil
	})

	require.NoError(t, err)
	assert.True(t, handlerCalled)
}

func TestRuntime_Invoke_HandlerError(t *testing.T) {
	t.Parallel()

	runtime := di.New()

	err := runtime.Invoke(func(di.Injector) error {
		return errHandler
	})

	require.Error(t, err)
	assert.Equal(t, errHandler, err)
}

func TestRuntime_Invoke_ModuleError(t *testing.T) {
	t.Parallel()

	failingModule := func(di.Injector) error {
		return errModule
	}

	runtime := di.New(failingModule)

	err := runtime.Invoke(func(di.Injector) error {
		t.Fatal("handler should not be called when module fails")

		return nil
	})

	require.Error(t, err)
	assert.Equal(t, errModule, err)
}

func TestRuntime_Invoke_WithExtraModules(t *testing.T) {
	t.Parallel()

	baseModuleCalled := false
	baseModule := func(_ di.Injector) error {
		baseModuleCalled = true

		return nil
	}

	runtime := di.New(baseModule)

	extraModuleCalled := false
	extraModule := func(_ di.Injector) error {
		extraModuleCalled = true

		return nil
	}

	err := runtime.Invoke(func(di.Injector) error {
		return nil
	}, extraModule)

	require.NoError(t, err)
	assert.True(t, baseModuleCalled, "base module should be called")
	assert.True(t, extraModuleCalled, "extra module should be called")
}

func TestRuntime_Invoke_ModuleOrder(t *testing.T) {
	t.Parallel()

	var order []int

	module1 := func(_ di.Injector) error {
		order = append(order, 1)

		return nil
	}

	module2 := func(_ di.Injector) error {
		order = append(order, 2)

		return nil
	}

	runtime := di.New(module1)

	module3 := func(_ di.Injector) error {
		order = append(order, 3)

		return nil
	}

	err := runtime.Invoke(func(di.Injector) error {
		order = append(order, 4)

		return nil
	}, module2, module3)

	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3, 4}, order, "modules should execute in order")
}

func TestRuntime_Invoke_NilModule(t *testing.T) {
	t.Parallel()

	runtime := di.New(nil)

	err := runtime.Invoke(func(di.Injector) error {
		return nil
	}, nil)

	require.NoError(t, err, "nil modules should be skipped")
}

func TestRuntime_Invoke_DependencyResolution(t *testing.T) {
	t.Parallel()

	type TestService struct {
		Name string
	}

	module := func(i di.Injector) error {
		do.Provide(i, func(di.Injector) (*TestService, error) {
			return &TestService{Name: "test"}, nil
		})

		return nil
	}

	rt := di.New(module)

	var service *TestService

	err := rt.Invoke(func(i di.Injector) error {
		var resolveErr error

		service, resolveErr = do.Invoke[*TestService](i)
		if resolveErr != nil {
			return resolveErr
		}

		return nil
	})

	require.NoError(t, err)
	require.NotNil(t, service)
	assert.Equal(t, "test", service.Name)
}

func TestRunEWithRuntime_Success(t *testing.T) {
	t.Parallel()

	runtime := di.New()

	handlerCalled := false

	var receivedCmd *cobra.Command

	runE := di.RunEWithRuntime(runtime, func(cmd *cobra.Command, _ di.Injector) error {
		handlerCalled = true
		receivedCmd = cmd

		return nil
	})

	cmd := &cobra.Command{Use: "test"}
	err := runE(cmd, nil)

	require.NoError(t, err)
	assert.True(t, handlerCalled)
	assert.Equal(t, cmd, receivedCmd)
}

func TestRunEWithRuntime_HandlerError(t *testing.T) {
	t.Parallel()

	runtime := di.New()

	runE := di.RunEWithRuntime(runtime, func(*cobra.Command, di.Injector) error {
		return errHandler
	})

	cmd := &cobra.Command{Use: "test"}
	err := runE(cmd, nil)

	require.Error(t, err)
	assert.Equal(t, errHandler, err)
}

func TestRunEWithRuntime_WithDependencies(t *testing.T) {
	t.Parallel()

	type Config struct {
		Value string
	}

	module := func(i di.Injector) error {
		do.Provide(i, func(di.Injector) (*Config, error) {
			return &Config{Value: "configured"}, nil
		})

		return nil
	}

	rt := di.New(module)

	var resolvedConfig *Config

	runE := di.RunEWithRuntime(rt, func(_ *cobra.Command, injector di.Injector) error {
		var err error

		resolvedConfig, err = do.Invoke[*Config](injector)

		return err
	})

	cmd := &cobra.Command{Use: "test"}
	err := runE(cmd, nil)

	require.NoError(t, err)
	require.NotNil(t, resolvedConfig)
	assert.Equal(t, "configured", resolvedConfig.Value)
}

func TestRuntime_Invoke_InjectorShutdown(t *testing.T) {
	t.Parallel()

	runtime := di.New()

	var capturedInjector di.Injector

	err := runtime.Invoke(func(injector di.Injector) error {
		capturedInjector = injector

		return nil
	})

	require.NoError(t, err)
	require.NotNil(t, capturedInjector)

	// After invoke completes, the injector should be shut down
	// We can't directly test shutdown state, but we can verify the injector exists
}

func TestRuntime_Invoke_MultipleInvocations(t *testing.T) {
	t.Parallel()

	runtime := di.New()

	// First invocation
	err1 := runtime.Invoke(func(di.Injector) error {
		return nil
	})
	require.NoError(t, err1)

	// Second invocation - should create a fresh injector
	err2 := runtime.Invoke(func(di.Injector) error {
		return nil
	})
	require.NoError(t, err2)
}
