package reconciler_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/client/reconciler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

// TestNewBaseWithClient tests that NewBaseWithClient creates a Base with the provided client.
func TestNewBaseWithClient(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	fakeClient := fake.NewSimpleDynamicClient(scheme)

	base := reconciler.NewBaseWithClient(fakeClient)

	require.NotNil(t, base)
	assert.Equal(t, fakeClient, base.Dynamic)
	assert.Empty(t, base.KubeconfigPath)
}

// TestNewBaseWithClient_NilClient tests that NewBaseWithClient handles nil client gracefully.
func TestNewBaseWithClient_NilClient(t *testing.T) {
	t.Parallel()

	base := reconciler.NewBaseWithClient(nil)

	require.NotNil(t, base)
	assert.Nil(t, base.Dynamic)
}

// TestNewBase_InvalidPath tests NewBase with an invalid kubeconfig path.
func TestNewBase_InvalidPath(t *testing.T) {
	t.Parallel()

	_, err := reconciler.NewBase("/nonexistent/path/to/kubeconfig")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "build rest config")
}

// SampleReconciler is a test type that embeds Base.
type SampleReconciler struct {
	*reconciler.Base

	Name string
}

// TestNewWithClient tests the generic NewWithClient function.
func TestNewWithClient(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	fakeClient := fake.NewSimpleDynamicClient(scheme)

	constructor := func(base *reconciler.Base) *SampleReconciler {
		return &SampleReconciler{
			Base: base,
			Name: "test-reconciler",
		}
	}

	result := reconciler.NewWithClient(fakeClient, constructor)

	require.NotNil(t, result)
	assert.Equal(t, "test-reconciler", result.Name)
	assert.Equal(t, fakeClient, result.Dynamic)
}

// TestNewWithClient_DifferentTypes tests NewWithClient with different custom types.
func TestNewWithClient_DifferentTypes(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	fakeClient := fake.NewSimpleDynamicClient(scheme)

	// Test with a value type constructor
	type ValueReconciler struct {
		Base    *reconciler.Base
		Counter int
	}

	constructor := func(base *reconciler.Base) ValueReconciler {
		return ValueReconciler{
			Base:    base,
			Counter: 42,
		}
	}

	result := reconciler.NewWithClient(fakeClient, constructor)

	assert.Equal(t, 42, result.Counter)
	assert.NotNil(t, result.Base)
}

// TestNew_InvalidKubeconfig tests New with an invalid kubeconfig path.
func TestNew_InvalidKubeconfig(t *testing.T) {
	t.Parallel()

	constructor := func(base *reconciler.Base) *SampleReconciler {
		return &SampleReconciler{Base: base, Name: "test"}
	}

	result, err := reconciler.New("/invalid/kubeconfig", constructor)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "create reconciler base")
}

// TestBaseStruct tests that Base struct fields are properly accessible.
func TestBaseStruct(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	fakeClient := fake.NewSimpleDynamicClient(scheme)

	base := &reconciler.Base{
		Dynamic:        fakeClient,
		KubeconfigPath: "/path/to/kubeconfig",
	}

	assert.Equal(t, fakeClient, base.Dynamic)
	assert.Equal(t, "/path/to/kubeconfig", base.KubeconfigPath)
}
