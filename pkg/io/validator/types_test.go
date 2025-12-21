package validator_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/io/validator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestValidationErrors creates standard validation errors for test cases to eliminate duplication.
func createTestValidationErrors() []validator.ValidationError {
	return []validator.ValidationError{
		{
			Field:         "metadata.name",
			Message:       "missing required field",
			FixSuggestion: "provide a valid name",
			Location:      validator.FileLocation{FilePath: "config.yaml", Line: 5},
		},
		{
			Field:         "spec.distribution",
			Message:       "invalid distribution",
			FixSuggestion: "use valid distribution",
			Location:      validator.FileLocation{FilePath: "config.yaml", Line: 10},
		},
	}
}

// TestValidationError_Construction tests that ValidationError can be created with all fields.
func TestValidationError_Construction(t *testing.T) {
	t.Parallel()

	testCases := createValidationErrorTestCases()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := validator.ValidationError{
				Field:         testCase.field,
				Message:       testCase.message,
				FixSuggestion: testCase.fix,
				Location:      testCase.location,
			}

			assertValidationErrorEquals(t, testCase.expected, err)
		})
	}
}

func createValidationErrorTestCases() []validationErrorTestCase {
	return []validationErrorTestCase{
		createCompleteValidationErrorCase(),
		createMinimalValidationErrorCase(),
		createEmptyLocationValidationErrorCase(),
	}
}

type validationErrorTestCase struct {
	name     string
	field    string
	message  string
	fix      string
	location validator.FileLocation
	expected validator.ValidationError
}

func createCompleteValidationErrorCase() validationErrorTestCase {
	return validationErrorTestCase{
		name:    "Complete validation error",
		field:   "metadata.name",
		message: "missing required field",
		fix:     "provide a valid name",
		location: validator.FileLocation{
			FilePath: "config.yaml",
			Line:     5,
			Column:   10,
		},
		expected: validator.ValidationError{
			Field:         "metadata.name",
			Message:       "missing required field",
			FixSuggestion: "provide a valid name",
			Location: validator.FileLocation{
				FilePath: "config.yaml",
				Line:     5,
				Column:   10,
			},
		},
	}
}

func createMinimalValidationErrorCase() validationErrorTestCase {
	return validationErrorTestCase{
		name:    "Minimal validation error",
		field:   "",
		message: "general error",
		fix:     "fix the configuration",
		location: validator.FileLocation{
			FilePath: "test.yaml",
			Line:     1,
			Column:   1,
		},
		expected: validator.ValidationError{
			Field:         "",
			Message:       "general error",
			FixSuggestion: "fix the configuration",
			Location: validator.FileLocation{
				FilePath: "test.yaml",
				Line:     1,
				Column:   1,
			},
		},
	}
}

func createEmptyLocationValidationErrorCase() validationErrorTestCase {
	return validationErrorTestCase{
		name:     "Error with empty location",
		field:    "spec.invalid",
		message:  "invalid value",
		fix:      "use valid value",
		location: validator.FileLocation{},
		expected: validator.ValidationError{
			Field:         "spec.invalid",
			Message:       "invalid value",
			FixSuggestion: "use valid value",
			Location:      validator.FileLocation{},
		},
	}
}

func assertValidationErrorEquals(t *testing.T, expected, actual validator.ValidationError) {
	t.Helper()
	assert.Equal(t, expected, actual)
	assert.Equal(t, expected.Field, actual.Field)
	assert.Equal(t, expected.Message, actual.Message)
	assert.Equal(t, expected.FixSuggestion, actual.FixSuggestion)
	assert.Equal(t, expected.Location, actual.Location)
}

// TestValidationError_Error tests the Error() method of ValidationError.
func TestValidationError_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      validator.ValidationError
		expected string
	}{
		{
			name: "Error with field",
			err: validator.ValidationError{
				Field:   "metadata.name",
				Message: "missing required field",
			},
			expected: "validation error in field 'metadata.name': missing required field",
		},
		{
			name: "Error without field",
			err: validator.ValidationError{
				Field:   "",
				Message: "general error",
			},
			expected: "validation error: general error",
		},
		{
			name: "Error with empty message",
			err: validator.ValidationError{
				Field:   "spec.distribution",
				Message: "",
			},
			expected: "validation error in field 'spec.distribution': ",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.err.Error()
			assert.Equal(t, testCase.expected, result)
		})
	}
}

// TestValidationError_ImplementsError tests that ValidationError implements the error interface.
func TestValidationError_ImplementsError(t *testing.T) {
	t.Parallel()

	err := validator.ValidationError{
		Field:         "metadata.name",
		Message:       "missing required field",
		FixSuggestion: "provide a valid name",
		Location: validator.FileLocation{
			FilePath: "config.yaml",
			Line:     5,
		},
	}

	// Should implement error interface
	var _ error = err

	// Error() method should return formatted message
	errorMsg := err.Error()
	assert.Contains(t, errorMsg, "metadata.name")
	assert.Contains(t, errorMsg, "missing required field")
}

// TestValidationError_JSONSerialization tests JSON marshaling and unmarshaling of ValidationError.
func TestValidationError_JSONSerialization(t *testing.T) {
	t.Parallel()

	original := validator.ValidationError{
		Field:         "metadata.name",
		Message:       "missing required field",
		FixSuggestion: "provide a valid name",
		Location: validator.FileLocation{
			FilePath: "config.yaml",
			Line:     5,
			Column:   10,
		},
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	require.NoError(t, err)
	assert.NotEmpty(t, jsonData)

	// Unmarshal from JSON
	var unmarshaled validator.ValidationError

	err = json.Unmarshal(jsonData, &unmarshaled)
	require.NoError(t, err)

	// Should be identical
	assert.Equal(t, original, unmarshaled)
	assert.Equal(t, original.Field, unmarshaled.Field)
	assert.Equal(t, original.Message, unmarshaled.Message)
	assert.Equal(t, original.FixSuggestion, unmarshaled.FixSuggestion)
	assert.Equal(t, original.Location.FilePath, unmarshaled.Location.FilePath)
	assert.Equal(t, original.Location.Line, unmarshaled.Location.Line)
	assert.Equal(t, original.Location.Column, unmarshaled.Location.Column)
}

// TestValidationResult_Construction tests ValidationResult creation with different states.
func TestValidationResult_Construction(t *testing.T) {
	t.Parallel()

	testCases := createValidationResultTestCases()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := validator.ValidationResult{
				Valid:  testCase.valid,
				Errors: testCase.errors,
			}

			assertValidationResultEquals(t, testCase.expected, result, testCase.errors)
		})
	}
}

func createValidationResultTestCases() []validationResultTestCase {
	return []validationResultTestCase{
		createValidResultNoErrorsCase(),
		createValidResultEmptyErrorsCase(),
		createInvalidResultWithErrorsCase(),
	}
}

type validationResultTestCase struct {
	name     string
	valid    bool
	errors   []validator.ValidationError
	expected validator.ValidationResult
}

func createValidResultNoErrorsCase() validationResultTestCase {
	return validationResultTestCase{
		name:   "Valid result with no errors",
		valid:  true,
		errors: nil,
		expected: validator.ValidationResult{
			Valid:  true,
			Errors: nil,
		},
	}
}

func createValidResultEmptyErrorsCase() validationResultTestCase {
	return validationResultTestCase{
		name:   "Valid result with empty errors slice",
		valid:  true,
		errors: []validator.ValidationError{},
		expected: validator.ValidationResult{
			Valid:  true,
			Errors: []validator.ValidationError{},
		},
	}
}

func createInvalidResultWithErrorsCase() validationResultTestCase {
	return validationResultTestCase{
		name:   "Invalid result with errors",
		valid:  false,
		errors: createTestValidationErrors(),
		expected: validator.ValidationResult{
			Valid:  false,
			Errors: createTestValidationErrors(),
		},
	}
}

func assertValidationResultEquals(
	t *testing.T,
	expected, actual validator.ValidationResult,
	originalErrors []validator.ValidationError,
) {
	t.Helper()
	assert.Equal(t, expected, actual)
	assert.Equal(t, expected.Valid, actual.Valid)
	assert.Len(t, actual.Errors, len(originalErrors))

	for i, expectedErr := range originalErrors {
		assert.Equal(t, expectedErr, actual.Errors[i])
	}
}

// Testvalidator.ValidationResult_Methods tests the methods of validator.ValidationResult.
func TestValidationResult_Methods(t *testing.T) {
	t.Parallel()

	t.Run("HasErrors", func(t *testing.T) {
		t.Parallel()

		// Result with no errors
		resultWithoutErrors := validator.ValidationResult{Valid: true, Errors: nil}
		assert.False(t, resultWithoutErrors.HasErrors())

		// Result with empty errors slice
		resultWithEmptyErrors := validator.ValidationResult{
			Valid:  true,
			Errors: []validator.ValidationError{},
		}
		assert.False(t, resultWithEmptyErrors.HasErrors())

		// Result with errors
		resultWithErrors := validator.ValidationResult{
			Valid:  false,
			Errors: []validator.ValidationError{{Message: "error"}},
		}
		assert.True(t, resultWithErrors.HasErrors())
	})

	t.Run("HasWarnings", func(t *testing.T) {
		t.Parallel()

		// Result with no warnings
		resultWithoutWarnings := validator.ValidationResult{Valid: true, Warnings: nil}
		assert.False(t, resultWithoutWarnings.HasWarnings())

		// Result with warnings
		resultWithWarnings := validator.ValidationResult{
			Valid:    true,
			Warnings: []validator.ValidationError{{Message: "warning"}},
		}
		assert.True(t, resultWithWarnings.HasWarnings())
	})

	t.Run("AddError", func(t *testing.T) {
		t.Parallel()

		result := validator.ValidationResult{Valid: true}
		err := validator.ValidationError{Field: "test", Message: "test error"}

		result.AddError(err)

		assert.False(t, result.Valid)
		assert.Len(t, result.Errors, 1)
		assert.Equal(t, err, result.Errors[0])
	})

	t.Run("AddWarning", func(t *testing.T) {
		t.Parallel()

		result := validator.ValidationResult{Valid: true}
		warning := validator.ValidationError{Field: "test", Message: "test warning"}

		result.AddWarning(warning)

		assert.True(t, result.Valid) // Should remain valid
		assert.Len(t, result.Warnings, 1)
		assert.Equal(t, warning, result.Warnings[0])
	})
}

// TestValidationResult_JSONSerialization tests JSON marshaling/unmarshaling of ValidationResult.
func TestValidationResult_JSONSerialization(t *testing.T) {
	t.Parallel()

	original := validator.ValidationResult{
		Valid: false,
		Errors: []validator.ValidationError{
			{
				Field:         "metadata.name",
				Message:       "missing required field",
				FixSuggestion: "provide a valid name",
				Location:      validator.FileLocation{FilePath: "config.yaml", Line: 5, Column: 10},
			},
			{
				Field:         "spec.distribution",
				Message:       "invalid distribution",
				FixSuggestion: "use valid distribution",
				Location: validator.FileLocation{
					FilePath: "config.yaml",
					Line:     10,
					Column:   15,
				},
			},
		},
		Warnings: []validator.ValidationError{
			{
				Field:         "metadata.labels",
				Message:       "recommended field missing",
				FixSuggestion: "consider adding labels",
				Location:      validator.FileLocation{FilePath: "config.yaml", Line: 3},
			},
		},
		ConfigFile: "config.yaml",
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	require.NoError(t, err)
	assert.NotEmpty(t, jsonData)

	// Unmarshal from JSON
	var unmarshaled validator.ValidationResult

	err = json.Unmarshal(jsonData, &unmarshaled)
	require.NoError(t, err)

	// Should be identical
	assert.Equal(t, original.Valid, unmarshaled.Valid)
	assert.Equal(t, original.ConfigFile, unmarshaled.ConfigFile)
	assert.Len(t, unmarshaled.Errors, len(original.Errors))
	assert.Len(t, unmarshaled.Warnings, len(original.Warnings))

	for i, originalErr := range original.Errors {
		unmarshaledErr := unmarshaled.Errors[i]
		assert.Equal(t, originalErr.Field, unmarshaledErr.Field)
		assert.Equal(t, originalErr.Message, unmarshaledErr.Message)
		assert.Equal(t, originalErr.FixSuggestion, unmarshaledErr.FixSuggestion)
		assert.Equal(t, originalErr.Location.FilePath, unmarshaledErr.Location.FilePath)
		assert.Equal(t, originalErr.Location.Line, unmarshaledErr.Location.Line)
		assert.Equal(t, originalErr.Location.Column, unmarshaledErr.Location.Column)
	}
}

// TestFileLocation_Construction tests that FileLocation can be created with different values.
func TestFileLocation_Construction(t *testing.T) {
	t.Parallel()

	testCases := createFileLocationTestCases()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			location := validator.FileLocation{
				FilePath: testCase.filePath,
				Line:     testCase.line,
				Column:   testCase.column,
			}

			assertFileLocationEquals(t, testCase.expected, location)
		})
	}
}

func createFileLocationTestCases() []struct {
	name     string
	filePath string
	line     int
	column   int
	expected validator.FileLocation
} {
	return []struct {
		name     string
		filePath string
		line     int
		column   int
		expected validator.FileLocation
	}{
		{
			name:     "Complete file location",
			filePath: "/path/to/config.yaml",
			line:     42,
			column:   15,
			expected: validator.FileLocation{
				FilePath: "/path/to/config.yaml",
				Line:     42,
				Column:   15,
			},
		},
		{
			name:     "Minimal file location",
			filePath: "config.yaml",
			line:     1,
			column:   1,
			expected: validator.FileLocation{
				FilePath: "config.yaml",
				Line:     1,
				Column:   1,
			},
		},
		{
			name:     "Relative path",
			filePath: "./configs/cluster.yaml",
			line:     10,
			column:   5,
			expected: validator.FileLocation{
				FilePath: "./configs/cluster.yaml",
				Line:     10,
				Column:   5,
			},
		},
		{
			name:     "Zero column",
			filePath: "test.yaml",
			line:     5,
			column:   0,
			expected: validator.FileLocation{
				FilePath: "test.yaml",
				Line:     5,
				Column:   0,
			},
		},
	}
}

func assertFileLocationEquals(t *testing.T, expected, actual validator.FileLocation) {
	t.Helper()
	assert.Equal(t, expected, actual)
	assert.Equal(t, expected.FilePath, actual.FilePath)
	assert.Equal(t, expected.Line, actual.Line)
	assert.Equal(t, expected.Column, actual.Column)
}

// TestFileLocation_String tests the string representation of FileLocation.
func TestFileLocation_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		location validator.FileLocation
		expected string
	}{
		{
			name: "Complete location",
			location: validator.FileLocation{
				FilePath: "/path/to/config.yaml",
				Line:     42,
				Column:   15,
			},
			expected: "/path/to/config.yaml:42:15",
		},
		{
			name: "Location without column",
			location: validator.FileLocation{
				FilePath: "config.yaml",
				Line:     5,
				Column:   0,
			},
			expected: "config.yaml:5",
		},
		{
			name: "Location without line or column",
			location: validator.FileLocation{
				FilePath: "test.yaml",
				Line:     0,
				Column:   0,
			},
			expected: "test.yaml",
		},
		{
			name: "Large line numbers",
			location: validator.FileLocation{
				FilePath: "large.yaml",
				Line:     999999,
				Column:   123,
			},
			expected: "large.yaml:999999:123",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.location.String()
			assert.Equal(t, testCase.expected, result)
		})
	}
}

// TestFileLocation_JSONSerialization tests JSON marshaling and unmarshaling of FileLocation.
func TestFileLocation_JSONSerialization(t *testing.T) {
	t.Parallel()

	original := validator.FileLocation{
		FilePath: "/path/to/config.yaml",
		Line:     42,
		Column:   15,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	require.NoError(t, err)
	assert.NotEmpty(t, jsonData)

	// Unmarshal from JSON
	var unmarshaled validator.FileLocation

	err = json.Unmarshal(jsonData, &unmarshaled)
	require.NoError(t, err)

	// Should be identical
	assert.Equal(t, original, unmarshaled)
	assert.Equal(t, original.FilePath, unmarshaled.FilePath)
	assert.Equal(t, original.Line, unmarshaled.Line)
	assert.Equal(t, original.Column, unmarshaled.Column)
}

// TestTypes_EdgeCases tests edge cases and boundary conditions.
func TestTypes_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("Empty string fields", testEmptyStringFields)
	t.Run("Very long field names and messages", testVeryLongFields)
	t.Run("Negative line and column numbers", testNegativeLineColumn)
	t.Run("Zero values", testZeroValues)
	t.Run("Large slices", testLargeSlices)
}

func testEmptyStringFields(t *testing.T) {
	t.Parallel()

	err := validator.ValidationError{
		Field:         "",
		Message:       "",
		FixSuggestion: "",
		Location: validator.FileLocation{
			FilePath: "",
			Line:     0,
			Column:   0,
		},
	}

	// Should handle empty strings gracefully
	assert.NotPanics(t, func() {
		errMsg := err.Error()
		assert.NotEmpty(t, errMsg) // Should still produce some output
	})

	// Location String should handle empty path
	assert.NotPanics(t, func() {
		locationStr := err.Location.String()
		assert.Empty(t, locationStr) // Empty path, no line/column
	})
}

func testVeryLongFields(t *testing.T) {
	t.Parallel()

	longString := make([]byte, 10000)
	for i := range longString {
		longString[i] = byte('a' + (i % 26))
	}

	longStr := string(longString)

	err := validator.ValidationError{
		Field:         longStr,
		Message:       longStr,
		FixSuggestion: longStr,
		Location: validator.FileLocation{
			FilePath: longStr,
			Line:     999999999,
			Column:   999999999,
		},
	}

	// Should handle very long strings without panic
	assert.NotPanics(t, func() {
		_ = err.Error()
		_, jsonErr := json.Marshal(err)
		assert.NoError(t, jsonErr)
	})
}

func testNegativeLineColumn(t *testing.T) {
	t.Parallel()

	location := validator.FileLocation{
		FilePath: "test.yaml",
		Line:     -1,
		Column:   -5,
	}

	// Should handle negative numbers gracefully
	assert.NotPanics(t, func() {
		str := location.String()
		// With negative line, should just return file path
		assert.Equal(t, "test.yaml", str)
	})
}

func testZeroValues(t *testing.T) {
	t.Parallel()

	// Zero value validator.ValidationError
	var zeroErr validator.ValidationError

	assert.NotPanics(t, func() {
		_ = zeroErr.Error()
	})

	// Zero value validator.ValidationResult
	var zeroResult validator.ValidationResult
	assert.False(t, zeroResult.HasErrors())
	assert.False(t, zeroResult.HasWarnings())

	// Zero value validator.FileLocation
	var zeroLocation validator.FileLocation

	assert.NotPanics(t, func() {
		str := zeroLocation.String()
		assert.Empty(t, str)
	})
}

func testLargeSlices(t *testing.T) {
	t.Parallel()

	// Create result with many errors
	result := validator.ValidationResult{Valid: false}
	for i := range 1000 {
		result.AddError(validator.ValidationError{
			Field:   fmt.Sprintf("field%d", i),
			Message: fmt.Sprintf("error %d", i),
		})
	}

	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1000)
	assert.True(t, result.HasErrors())

	// Should handle JSON serialization of large slices
	assert.NotPanics(t, func() {
		_, err := json.Marshal(result)
		assert.NoError(t, err)
	})
}

// TestValidationErrorWithCompleteFields tests ValidationError with all possible fields populated.
func TestValidationErrorWithCompleteFields(t *testing.T) {
	t.Parallel()

	location := validator.FileLocation{FilePath: "complex.yaml", Line: 25, Column: 12}

	err := validator.ValidationError{
		Field:         "metadata.labels.app",
		Message:       "label value contains invalid characters",
		CurrentValue:  "my-app@v1",
		ExpectedValue: "my-app-v1",
		FixSuggestion: "Replace '@' with '-' in label values",
		Location:      location,
	}

	// Test Error() method with field
	errorStr := err.Error()
	assert.Contains(t, errorStr, "validation error in field 'metadata.labels.app'")
	assert.Contains(t, errorStr, "label value contains invalid characters")

	// Test all fields are preserved
	assert.Equal(t, "metadata.labels.app", err.Field)
	assert.Equal(t, "label value contains invalid characters", err.Message)
	assert.Equal(t, "my-app@v1", err.CurrentValue)
	assert.Equal(t, "my-app-v1", err.ExpectedValue)
	assert.Equal(t, "Replace '@' with '-' in label values", err.FixSuggestion)
	assert.Equal(t, location, err.Location)
}

// TestValidationErrorWithoutField tests ValidationError Error() method when Field is empty.
func TestValidationErrorWithoutField(t *testing.T) {
	t.Parallel()

	err := validator.ValidationError{
		Field:   "", // No field specified
		Message: "general configuration error",
	}

	errorStr := err.Error()
	assert.Equal(t, "validation error: general configuration error", errorStr)
}
