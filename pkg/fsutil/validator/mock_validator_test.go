package validator_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/validator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestMockValidator_Validate exercises the generated MockValidator.Validate method
// to verify the mock implements the Validator interface correctly.
func TestMockValidator_Validate(t *testing.T) {
	t.Parallel()

	type config struct {
		Name  string
		Valid bool
	}

	tests := []struct {
		name        string
		config      config
		returnValid bool
		errorCount  int
	}{
		{
			name:        "valid config returns valid result",
			config:      config{Name: "good", Valid: true},
			returnValid: true,
			errorCount:  0,
		},
		{
			name:        "invalid config returns errors",
			config:      config{Name: "", Valid: false},
			returnValid: false,
			errorCount:  2,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockVal := validator.NewMockValidator[config](t)

			expectedResult := validator.NewValidationResult("test.yaml")

			if !testCase.returnValid {
				for range testCase.errorCount {
					expectedResult.AddError(validator.ValidationError{
						Field:   "name",
						Message: "name is required",
					})
				}
			}

			mockVal.EXPECT().
				Validate(testCase.config).
				Return(expectedResult)

			result := mockVal.Validate(testCase.config)

			require.NotNil(t, result)
			assert.Equal(t, testCase.returnValid, result.Valid)
			assert.Len(t, result.Errors, testCase.errorCount)
		})
	}
}

// TestMockValidator_RunAndReturn verifies the RunAndReturn fluent API.
func TestMockValidator_RunAndReturn(t *testing.T) {
	t.Parallel()

	type appConfig struct {
		Port int
	}

	mockVal := validator.NewMockValidator[appConfig](t)

	mockVal.EXPECT().
		Validate(mock.Anything).
		RunAndReturn(func(cfg appConfig) *validator.ValidationResult {
			result := validator.NewValidationResult("dynamic.yaml")
			if cfg.Port <= 0 {
				result.AddError(validator.ValidationError{
					Field:         "port",
					Message:       "port must be positive",
					CurrentValue:  cfg.Port,
					ExpectedValue: "> 0",
				})
			}

			return result
		})

	// Valid config
	validResult := mockVal.Validate(appConfig{Port: 8080})
	require.NotNil(t, validResult)
	assert.True(t, validResult.Valid)
	assert.Empty(t, validResult.Errors)
}

// TestMockValidator_Run verifies the Run callback captures arguments.
func TestMockValidator_Run(t *testing.T) {
	t.Parallel()

	type myConfig struct {
		Name string
	}

	mockVal := validator.NewMockValidator[myConfig](t)

	var capturedConfig myConfig

	expectedResult := validator.NewValidationResult("captured.yaml")

	mockVal.EXPECT().
		Validate(mock.Anything).
		Run(func(cfg myConfig) {
			capturedConfig = cfg
		}).
		Return(expectedResult)

	input := myConfig{Name: "capture-me"}
	result := mockVal.Validate(input)

	require.NotNil(t, result)
	assert.True(t, result.Valid)
	assert.Equal(t, "capture-me", capturedConfig.Name)
}

// TestMockValidator_NilReturn verifies mock handles nil return gracefully.
func TestMockValidator_NilReturn(t *testing.T) {
	t.Parallel()

	type simpleConfig struct{}

	mockVal := validator.NewMockValidator[simpleConfig](t)

	mockVal.EXPECT().
		Validate(mock.Anything).
		Return(nil)

	result := mockVal.Validate(simpleConfig{})

	assert.Nil(t, result, "should return nil when mock returns nil")
}

// TestValidationResult_MultipleErrors verifies adding multiple errors.
func TestValidationResult_MultipleErrors(t *testing.T) {
	t.Parallel()

	result := validator.NewValidationResult("multi-error.yaml")

	errors := []validator.ValidationError{
		{Field: "field1", Message: "error 1"},
		{Field: "field2", Message: "error 2"},
		{Field: "field3", Message: "error 3"},
	}

	for _, validationErr := range errors {
		result.AddError(validationErr)
	}

	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 3)
	assert.True(t, result.HasErrors())
	assert.False(t, result.HasWarnings())
}

// TestValidationResult_MultipleWarnings verifies adding multiple warnings preserves validity.
func TestValidationResult_MultipleWarnings(t *testing.T) {
	t.Parallel()

	result := validator.NewValidationResult("multi-warning.yaml")

	warnings := []validator.ValidationError{
		{Field: "deprecated1", Message: "deprecated field 1"},
		{Field: "deprecated2", Message: "deprecated field 2"},
	}

	for _, warning := range warnings {
		result.AddWarning(warning)
	}

	assert.True(t, result.Valid, "warnings should not affect validity")
	assert.Empty(t, result.Errors)
	assert.Len(t, result.Warnings, 2)
	assert.True(t, result.HasWarnings())
	assert.False(t, result.HasErrors())
}

// TestValidationError_WithLocation verifies error messages with location info.
func TestValidationError_WithLocation(t *testing.T) {
	t.Parallel()

	err := validator.ValidationError{
		Field:   "spec.replicas",
		Message: "must be >= 1",
		Location: validator.FileLocation{
			FilePath: "/path/to/config.yaml",
			Line:     15,
			Column:   3,
		},
	}

	assert.Contains(t, err.Error(), "spec.replicas")
	assert.Contains(t, err.Error(), "must be >= 1")
	assert.Equal(t, "/path/to/config.yaml:15:3", err.Location.String())
}

// TestValidationError_AllFields verifies all fields are preserved.
func TestValidationError_AllFields(t *testing.T) {
	t.Parallel()

	err := validator.ValidationError{
		Field:         "spec.image",
		Message:       "invalid image reference",
		CurrentValue:  "bad:image:tag",
		ExpectedValue: "<registry>/<name>:<tag>",
		FixSuggestion: "Use format registry/name:tag",
		Location: validator.FileLocation{
			FilePath: "/configs/deploy.yaml",
			Line:     42,
		},
	}

	assert.Equal(t, "spec.image", err.Field)
	assert.Equal(t, "invalid image reference", err.Message)
	assert.Equal(t, "bad:image:tag", err.CurrentValue)
	assert.Equal(t, "<registry>/<name>:<tag>", err.ExpectedValue)
	assert.Equal(t, "Use format registry/name:tag", err.FixSuggestion)
	assert.Equal(t, "/configs/deploy.yaml:42", err.Location.String())
}

// TestFileLocation_EmptyPath verifies FileLocation.String with empty path.
func TestFileLocation_EmptyPath(t *testing.T) {
	t.Parallel()

	location := validator.FileLocation{}
	assert.Empty(t, location.String())
}
