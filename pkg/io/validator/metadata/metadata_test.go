package metadata_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/io/validator"
	"github.com/devantler-tech/ksail/v5/pkg/io/validator/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateMetadata(t *testing.T) {
	t.Parallel()

	testValidMetadata(t)
	testMissingKind(t)
	testMissingAPIVersion(t)
	testMissingBothFields(t)
	testEmptyExpectedValues(t)
	testMismatchedKind(t)
	testMismatchedAPIVersion(t)
	testMismatchedBothFields(t)
}

// testValidMetadata tests validation with valid metadata.
func testValidMetadata(t *testing.T) {
	t.Helper()

	t.Run("valid_metadata", func(t *testing.T) {
		t.Parallel()

		result := &validator.ValidationResult{}

		metadata.ValidateMetadata(
			"Cluster",
			"kind.x-k8s.io/v1alpha4",
			"Cluster",
			"kind.x-k8s.io/v1alpha4",
			result,
		)

		require.Empty(t, result.Errors, "Expected no errors for valid metadata")
	})
}

// testMissingKind tests validation with missing kind field.
func testMissingKind(t *testing.T) {
	t.Helper()

	t.Run("missing_kind", func(t *testing.T) {
		t.Parallel()

		result := &validator.ValidationResult{}

		metadata.ValidateMetadata(
			"",
			"kind.x-k8s.io/v1alpha4",
			"Cluster",
			"kind.x-k8s.io/v1alpha4",
			result,
		)

		require.Len(t, result.Errors, 1, "Expected 1 error for missing kind")
		validateKindError(t, result.Errors, "Cluster")
	})
}

// testMissingAPIVersion tests validation with missing apiVersion field.
func testMissingAPIVersion(t *testing.T) {
	t.Helper()

	t.Run("missing_api_version", func(t *testing.T) {
		t.Parallel()

		result := &validator.ValidationResult{}

		metadata.ValidateMetadata(
			"Cluster",
			"",
			"Cluster",
			"kind.x-k8s.io/v1alpha4",
			result,
		)

		require.Len(t, result.Errors, 1, "Expected 1 error for missing apiVersion")
		validateAPIVersionError(t, result.Errors, "kind.x-k8s.io/v1alpha4")
	})
}

// testMissingBothFields tests validation with both fields missing.
func testMissingBothFields(t *testing.T) {
	t.Helper()

	t.Run("missing_both", func(t *testing.T) {
		t.Parallel()

		result := &validator.ValidationResult{}

		metadata.ValidateMetadata(
			"",
			"",
			"Cluster",
			"kind.x-k8s.io/v1alpha4",
			result,
		)

		require.Len(t, result.Errors, 2, "Expected 2 errors for missing both fields")
		validateKindError(t, result.Errors, "Cluster")
		validateAPIVersionError(t, result.Errors, "kind.x-k8s.io/v1alpha4")
	})
}

// testEmptyExpectedValues tests validation with empty expected values.
func testEmptyExpectedValues(t *testing.T) {
	t.Helper()

	t.Run("empty_expected_values", func(t *testing.T) {
		t.Parallel()

		result := &validator.ValidationResult{}

		metadata.ValidateMetadata("", "", "", "", result)

		require.Len(t, result.Errors, 2, "Expected 2 errors for empty expected values")
		validateKindError(t, result.Errors, "")
		validateAPIVersionError(t, result.Errors, "")
	})
}

// testMismatchedKind tests validation with mismatched kind field.
func testMismatchedKind(t *testing.T) {
	t.Helper()

	t.Run("mismatched_kind", func(t *testing.T) {
		t.Parallel()

		result := &validator.ValidationResult{}

		metadata.ValidateMetadata(
			"NotCluster",
			"ksail.dev/v1alpha1",
			"Cluster",
			"ksail.dev/v1alpha1",
			result,
		)

		require.Len(t, result.Errors, 1, "Expected 1 error for mismatched kind")
		kindError := findErrorByField(result.Errors, "kind")
		require.NotNil(t, kindError, "Should have kind error")
		assert.Equal(t, "kind does not match expected value", kindError.Message)
		assert.Equal(t, "NotCluster", kindError.CurrentValue)
		assert.Equal(t, "Cluster", kindError.ExpectedValue)
		assert.Equal(t, "Set kind to 'Cluster'", kindError.FixSuggestion)
	})
}

// testMismatchedAPIVersion tests validation with mismatched apiVersion field.
func testMismatchedAPIVersion(t *testing.T) {
	t.Helper()

	t.Run("mismatched_api_version", func(t *testing.T) {
		t.Parallel()

		result := &validator.ValidationResult{}

		metadata.ValidateMetadata(
			"Cluster",
			"some.other.group/v1",
			"Cluster",
			"ksail.dev/v1alpha1",
			result,
		)

		require.Len(t, result.Errors, 1, "Expected 1 error for mismatched apiVersion")
		apiVersionError := findErrorByField(result.Errors, "apiVersion")
		require.NotNil(t, apiVersionError, "Should have apiVersion error")
		assert.Equal(t, "apiVersion does not match expected value", apiVersionError.Message)
		assert.Equal(t, "some.other.group/v1", apiVersionError.CurrentValue)
		assert.Equal(t, "ksail.dev/v1alpha1", apiVersionError.ExpectedValue)
		assert.Equal(t, "Set apiVersion to 'ksail.dev/v1alpha1'", apiVersionError.FixSuggestion)
	})
}

// testMismatchedBothFields tests validation with both fields mismatched.
func testMismatchedBothFields(t *testing.T) {
	t.Helper()

	t.Run("mismatched_both", func(t *testing.T) {
		t.Parallel()

		result := &validator.ValidationResult{}

		metadata.ValidateMetadata(
			"NotCluster",
			"some.other.group/v1",
			"Cluster",
			"ksail.dev/v1alpha1",
			result,
		)

		require.Len(t, result.Errors, 2, "Expected 2 errors for mismatched both fields")

		kindError := findErrorByField(result.Errors, "kind")
		require.NotNil(t, kindError, "Should have kind error")
		assert.Equal(t, "kind does not match expected value", kindError.Message)
		assert.Equal(t, "NotCluster", kindError.CurrentValue)
		assert.Equal(t, "Cluster", kindError.ExpectedValue)

		apiVersionError := findErrorByField(result.Errors, "apiVersion")
		require.NotNil(t, apiVersionError, "Should have apiVersion error")
		assert.Equal(t, "apiVersion does not match expected value", apiVersionError.Message)
		assert.Equal(t, "some.other.group/v1", apiVersionError.CurrentValue)
		assert.Equal(t, "ksail.dev/v1alpha1", apiVersionError.ExpectedValue)
	})
}

// validateKindError validates that a kind error exists with expected content.
func validateKindError(t *testing.T, errors []validator.ValidationError, expectedKind string) {
	t.Helper()

	kindError := findErrorByField(errors, "kind")
	require.NotNil(t, kindError, "Should have kind error")
	assert.Equal(t, "kind is required", kindError.Message)
	assert.Equal(t, expectedKind, kindError.ExpectedValue)
	assert.Equal(t, "Set kind to '"+expectedKind+"'", kindError.FixSuggestion)
}

// validateAPIVersionError validates that an apiVersion error exists with expected content.
func validateAPIVersionError(
	t *testing.T,
	errors []validator.ValidationError,
	expectedAPIVersion string,
) {
	t.Helper()

	apiVersionError := findErrorByField(errors, "apiVersion")
	require.NotNil(t, apiVersionError, "Should have apiVersion error")
	assert.Equal(t, "apiVersion is required", apiVersionError.Message)
	assert.Equal(t, expectedAPIVersion, apiVersionError.ExpectedValue)
	assert.Equal(t, "Set apiVersion to '"+expectedAPIVersion+"'", apiVersionError.FixSuggestion)
}

// Helper function to find an error by field name.
func findErrorByField(errors []validator.ValidationError, field string) *validator.ValidationError {
	for i := range errors {
		if errors[i].Field == field {
			return &errors[i]
		}
	}

	return nil
}
