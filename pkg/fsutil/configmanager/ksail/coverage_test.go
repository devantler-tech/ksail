package configmanager_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestSetFieldValueFromFlag_String tests string field setting.
func TestSetFieldValueFromFlag_String(t *testing.T) {
	t.Parallel()

	var s string

	err := configmanager.SetFieldValueFromFlagForTest(&s, "hello")
	require.NoError(t, err)
	assert.Equal(t, "hello", s)
}

// TestSetFieldValueFromFlag_Duration tests duration field setting.
//
//nolint:varnamelen // Short names keep the table-driven tests readable.
func TestSetFieldValueFromFlag_Duration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantDur time.Duration
		wantErr bool
	}{
		{name: "valid duration", raw: "5m", wantDur: 5 * time.Minute},
		{name: "empty string", raw: "", wantDur: 0},
		{name: "invalid duration", raw: "not-a-duration", wantErr: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var d metav1.Duration

			err := configmanager.SetFieldValueFromFlagForTest(&d, testCase.raw)

			if testCase.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "parse duration")
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.wantDur, d.Duration)
			}
		})
	}
}

// TestSetFieldValueFromFlag_Bool tests bool field setting.
//
//nolint:varnamelen // Short names keep the table-driven tests readable.
func TestSetFieldValueFromFlag_Bool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    bool
		wantErr bool
	}{
		{name: "true", raw: "true", want: true},
		{name: "false", raw: "false", want: false},
		{name: "empty string", raw: "", want: false},
		{name: "invalid bool", raw: "maybe", wantErr: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var b bool

			err := configmanager.SetFieldValueFromFlagForTest(&b, testCase.raw)

			if testCase.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "parse bool")
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.want, b)
			}
		})
	}
}

// TestSetFieldValueFromFlag_Int32 tests int32 field setting.
//
//nolint:varnamelen // Short names keep the table-driven tests readable.
func TestSetFieldValueFromFlag_Int32(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    int32
		wantErr bool
	}{
		{name: "valid int", raw: "42", want: 42},
		{name: "zero", raw: "0", want: 0},
		{name: "negative", raw: "-1", want: -1},
		{name: "empty string", raw: "", want: 0},
		{name: "invalid int", raw: "not-a-number", wantErr: true},
		{name: "overflow", raw: "99999999999", wantErr: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var i int32

			err := configmanager.SetFieldValueFromFlagForTest(&i, testCase.raw)

			if testCase.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "parse int32")
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.want, i)
			}
		})
	}
}

// TestSetFieldValueFromFlag_Unsupported tests unsupported type error.
func TestSetFieldValueFromFlag_Unsupported(t *testing.T) {
	t.Parallel()

	var f float64

	err := configmanager.SetFieldValueFromFlagForTest(&f, "1.0")

	require.Error(t, err)
	assert.ErrorIs(t, err, configmanager.ErrUnsupportedFlagFieldTypeForTest)
}

// TestMetaV1DurationDecodeHook tests the decode hook for various conversions.
//
//nolint:funlen // Table-driven test coverage is naturally long.
func TestMetaV1DurationDecodeHook(t *testing.T) {
	t.Parallel()

	hook := configmanager.MetaV1DurationDecodeHookForTest()

	t.Run("string to metav1.Duration", func(t *testing.T) {
		t.Parallel()

		result, err := hook(
			reflect.TypeFor[string](),
			reflect.TypeFor[metav1.Duration](),
			"5m",
		)
		require.NoError(t, err)

		dur, ok := result.(metav1.Duration)
		require.True(t, ok)
		assert.Equal(t, 5*time.Minute, dur.Duration)
	})

	t.Run("string to *metav1.Duration", func(t *testing.T) {
		t.Parallel()

		result, err := hook(
			reflect.TypeFor[string](),
			reflect.TypeFor[*metav1.Duration](),
			"30s",
		)
		require.NoError(t, err)

		dur, ok := result.(*metav1.Duration)
		require.True(t, ok)
		assert.Equal(t, 30*time.Second, dur.Duration)
	})

	t.Run("empty string to metav1.Duration", func(t *testing.T) {
		t.Parallel()

		result, err := hook(
			reflect.TypeFor[string](),
			reflect.TypeFor[metav1.Duration](),
			"",
		)
		require.NoError(t, err)

		dur, ok := result.(metav1.Duration)
		require.True(t, ok)
		assert.Equal(t, time.Duration(0), dur.Duration)
	})

	t.Run("empty string to *metav1.Duration", func(t *testing.T) {
		t.Parallel()

		result, err := hook(
			reflect.TypeFor[string](),
			reflect.TypeFor[*metav1.Duration](),
			"",
		)
		require.NoError(t, err)

		dur, ok := result.(*metav1.Duration)
		require.True(t, ok)
		assert.Equal(t, time.Duration(0), dur.Duration)
	})

	t.Run("invalid duration string", func(t *testing.T) {
		t.Parallel()

		_, err := hook(
			reflect.TypeFor[string](),
			reflect.TypeFor[metav1.Duration](),
			"not-a-duration",
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse duration")
	})

	t.Run("non-string type passthrough", func(t *testing.T) {
		t.Parallel()

		result, err := hook(
			reflect.TypeFor[int](),
			reflect.TypeFor[metav1.Duration](),
			42,
		)
		require.NoError(t, err)
		assert.Equal(t, 42, result)
	})

	t.Run("non-duration target passthrough", func(t *testing.T) {
		t.Parallel()

		result, err := hook(
			reflect.TypeFor[string](),
			reflect.TypeFor[string](),
			"hello",
		)
		require.NoError(t, err)
		assert.Equal(t, "hello", result)
	})

	t.Run("non-string data type assertion failure", func(t *testing.T) {
		t.Parallel()

		// Use a string reflect type but non-string actual data
		// This shouldn't normally happen, but we test the defensive branch
		result, err := hook(
			reflect.TypeFor[string](),
			reflect.TypeFor[metav1.Duration](),
			42, // int data but string type
		)
		// The type assertion (data.(string)) should return ok=false
		// and the function returns data unchanged
		require.NoError(t, err)
		assert.Equal(t, 42, result)
	})
}

// TestResolveVClusterName tests the vCluster name resolution logic.
func TestResolveVClusterName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		context string
		want    string
	}{
		{
			name:    "empty context returns default",
			context: "",
			want:    "vcluster-default",
		},
		{
			name:    "vcluster-docker prefix extracts name",
			context: "vcluster-docker_my-vcluster",
			want:    "my-vcluster",
		},
		{
			name:    "non-vcluster context returns default",
			context: "kind-my-cluster",
			want:    "vcluster-default",
		},
		{
			name:    "vcluster-docker with empty name returns default",
			context: "vcluster-docker_",
			want:    "vcluster-default",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mgr := configmanager.NewConfigManager(nil, "ksail.yaml")
			mgr.Config = &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Connection: v1alpha1.Connection{
							Context: testCase.context,
						},
					},
				},
			}

			got := mgr.ResolveVClusterNameForTest()
			assert.Equal(t, testCase.want, got)
		})
	}
}

// TestGetDefaultTalosPatches tests the default Talos patches generation.
func TestGetDefaultTalosPatches(t *testing.T) {
	t.Parallel()

	t.Run("no metrics server returns empty", func(t *testing.T) {
		t.Parallel()

		mgr := configmanager.NewConfigManager(nil, "ksail.yaml")
		mgr.Config = &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					MetricsServer: v1alpha1.MetricsServerDefault,
				},
			},
		}

		patches := mgr.GetDefaultTalosPatchesForTest()
		assert.Empty(t, patches)
	})

	t.Run("metrics server enabled returns kubelet patches", func(t *testing.T) {
		t.Parallel()

		mgr := configmanager.NewConfigManager(nil, "ksail.yaml")
		mgr.Config = &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					MetricsServer: v1alpha1.MetricsServerEnabled,
				},
			},
		}

		patches := mgr.GetDefaultTalosPatchesForTest()
		require.Len(t, patches, 2)
		assert.Contains(t, string(patches[0].Content), "rotate-server-certificates")
		assert.Contains(t, string(patches[1].Content), "kubelet-serving-cert-approver")
	})
}
