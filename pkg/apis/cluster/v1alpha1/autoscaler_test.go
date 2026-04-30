package v1alpha1_test

import (
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

// --- NodeAutoscalerEnabled ---

func TestNodeAutoscalerEnabled_Default(t *testing.T) {
	t.Parallel()

	var nodeEnabled v1alpha1.NodeAutoscalerEnabled
	assert.Equal(t, v1alpha1.NodeAutoscalerEnabledDisabled, nodeEnabled.Default())
}

func TestNodeAutoscalerEnabled_ValidValues(t *testing.T) {
	t.Parallel()

	var nodeEnabled v1alpha1.NodeAutoscalerEnabled

	values := nodeEnabled.ValidValues()
	assert.Contains(t, values, "Enabled")
	assert.Contains(t, values, "Disabled")
	assert.Len(t, values, 2)
}

func TestNodeAutoscalerEnabled_StringAndType(t *testing.T) {
	t.Parallel()

	nodeEnabled := v1alpha1.NodeAutoscalerEnabledEnabled
	assert.Equal(t, "Enabled", nodeEnabled.String())
	assert.Equal(t, "NodeAutoscalerEnabled", nodeEnabled.Type())
}

func TestNodeAutoscalerEnabled_Set(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		expected  v1alpha1.NodeAutoscalerEnabled
		wantError bool
	}{
		{"enabled_lower", "enabled", v1alpha1.NodeAutoscalerEnabledEnabled, false},
		{"disabled_mixed", "Disabled", v1alpha1.NodeAutoscalerEnabledDisabled, false},
		{"invalid", "maybe", "", true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var nodeEnabled v1alpha1.NodeAutoscalerEnabled

			err := nodeEnabled.Set(testCase.input)
			if testCase.wantError {
				require.Error(t, err)
				require.ErrorIs(t, err, v1alpha1.ErrInvalidNodeAutoscalerEnabled)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.expected, nodeEnabled)
			}
		})
	}
}

func TestValidNodeAutoscalerEnableds(t *testing.T) {
	t.Parallel()

	values := v1alpha1.ValidNodeAutoscalerEnableds()
	assert.Contains(t, values, v1alpha1.NodeAutoscalerEnabledEnabled)
	assert.Contains(t, values, v1alpha1.NodeAutoscalerEnabledDisabled)
	assert.Len(t, values, 2)
}

// --- PodAutoscalerHorizontal ---

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
			name: "pool name starts with number is invalid",
			cluster: &v1alpha1.ClusterSpec{
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Pools: []v1alpha1.NodePool{
							{Name: "1pool", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
						},
					},
				},
			},
			wantErr: v1alpha1.ErrInvalidPoolName,
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
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: -1, Max: 5},
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
							{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 0, Max: -1},
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
			name: "capacity guard: maxNodesTotal caps pool capacity",
			cluster: &v1alpha1.ClusterSpec{
				Provider:      v1alpha1.ProviderHetzner,
				ControlPlanes: 1,
				Workers:       1,
				Autoscaler: v1alpha1.AutoscalerConfig{
					Node: v1alpha1.NodeAutoscalerConfig{
						Enabled:       v1alpha1.NodeAutoscalerEnabledEnabled,
						MaxNodesTotal: 3,
						Pools: []v1alpha1.NodePool{
							// pool.Max=10 but MaxNodesTotal=3 → effectivePoolCapacity=3
							// total = 1 + 1 + 3 = 5 ≤ serverLimit(10) → valid
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
