package configmanager_test

import (
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errMockConfigManagerLoadFailed = errors.New("load failed")

// TestMockConfigManager_Load exercises the generated MockConfigManager.Load method
// to verify the mock implements the ConfigManager interface correctly.
//
//nolint:funlen,modernize // Table-driven pointer fixtures are clearer with the helper.
func TestMockConfigManager_Load(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		opts        configmanager.LoadOptions
		returnVal   *string
		returnErr   error
		expectError bool
	}{
		{
			name:        "successful load",
			opts:        configmanager.LoadOptions{},
			returnVal:   ptrTo("loaded-config"),
			returnErr:   nil,
			expectError: false,
		},
		{
			name:        "load with silent option",
			opts:        configmanager.LoadOptions{Silent: true},
			returnVal:   ptrTo("silent-config"),
			returnErr:   nil,
			expectError: false,
		},
		{
			name:        "load returns error",
			opts:        configmanager.LoadOptions{},
			returnVal:   nil,
			returnErr:   errMockConfigManagerLoadFailed,
			expectError: true,
		},
		{
			name: "load with all options",
			opts: configmanager.LoadOptions{
				Silent:           true,
				IgnoreConfigFile: true,
				SkipValidation:   true,
			},
			returnVal:   ptrTo("all-opts"),
			returnErr:   nil,
			expectError: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockCM := configmanager.NewMockConfigManager[string](t)

			mockCM.EXPECT().
				Load(testCase.opts).
				Return(testCase.returnVal, testCase.returnErr)

			result, err := mockCM.Load(testCase.opts)

			if testCase.expectError {
				require.Error(t, err)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, *testCase.returnVal, *result)
			}
		})
	}
}

// TestMockConfigManager_RunAndReturn verifies the RunAndReturn fluent API.
func TestMockConfigManager_RunAndReturn(t *testing.T) {
	t.Parallel()

	mockCM := configmanager.NewMockConfigManager[string](t)

	mockCM.EXPECT().
		Load(mock.Anything).
		RunAndReturn(func(opts configmanager.LoadOptions) (*string, error) {
			if opts.Silent {
				val := "silent-result"

				return &val, nil
			}

			val := "verbose-result"

			return &val, nil
		}).
		Twice()

	// Test with silent=true
	silentResult, err := mockCM.Load(configmanager.LoadOptions{Silent: true})

	require.NoError(t, err)
	require.NotNil(t, silentResult)
	assert.Equal(t, "silent-result", *silentResult)

	verboseResult, err := mockCM.Load(configmanager.LoadOptions{})

	require.NoError(t, err)
	require.NotNil(t, verboseResult)
	assert.Equal(t, "verbose-result", *verboseResult)
}

// TestMockConfigManager_Run verifies the Run callback is invoked.
func TestMockConfigManager_Run(t *testing.T) {
	t.Parallel()

	mockCM := configmanager.NewMockConfigManager[string](t)

	var capturedOpts configmanager.LoadOptions

	expectedResult := "captured"

	mockCM.EXPECT().
		Load(mock.Anything).
		Run(func(opts configmanager.LoadOptions) {
			capturedOpts = opts
		}).
		Return(&expectedResult, nil)

	inputOpts := configmanager.LoadOptions{
		Silent:           true,
		IgnoreConfigFile: true,
	}

	result, err := mockCM.Load(inputOpts)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "captured", *result)
	assert.True(t, capturedOpts.Silent)
	assert.True(t, capturedOpts.IgnoreConfigFile)
}

// TestLoadOptions_Defaults verifies the zero value of LoadOptions represents the standard defaults.
func TestLoadOptions_Defaults(t *testing.T) {
	t.Parallel()

	opts := configmanager.LoadOptions{}

	assert.False(t, opts.Silent)
	assert.False(t, opts.IgnoreConfigFile)
	assert.False(t, opts.SkipValidation)
	assert.Nil(t, opts.Timer)
}

//nolint:modernize // Generic helper keeps the table definitions concise.
func ptrTo[T any](v T) *T {
	return &v
}
