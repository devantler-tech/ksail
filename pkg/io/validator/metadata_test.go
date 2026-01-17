package validator_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/io/validator"
	"github.com/stretchr/testify/assert"
)

type validateMetadataTestCase struct {
	name                string
	kind                string
	apiVersion          string
	expectedKind        string
	expectedAPIVersion  string
	expectedValid       bool
	expectedErrorCount  int
	expectedErrorFields []string
}

func getValidMetadataTestCases() []validateMetadataTestCase {
	return []validateMetadataTestCase{
		{
			name:               "valid metadata",
			kind:               "Cluster",
			apiVersion:         "ksail.io/v1alpha1",
			expectedKind:       "Cluster",
			expectedAPIVersion: "ksail.io/v1alpha1",
			expectedValid:      true,
			expectedErrorCount: 0,
		},
	}
}

func getMissingFieldsTestCases() []validateMetadataTestCase {
	return []validateMetadataTestCase{
		{
			name:                "missing kind",
			kind:                "",
			apiVersion:          "ksail.io/v1alpha1",
			expectedKind:        "Cluster",
			expectedAPIVersion:  "ksail.io/v1alpha1",
			expectedValid:       false,
			expectedErrorCount:  1,
			expectedErrorFields: []string{"kind"},
		},
		{
			name:                "missing apiVersion",
			kind:                "Cluster",
			apiVersion:          "",
			expectedKind:        "Cluster",
			expectedAPIVersion:  "ksail.io/v1alpha1",
			expectedValid:       false,
			expectedErrorCount:  1,
			expectedErrorFields: []string{"apiVersion"},
		},
		{
			name:                "missing both kind and apiVersion",
			kind:                "",
			apiVersion:          "",
			expectedKind:        "Cluster",
			expectedAPIVersion:  "ksail.io/v1alpha1",
			expectedValid:       false,
			expectedErrorCount:  2,
			expectedErrorFields: []string{"kind", "apiVersion"},
		},
	}
}

func getWrongValueTestCases() []validateMetadataTestCase {
	return []validateMetadataTestCase{
		{
			name:                "wrong kind",
			kind:                "WrongKind",
			apiVersion:          "ksail.io/v1alpha1",
			expectedKind:        "Cluster",
			expectedAPIVersion:  "ksail.io/v1alpha1",
			expectedValid:       false,
			expectedErrorCount:  1,
			expectedErrorFields: []string{"kind"},
		},
		{
			name:                "wrong apiVersion",
			kind:                "Cluster",
			apiVersion:          "ksail.io/v1beta1",
			expectedKind:        "Cluster",
			expectedAPIVersion:  "ksail.io/v1alpha1",
			expectedValid:       false,
			expectedErrorCount:  1,
			expectedErrorFields: []string{"apiVersion"},
		},
		{
			name:                "both kind and apiVersion wrong",
			kind:                "Pod",
			apiVersion:          "v1",
			expectedKind:        "Cluster",
			expectedAPIVersion:  "ksail.io/v1alpha1",
			expectedValid:       false,
			expectedErrorCount:  2,
			expectedErrorFields: []string{"kind", "apiVersion"},
		},
	}
}

func getCaseSensitivityTestCases() []validateMetadataTestCase {
	return []validateMetadataTestCase{
		{
			name:               "case sensitive kind match",
			kind:               "cluster",
			apiVersion:         "ksail.io/v1alpha1",
			expectedKind:       "Cluster",
			expectedAPIVersion: "ksail.io/v1alpha1",
			expectedValid:      false,
			expectedErrorCount: 1,
		},
		{
			name:               "case sensitive apiVersion match",
			kind:               "Cluster",
			apiVersion:         "KSAIL.IO/V1ALPHA1",
			expectedKind:       "Cluster",
			expectedAPIVersion: "ksail.io/v1alpha1",
			expectedValid:      false,
			expectedErrorCount: 1,
		},
	}
}

func getValidateMetadataTestCases() []validateMetadataTestCase {
	testCases := getValidMetadataTestCases()
	testCases = append(testCases, getMissingFieldsTestCases()...)
	testCases = append(testCases, getWrongValueTestCases()...)
	testCases = append(testCases, getCaseSensitivityTestCases()...)

	return testCases
}

// TestValidateMetadata tests the ValidateMetadata function for various scenarios.
func TestValidateMetadata(t *testing.T) {
	t.Parallel()

	tests := getValidateMetadataTestCases()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := validator.NewValidationResult("test-config.yaml")

			validator.ValidateMetadata(
				testCase.kind,
				testCase.apiVersion,
				testCase.expectedKind,
				testCase.expectedAPIVersion,
				result,
			)

			assert.Equal(t, testCase.expectedValid, result.Valid)
			assert.Len(t, result.Errors, testCase.expectedErrorCount)

			// Verify expected error fields
			errorFields := make([]string, 0, len(result.Errors))
			for _, err := range result.Errors {
				errorFields = append(errorFields, err.Field)
			}

			for _, expectedField := range testCase.expectedErrorFields {
				assert.Contains(t, errorFields, expectedField)
			}
		})
	}
}

// TestValidateMetadata_MissingKindErrorMessage tests the error message for missing kind.
func TestValidateMetadata_MissingKindErrorMessage(t *testing.T) {
	t.Parallel()

	result := validator.NewValidationResult("test.yaml")
	validator.ValidateMetadata("", "v1alpha1", "Cluster", "v1alpha1", result)

	assert.Len(t, result.Errors, 1)
	err := result.Errors[0]
	assert.Equal(t, "kind", err.Field)
	assert.Equal(t, "kind is required", err.Message)
	assert.Equal(t, "Cluster", err.ExpectedValue)
	assert.Contains(t, err.FixSuggestion, "Cluster")
}

// TestValidateMetadata_WrongKindErrorMessage tests the error message for wrong kind.
func TestValidateMetadata_WrongKindErrorMessage(t *testing.T) {
	t.Parallel()

	result := validator.NewValidationResult("test.yaml")
	validator.ValidateMetadata("WrongKind", "v1alpha1", "Cluster", "v1alpha1", result)

	assert.Len(t, result.Errors, 1)
	err := result.Errors[0]
	assert.Equal(t, "kind", err.Field)
	assert.Equal(t, "kind does not match expected value", err.Message)
	assert.Equal(t, "WrongKind", err.CurrentValue)
	assert.Equal(t, "Cluster", err.ExpectedValue)
	assert.Contains(t, err.FixSuggestion, "Cluster")
}

// TestValidateMetadata_MissingAPIVersionErrorMessage tests the error message for missing apiVersion.
func TestValidateMetadata_MissingAPIVersionErrorMessage(t *testing.T) {
	t.Parallel()

	result := validator.NewValidationResult("test.yaml")
	validator.ValidateMetadata("Cluster", "", "Cluster", "ksail.io/v1alpha1", result)

	assert.Len(t, result.Errors, 1)
	err := result.Errors[0]
	assert.Equal(t, "apiVersion", err.Field)
	assert.Equal(t, "apiVersion is required", err.Message)
	assert.Equal(t, "ksail.io/v1alpha1", err.ExpectedValue)
	assert.Contains(t, err.FixSuggestion, "ksail.io/v1alpha1")
}

// TestValidateMetadata_WrongAPIVersionErrorMessage tests the error message for wrong apiVersion.
func TestValidateMetadata_WrongAPIVersionErrorMessage(t *testing.T) {
	t.Parallel()

	result := validator.NewValidationResult("test.yaml")
	validator.ValidateMetadata("Cluster", "v1beta1", "Cluster", "ksail.io/v1alpha1", result)

	assert.Len(t, result.Errors, 1)
	err := result.Errors[0]
	assert.Equal(t, "apiVersion", err.Field)
	assert.Equal(t, "apiVersion does not match expected value", err.Message)
	assert.Equal(t, "v1beta1", err.CurrentValue)
	assert.Equal(t, "ksail.io/v1alpha1", err.ExpectedValue)
	assert.Contains(t, err.FixSuggestion, "ksail.io/v1alpha1")
}

// TestValidateMetadata_PreservesExistingErrors tests that ValidateMetadata preserves
// existing errors in the result.
func TestValidateMetadata_PreservesExistingErrors(t *testing.T) {
	t.Parallel()

	result := validator.NewValidationResult("test.yaml")
	// Add an existing error
	result.AddError(validator.ValidationError{
		Field:   "existing",
		Message: "existing error",
	})

	// Now validate metadata with errors
	validator.ValidateMetadata("", "", "Cluster", "v1alpha1", result)

	// Should have 3 errors total: 1 existing + 2 from metadata validation
	assert.Len(t, result.Errors, 3)
	assert.Equal(t, "existing", result.Errors[0].Field)
}
