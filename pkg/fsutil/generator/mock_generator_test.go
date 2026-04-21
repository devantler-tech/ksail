package generator_test

import (
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/generator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errMockGeneratorGenerateFailed = errors.New("generate failed")

// TestMockGenerator_Generate exercises the generated MockGenerator.Generate method
// to verify the mock implements the Generator interface correctly.
func TestMockGenerator_Generate(t *testing.T) {
	t.Parallel()

	type model struct {
		Name string
	}

	type options struct {
		Force bool
	}

	tests := []struct {
		name        string
		model       model
		opts        options
		returnStr   string
		returnErr   error
		expectError bool
	}{
		{
			name:        "successful generate",
			model:       model{Name: "test"},
			opts:        options{Force: false},
			returnStr:   "generated-output",
			returnErr:   nil,
			expectError: false,
		},
		{
			name:        "generate returns error",
			model:       model{Name: "bad"},
			opts:        options{Force: true},
			returnStr:   "",
			returnErr:   errMockGeneratorGenerateFailed,
			expectError: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockGen := generator.NewMockGenerator[model, options](t)

			mockGen.EXPECT().
				Generate(testCase.model, testCase.opts).
				Return(testCase.returnStr, testCase.returnErr)

			result, err := mockGen.Generate(testCase.model, testCase.opts)

			if testCase.expectError {
				require.Error(t, err)
				assert.Empty(t, result)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.returnStr, result)
			}
		})
	}
}

// TestMockGenerator_RunAndReturn verifies the RunAndReturn fluent API on mock calls.
func TestMockGenerator_RunAndReturn(t *testing.T) {
	t.Parallel()

	type myModel struct {
		Value int
	}

	type myOpts struct {
		Prefix string
	}

	mockGen := generator.NewMockGenerator[myModel, myOpts](t)

	mockGen.EXPECT().
		Generate(mock.Anything, mock.Anything).
		RunAndReturn(func(_ myModel, opts myOpts) (string, error) {
			return opts.Prefix + "-generated", nil
		})

	result, err := mockGen.Generate(
		myModel{Value: 1},
		myOpts{Prefix: "custom"},
	)

	require.NoError(t, err)
	assert.Equal(t, "custom-generated", result)
}

// TestMockGenerator_Run verifies the Run callback captures arguments.
func TestMockGenerator_Run(t *testing.T) {
	t.Parallel()

	type genModel struct {
		ID string
	}

	type genOpts struct {
		Debug bool
	}

	mockGen := generator.NewMockGenerator[genModel, genOpts](t)

	var (
		capturedModel genModel
		capturedOpts  genOpts
	)

	mockGen.EXPECT().
		Generate(mock.Anything, mock.Anything).
		Run(func(model genModel, opts genOpts) {
			capturedModel = model
			capturedOpts = opts
		}).
		Return("result", nil)

	result, err := mockGen.Generate(
		genModel{ID: "abc123"},
		genOpts{Debug: true},
	)

	require.NoError(t, err)
	assert.Equal(t, "result", result)
	assert.Equal(t, "abc123", capturedModel.ID)
	assert.True(t, capturedOpts.Debug)
}
