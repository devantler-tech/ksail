package v1alpha1_test

import (
	"encoding/json"
	"strings"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- AutoscalerExpander ---

func TestAutoscalerExpander_Default(t *testing.T) {
	t.Parallel()

	var e v1alpha1.AutoscalerExpander
	assert.Equal(t, v1alpha1.AutoscalerExpanderLeastWaste, e.Default())
}

func TestAutoscalerExpander_ValidValues(t *testing.T) {
	t.Parallel()

	var e v1alpha1.AutoscalerExpander

	values := e.ValidValues()
	assert.Contains(t, values, "Price")
	assert.Contains(t, values, "LeastWaste")
	assert.Contains(t, values, "LeastNodes")
	assert.Contains(t, values, "Random")
	assert.Len(t, values, 4)
}

func TestAutoscalerExpander_StringAndType(t *testing.T) {
	t.Parallel()

	e := v1alpha1.AutoscalerExpanderPrice
	assert.Equal(t, "Price", e.String())
	assert.Equal(t, "AutoscalerExpander", e.Type())
}

func TestAutoscalerExpander_Set(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		expected  v1alpha1.AutoscalerExpander
		wantError bool
	}{
		{"price_lowercase", "price", v1alpha1.AutoscalerExpanderPrice, false},
		{"leastwaste_mixed", "LeastWaste", v1alpha1.AutoscalerExpanderLeastWaste, false},
		{"leastnodes_upper", "LEASTNODES", v1alpha1.AutoscalerExpanderLeastNodes, false},
		{"random", "Random", v1alpha1.AutoscalerExpanderRandom, false},
		{"invalid", "invalid", "", true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var expander v1alpha1.AutoscalerExpander

			err := expander.Set(testCase.input)
			if testCase.wantError {
				require.Error(t, err)
				require.ErrorIs(t, err, v1alpha1.ErrInvalidAutoscalerExpander)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.expected, expander)
			}
		})
	}
}

func TestValidAutoscalerExpanders(t *testing.T) {
	t.Parallel()

	expanders := v1alpha1.ValidAutoscalerExpanders()
	assert.Contains(t, expanders, v1alpha1.AutoscalerExpanderPrice)
	assert.Contains(t, expanders, v1alpha1.AutoscalerExpanderLeastWaste)
	assert.Contains(t, expanders, v1alpha1.AutoscalerExpanderLeastNodes)
	assert.Contains(t, expanders, v1alpha1.AutoscalerExpanderRandom)
	assert.Len(t, expanders, 4)
}

func TestAutoscalerExpanderList_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		list v1alpha1.AutoscalerExpanderList
		want string
	}{
		{"empty_list", v1alpha1.AutoscalerExpanderList{}, ""},
		{
			"single",
			v1alpha1.AutoscalerExpanderList{v1alpha1.AutoscalerExpanderLeastWaste},
			"LeastWaste",
		},
		{
			"multiple",
			v1alpha1.AutoscalerExpanderList{
				v1alpha1.AutoscalerExpanderLeastNodes,
				v1alpha1.AutoscalerExpanderLeastWaste,
			},
			"LeastNodes,LeastWaste",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.want, testCase.list.String())
		})
	}
}

func TestSplitAutoscalerExpanders(t *testing.T) {
	t.Parallel()

	leastNodes := string(v1alpha1.AutoscalerExpanderLeastNodes)
	leastWaste := string(v1alpha1.AutoscalerExpanderLeastWaste)
	bothInOrder := v1alpha1.AutoscalerExpanderList{
		v1alpha1.AutoscalerExpanderLeastNodes,
		v1alpha1.AutoscalerExpanderLeastWaste,
	}

	tests := []struct {
		name string
		raw  string
		want v1alpha1.AutoscalerExpanderList
	}{
		{"empty_input", "", v1alpha1.AutoscalerExpanderList{}},
		{"whitespace", " ", v1alpha1.AutoscalerExpanderList{}},
		{
			"single",
			leastWaste,
			v1alpha1.AutoscalerExpanderList{v1alpha1.AutoscalerExpanderLeastWaste},
		},
		{"comma_separated", leastNodes + "," + leastWaste, bothInOrder},
		{"comma_separated_spaces", leastNodes + ", " + leastWaste, bothInOrder},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.want, v1alpha1.SplitAutoscalerExpanders(testCase.raw))
		})
	}
}

// TestAutoscalerExpanderList_UnmarshalJSON verifies the list decodes from both
// the current array form and the legacy scalar form that older ksail versions
// persisted to cluster state, so spec.json files written before the field became
// a list remain readable after an upgrade (regression for the
// "cannot unmarshal string into ... AutoscalerExpanderList" load failure).
func TestAutoscalerExpanderList_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    v1alpha1.AutoscalerExpanderList
		wantErr bool
	}{
		{
			"legacy_scalar",
			`"LeastWaste"`,
			v1alpha1.AutoscalerExpanderList{v1alpha1.AutoscalerExpanderLeastWaste},
			false,
		},
		{
			"comma_separated_scalar",
			`"LeastNodes,LeastWaste"`,
			v1alpha1.AutoscalerExpanderList{
				v1alpha1.AutoscalerExpanderLeastNodes,
				v1alpha1.AutoscalerExpanderLeastWaste,
			},
			false,
		},
		{
			"array",
			`["LeastNodes","LeastWaste"]`,
			v1alpha1.AutoscalerExpanderList{
				v1alpha1.AutoscalerExpanderLeastNodes,
				v1alpha1.AutoscalerExpanderLeastWaste,
			},
			false,
		},
		{"empty_scalar", `""`, v1alpha1.AutoscalerExpanderList{}, false},
		{"empty_array", `[]`, v1alpha1.AutoscalerExpanderList{}, false},
		{"null", `null`, nil, false},
		{"invalid_json", `{`, nil, true},
		{"number", `123`, nil, true},
		{"array_with_non_string_element", `[123]`, nil, true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var list v1alpha1.AutoscalerExpanderList

			err := json.Unmarshal([]byte(testCase.input), &list)
			if testCase.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.want, list)
		})
	}
}

// --- ValidateAutoscalerConfig ---

func TestPodAutoscalerHorizontal_Default(t *testing.T) {
	t.Parallel()

	var podHorizontal v1alpha1.PodAutoscalerHorizontal
	assert.Equal(t, v1alpha1.PodAutoscalerHorizontalDisabled, podHorizontal.Default())
}

func TestPodAutoscalerHorizontal_ValidValues(t *testing.T) {
	t.Parallel()

	var podHorizontal v1alpha1.PodAutoscalerHorizontal

	values := podHorizontal.ValidValues()
	assert.Contains(t, values, "Enabled")
	assert.Contains(t, values, "Disabled")
	assert.Len(t, values, 2)
}

func TestPodAutoscalerHorizontal_StringAndType(t *testing.T) {
	t.Parallel()

	podHorizontal := v1alpha1.PodAutoscalerHorizontalEnabled
	assert.Equal(t, "Enabled", podHorizontal.String())
	assert.Equal(t, "PodAutoscalerHorizontal", podHorizontal.Type())
}

func TestPodAutoscalerHorizontal_Set(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		expected  v1alpha1.PodAutoscalerHorizontal
		wantError bool
	}{
		{"enabled", "Enabled", v1alpha1.PodAutoscalerHorizontalEnabled, false},
		{"disabled_lower", "disabled", v1alpha1.PodAutoscalerHorizontalDisabled, false},
		{"invalid", "bad", "", true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var podHorizontal v1alpha1.PodAutoscalerHorizontal

			err := podHorizontal.Set(testCase.input)
			if testCase.wantError {
				require.Error(t, err)
				require.ErrorIs(t, err, v1alpha1.ErrInvalidPodAutoscalerHorizontal)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.expected, podHorizontal)
			}
		})
	}
}

func TestValidPodAutoscalerHorizontals(t *testing.T) {
	t.Parallel()

	values := v1alpha1.ValidPodAutoscalerHorizontals()
	assert.Contains(t, values, v1alpha1.PodAutoscalerHorizontalEnabled)
	assert.Contains(t, values, v1alpha1.PodAutoscalerHorizontalDisabled)
	assert.Len(t, values, 2)
}

// --- PodAutoscalerVertical ---

func TestPodAutoscalerVertical_Default(t *testing.T) {
	t.Parallel()

	var podVertical v1alpha1.PodAutoscalerVertical
	assert.Equal(t, v1alpha1.PodAutoscalerVerticalDisabled, podVertical.Default())
}

func TestPodAutoscalerVertical_ValidValues(t *testing.T) {
	t.Parallel()

	var podVertical v1alpha1.PodAutoscalerVertical

	values := podVertical.ValidValues()
	assert.Contains(t, values, "Enabled")
	assert.Contains(t, values, "Disabled")
	assert.Len(t, values, 2)
}

func TestPodAutoscalerVertical_StringAndType(t *testing.T) {
	t.Parallel()

	podVertical := v1alpha1.PodAutoscalerVerticalEnabled
	assert.Equal(t, "Enabled", podVertical.String())
	assert.Equal(t, "PodAutoscalerVertical", podVertical.Type())
}

func TestPodAutoscalerVertical_Set(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		expected  v1alpha1.PodAutoscalerVertical
		wantError bool
	}{
		{"enabled_upper", "ENABLED", v1alpha1.PodAutoscalerVerticalEnabled, false},
		{"disabled", "Disabled", v1alpha1.PodAutoscalerVerticalDisabled, false},
		{"invalid", "nope", "", true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var podVertical v1alpha1.PodAutoscalerVertical

			err := podVertical.Set(testCase.input)
			if testCase.wantError {
				require.Error(t, err)
				require.ErrorIs(t, err, v1alpha1.ErrInvalidPodAutoscalerVertical)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.expected, podVertical)
			}
		})
	}
}

func TestValidPodAutoscalerVerticals(t *testing.T) {
	t.Parallel()

	values := v1alpha1.ValidPodAutoscalerVerticals()
	assert.Contains(t, values, v1alpha1.PodAutoscalerVerticalEnabled)
	assert.Contains(t, values, v1alpha1.PodAutoscalerVerticalDisabled)
	assert.Len(t, values, 2)
}

// --- ValidateAutoscalerConfig ---

//nolint:funlen,maintidx // Table-driven test with comprehensive coverage.
func TestValidateAutoscalerConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cluster     *v1alpha1.ClusterSpec
		provider    *v1alpha1.ProviderSpec
		wantErr     error
		errContains string
	}{
		{
			name:    "nil cluster returns nil",
			cluster: nil,
			wantErr: nil,
		},
		{
			name:    "empty autoscaler is valid",
			cluster: &v1alpha1.ClusterSpec{},
			wantErr: nil,
		},
		{
			name: "valid pool",
			cluster: &v1alpha1.ClusterSpec{
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
						},
					},
				},
			},
			wantErr: nil,
		},
		{
			name: "pool name exceeds 63 characters",
			cluster: &v1alpha1.ClusterSpec{
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Pools: []v1alpha1.NodePool{
							{
								Name:       "a" + strings.Repeat("b", 63),
								ServerType: "cx23",
								Location:   "fsn1",
								Min:        1,
								Max:        5,
							},
						},
					},
				},
			},
			wantErr: v1alpha1.ErrInvalidPoolName,
		},
		{
			name: "pool name with underscore is invalid",
			cluster: &v1alpha1.ClusterSpec{
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Pools: []v1alpha1.NodePool{
							{Name: "my_pool", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
						},
					},
				},
			},
			wantErr:     v1alpha1.ErrInvalidPoolName,
			errContains: "my_pool",
		},
		{
			name: "pool name starts with number is valid (DNS-1123 allows digit start)",
			cluster: &v1alpha1.ClusterSpec{
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Pools: []v1alpha1.NodePool{
							{Name: "1pool", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
						},
					},
				},
			},
			wantErr: nil,
		},
		{
			name: "pool name starts with hyphen is invalid",
			cluster: &v1alpha1.ClusterSpec{
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Pools: []v1alpha1.NodePool{
							{Name: "-pool", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
						},
					},
				},
			},
			wantErr: v1alpha1.ErrInvalidPoolName,
		},
		{
			name: "pool serverType empty is invalid",
			cluster: &v1alpha1.ClusterSpec{
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "", Location: "fsn1", Min: 1, Max: 5},
						},
					},
				},
			},
			wantErr: v1alpha1.ErrPoolServerTypeEmpty,
		},
		{
			name: "pool location empty is invalid",
			cluster: &v1alpha1.ClusterSpec{
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "", Min: 1, Max: 5},
						},
					},
				},
			},
			wantErr: v1alpha1.ErrPoolLocationEmpty,
		},
		{
			name: "pool with valid labels and taints",
			cluster: &v1alpha1.ClusterSpec{
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Pools: []v1alpha1.NodePool{
							{
								Name:       "gpu",
								ServerType: "cx23",
								Location:   "fsn1",
								Min:        0,
								Max:        3,
								Labels: map[string]string{
									"workload":               "gpu",
									"team.example.com/owner": "ml",
								},
								Taints: []v1alpha1.NodePoolTaint{
									{
										Key:    "dedicated",
										Value:  "gpu",
										Effect: v1alpha1.TaintEffectNoSchedule,
									},
									{Key: "spot", Effect: v1alpha1.TaintEffectNoExecute},
								},
							},
						},
					},
				},
			},
			wantErr: nil,
		},
		{
			name: "pool with invalid label key is invalid",
			cluster: &v1alpha1.ClusterSpec{
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Pools: []v1alpha1.NodePool{
							{
								Name:       "workers",
								ServerType: "cx23",
								Location:   "fsn1",
								Min:        1,
								Max:        5,
								Labels:     map[string]string{"Invalid Key!": "value"},
							},
						},
					},
				},
			},
			wantErr: v1alpha1.ErrInvalidPoolLabel,
		},
		{
			name: "pool with invalid label value is invalid",
			cluster: &v1alpha1.ClusterSpec{
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Pools: []v1alpha1.NodePool{
							{
								Name:       "workers",
								ServerType: "cx23",
								Location:   "fsn1",
								Min:        1,
								Max:        5,
								Labels:     map[string]string{"workload": "not a valid value!"},
							},
						},
					},
				},
			},
			wantErr: v1alpha1.ErrInvalidPoolLabel,
		},
		{
			name: "pool with invalid taint key is invalid",
			cluster: &v1alpha1.ClusterSpec{
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Pools: []v1alpha1.NodePool{
							{
								Name:       "workers",
								ServerType: "cx23",
								Location:   "fsn1",
								Min:        1,
								Max:        5,
								Taints: []v1alpha1.NodePoolTaint{
									{Key: "bad key", Effect: v1alpha1.TaintEffectNoSchedule},
								},
							},
						},
					},
				},
			},
			wantErr: v1alpha1.ErrInvalidPoolTaint,
		},
		{
			name: "pool with invalid taint effect is invalid",
			cluster: &v1alpha1.ClusterSpec{
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Pools: []v1alpha1.NodePool{
							{
								Name:       "workers",
								ServerType: "cx23",
								Location:   "fsn1",
								Min:        1,
								Max:        5,
								Taints: []v1alpha1.NodePoolTaint{
									{Key: "dedicated", Value: "gpu", Effect: "Nonsense"},
								},
							},
						},
					},
				},
			},
			wantErr: v1alpha1.ErrInvalidPoolTaint,
		},
		{
			name: "node autoscaler enabled with no pools is invalid",
			cluster: &v1alpha1.ClusterSpec{
				Provider: v1alpha1.ProviderHetzner,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled: v1alpha1.NodeAutoscalerEnabledEnabled,
						Pools:   []v1alpha1.NodePool{},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr: v1alpha1.ErrAutoscalerEnabledNoPools,
		},
		{
			name: "pool min exceeds max",
			cluster: &v1alpha1.ClusterSpec{
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 5, Max: 3},
						},
					},
				},
			},
			wantErr:     v1alpha1.ErrPoolMinExceedsMax,
			errContains: "workers",
		},
		{
			name: "pool with negative min is invalid",
			cluster: &v1alpha1.ClusterSpec{
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Pools: []v1alpha1.NodePool{
							{
								Name:       "workers",
								ServerType: "cx23",
								Location:   "fsn1",
								Min:        -1,
								Max:        5,
							},
						},
					},
				},
			},
			wantErr: v1alpha1.ErrInvalidPoolCapacity,
		},
		{
			name: "pool with negative max is invalid",
			cluster: &v1alpha1.ClusterSpec{
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Pools: []v1alpha1.NodePool{
							{
								Name:       "workers",
								ServerType: "cx23",
								Location:   "fsn1",
								Min:        0,
								Max:        -1,
							},
						},
					},
				},
			},
			wantErr: v1alpha1.ErrInvalidPoolCapacity,
		},
		{
			name: "duplicate pool name",
			cluster: &v1alpha1.ClusterSpec{
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
							{
								Name:       "workers",
								ServerType: "cax11",
								Location:   "nbg1",
								Min:        0,
								Max:        3,
							},
						},
					},
				},
			},
			wantErr:     v1alpha1.ErrDuplicatePoolName,
			errContains: "workers",
		},
		{
			name: "capacity guard: total exceeds server limit",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 3,
				Workers:       2,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled: v1alpha1.NodeAutoscalerEnabledEnabled,
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 8},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr:     v1alpha1.ErrAutoscalerExceedsServerLimit,
			errContains: "exceeds serverLimit",
		},
		{
			name: "capacity guard: total within server limit",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 3,
				Workers:       2,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled: v1alpha1.NodeAutoscalerEnabledEnabled,
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr: nil,
		},
		{
			name: "capacity guard skipped when autoscaler disabled",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 3,
				Workers:       2,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled: v1alpha1.NodeAutoscalerEnabledDisabled,
						Pools: []v1alpha1.NodePool{
							{
								Name:       "workers",
								ServerType: "cx23",
								Location:   "fsn1",
								Min:        1,
								Max:        100,
							},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr: nil,
		},
		{
			name: "capacity guard skipped for non-hetzner provider",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderDocker,
				ControlPlanes: 3,
				Workers:       2,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled: v1alpha1.NodeAutoscalerEnabledEnabled,
						Pools: []v1alpha1.NodePool{
							{
								Name:       "workers",
								ServerType: "cx23",
								Location:   "fsn1",
								Min:        1,
								Max:        100,
							},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{},
			wantErr:  nil,
		},
		{
			name: "capacity guard uses default server limit when zero",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 3,
				Workers:       2,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled: v1alpha1.NodeAutoscalerEnabledEnabled,
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
						},
					},
				},
			},
			// ServerLimit = 0 → falls back to DefaultHetznerServerLimit (10)
			// total = 3 + 2 + 5 = 10, equals limit → valid
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 0},
			},
			wantErr: nil,
		},
		{
			name: "capacity guard: maxNodesTotal caps the cluster total",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 1,
				Workers:       1,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled:       v1alpha1.NodeAutoscalerEnabledEnabled,
						MaxNodesTotal: 8,
						Pools: []v1alpha1.NodePool{
							// baseline 2 + poolCapacity 10 = 12, clamped by
							// MaxNodesTotal(8) → reachableTotal 8 ≤ serverLimit(10) → valid
							{
								Name:       "workers",
								ServerType: "cx23",
								Location:   "fsn1",
								Min:        1,
								Max:        10,
							},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr: nil,
		},
		{
			// Regression for #5017: MaxNodesTotal is the TOTAL ceiling (== the
			// --max-nodes-total flag), not an autoscaler-only budget added on top of
			// the baseline. With baseline 6 + poolCapacity 10, the old logic computed
			// 6 + min(10,10) = 16 and wrongly rejected, even though MaxNodesTotal(10)
			// equals serverLimit(10). reachableTotal = min(16, 10) = 10 → valid.
			//
			// Distribution is deliberately NOT Talos: only the Talos Hetzner path builds
			// a snapshot, so only it reserves a slot (#6171). Every other distribution is
			// entitled to the full serverLimit, which is what this case pins.
			name: "capacity guard: maxNodesTotal equal to serverLimit is valid for a non-Talos distribution",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				Distribution:  v1alpha1.DistributionK3s,
				ControlPlanes: 3,
				Workers:       3,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled:       v1alpha1.NodeAutoscalerEnabledEnabled,
						MaxNodesTotal: 10,
						Pools: []v1alpha1.NodePool{
							{
								Name:       "workers",
								ServerType: "cx23",
								Location:   "fsn1",
								Min:        1,
								Max:        10,
							},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr: nil,
		},
		{
			// #6171: the Talos Hetzner path builds the autoscaler's node image by booting
			// ONE temporary server, so a cluster that can reach serverLimit leaves that
			// server nowhere to go. It fails LATE — fine while the pinned version's
			// snapshot exists, then every deploy dies on resource_limit_exceeded after a
			// Talos bump. Reject it as a config error instead.
			name: "snapshot slot: Talos reachableTotal equal to serverLimit is rejected",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				Distribution:  v1alpha1.DistributionTalos,
				ControlPlanes: 3,
				Workers:       3,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled:       v1alpha1.NodeAutoscalerEnabledEnabled,
						MaxNodesTotal: 10,
						Pools: []v1alpha1.NodePool{
							{
								Name:       "workers",
								ServerType: "cx23",
								Location:   "fsn1",
								Min:        1,
								Max:        10,
							},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr: v1alpha1.ErrAutoscalerLeavesNoSnapshotSlot,
		},
		{
			// One below the limit leaves exactly the reserved slot free → valid. Pins the
			// boundary so the reserve can't silently grow or vanish.
			name: "snapshot slot: Talos reachableTotal one below serverLimit is valid",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				Distribution:  v1alpha1.DistributionTalos,
				ControlPlanes: 3,
				Workers:       3,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled:       v1alpha1.NodeAutoscalerEnabledEnabled,
						MaxNodesTotal: 9,
						Pools: []v1alpha1.NodePool{
							{
								Name:       "workers",
								ServerType: "cx23",
								Location:   "fsn1",
								Min:        1,
								Max:        10,
							},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr: nil,
		},
		{
			// The reserve must not fire when the autoscaler is off: no autoscaler secret
			// means no snapshot build, so there is no temporary server to make room for.
			name: "snapshot slot: Talos at serverLimit with autoscaler disabled is valid",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				Distribution:  v1alpha1.DistributionTalos,
				ControlPlanes: 3,
				Workers:       7,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled: v1alpha1.NodeAutoscalerEnabledDisabled,
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr: nil,
		},
		{
			// Uncapped (MaxNodesTotal=0) pools may still blow the server limit:
			// baseline 6 + poolCapacity 10 = 16 > serverLimit(10) → rejected.
			name: "capacity guard: uncapped pools exceeding serverLimit are rejected",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 3,
				Workers:       3,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled: v1alpha1.NodeAutoscalerEnabledEnabled,
						Pools: []v1alpha1.NodePool{
							{
								Name:       "workers",
								ServerType: "cx23",
								Location:   "fsn1",
								Min:        1,
								Max:        10,
							},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr:     v1alpha1.ErrAutoscalerExceedsServerLimit,
			errContains: "exceeds serverLimit",
		},
		{
			// A MaxNodesTotal below the static baseline must not hide a baseline that
			// already exceeds serverLimit: control-planes + workers are provisioned
			// unconditionally, regardless of the autoscaler ceiling. baseline 8 alone
			// exceeds serverLimit(6); clamping reachableTotal down to MaxNodesTotal(5)
			// must not drop below the baseline → 8 > 6 → rejected.
			name: "capacity guard: maxNodesTotal below baseline cannot hide baseline over serverLimit",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 4,
				Workers:       4,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled:       v1alpha1.NodeAutoscalerEnabledEnabled,
						MaxNodesTotal: 5,
						Pools: []v1alpha1.NodePool{
							{
								Name:       "workers",
								ServerType: "cx23",
								Location:   "fsn1",
								Min:        1,
								Max:        2,
							},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 6},
			},
			wantErr:     v1alpha1.ErrAutoscalerExceedsServerLimit,
			errContains: "exceeds serverLimit",
		},
		{
			name: "capacity guard: negative maxNodesTotal is invalid",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 1,
				Workers:       1,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled:       v1alpha1.NodeAutoscalerEnabledEnabled,
						MaxNodesTotal: -1,
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 0, Max: 5},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr: v1alpha1.ErrInvalidMaxNodesTotal,
		},
		{
			name: "scaleDownUtilizationThreshold: non-numeric is invalid",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 1,
				Workers:       1,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled:                       v1alpha1.NodeAutoscalerEnabledEnabled,
						ScaleDownUtilizationThreshold: "abc",
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 0, Max: 5},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr: v1alpha1.ErrInvalidScaleDownUtilizationThreshold,
		},
		{
			name: "scaleDownUtilizationThreshold: out-of-range is invalid",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 1,
				Workers:       1,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled:                       v1alpha1.NodeAutoscalerEnabledEnabled,
						ScaleDownUtilizationThreshold: "1.5",
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 0, Max: 5},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr: v1alpha1.ErrInvalidScaleDownUtilizationThreshold,
		},
		{
			name: "scaleDownUtilizationThreshold: NaN is invalid",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 1,
				Workers:       1,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled:                       v1alpha1.NodeAutoscalerEnabledEnabled,
						ScaleDownUtilizationThreshold: "NaN",
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 0, Max: 5},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr: v1alpha1.ErrInvalidScaleDownUtilizationThreshold,
		},
		{
			name: "scaleDownUtilizationThreshold: valid ratio passes",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 1,
				Workers:       1,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled:                       v1alpha1.NodeAutoscalerEnabledEnabled,
						ScaleDownUtilizationThreshold: "0.65",
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 0, Max: 5},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr: nil,
		},
		{
			name: "capacity guard: negative serverLimit is invalid",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 1,
				Workers:       1,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled: v1alpha1.NodeAutoscalerEnabledEnabled,
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 0, Max: 5},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: -1},
			},
			wantErr: v1alpha1.ErrInvalidServerLimit,
		},
		{
			name: "price expander rejected for hetzner provider",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 1,
				Workers:       1,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled:  v1alpha1.NodeAutoscalerEnabledEnabled,
						Expander: v1alpha1.AutoscalerExpanderList{v1alpha1.AutoscalerExpanderPrice},
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr:     v1alpha1.ErrExpanderNotSupportedForProvider,
			errContains: "pricing API",
		},
		{
			name: "price expander accepted for docker provider",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderDocker,
				ControlPlanes: 1,
				Workers:       1,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled:  v1alpha1.NodeAutoscalerEnabledEnabled,
						Expander: v1alpha1.AutoscalerExpanderList{v1alpha1.AutoscalerExpanderPrice},
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{},
			wantErr:  nil,
		},
		{
			name: "least-waste expander accepted for hetzner provider",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 1,
				Workers:       1,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled: v1alpha1.NodeAutoscalerEnabledEnabled,
						Expander: v1alpha1.AutoscalerExpanderList{
							v1alpha1.AutoscalerExpanderLeastWaste,
						},
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr: nil,
		},
		{
			name: "expander priority list accepted for hetzner provider",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 1,
				Workers:       1,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled: v1alpha1.NodeAutoscalerEnabledEnabled,
						Expander: v1alpha1.AutoscalerExpanderList{
							v1alpha1.AutoscalerExpanderLeastNodes,
							v1alpha1.AutoscalerExpanderLeastWaste,
						},
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr: nil,
		},
		{
			name: "price in expander priority list rejected for hetzner provider",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 1,
				Workers:       1,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled: v1alpha1.NodeAutoscalerEnabledEnabled,
						Expander: v1alpha1.AutoscalerExpanderList{
							v1alpha1.AutoscalerExpanderLeastNodes,
							v1alpha1.AutoscalerExpanderPrice,
						},
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{ServerLimit: 10},
			},
			wantErr:     v1alpha1.ErrExpanderNotSupportedForProvider,
			errContains: "pricing API",
		},
		{
			name: "invalid expander value rejected",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderDocker,
				ControlPlanes: 1,
				Workers:       1,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled:  v1alpha1.NodeAutoscalerEnabledEnabled,
						Expander: v1alpha1.AutoscalerExpanderList{"Bogus"},
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{},
			wantErr:  v1alpha1.ErrInvalidAutoscalerExpander,
		},
		{
			name: "duplicate expander value rejected",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderDocker,
				ControlPlanes: 1,
				Workers:       1,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled: v1alpha1.NodeAutoscalerEnabledEnabled,
						Expander: v1alpha1.AutoscalerExpanderList{
							v1alpha1.AutoscalerExpanderLeastWaste,
							v1alpha1.AutoscalerExpanderLeastWaste,
						},
						Pools: []v1alpha1.NodePool{
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
						},
					},
				},
			},
			provider: &v1alpha1.ProviderSpec{},
			wantErr:  v1alpha1.ErrDuplicateAutoscalerExpander,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := v1alpha1.ValidateAutoscalerConfig(testCase.cluster, testCase.provider)

			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr)

				if testCase.errContains != "" {
					assert.Contains(t, err.Error(), testCase.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
