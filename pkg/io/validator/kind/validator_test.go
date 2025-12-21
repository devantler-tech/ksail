package kind_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/io/validator"
	kindvalidator "github.com/devantler-tech/ksail/v5/pkg/io/validator/kind"
	"github.com/stretchr/testify/require"
	kindapi "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// TestNewValidator tests the NewValidator constructor.
func TestNewValidator(t *testing.T) {
	t.Parallel()

	runNewValidatorConstructorTest(t, func() validator.Validator[*kindapi.Cluster] {
		return kindvalidator.NewValidator()
	})
}

// TestValidate tests the main Validate method with comprehensive scenarios.
func TestValidate(t *testing.T) {
	t.Parallel()

	runValidateTest[*kindapi.Cluster](t, testKindValidatorContract)
}

// Helper function for contract testing.
func testKindValidatorContract(t *testing.T) {
	t.Helper()

	// This test MUST FAIL initially to follow TDD approach
	validatorInstance := kindvalidator.NewValidator()
	testCases := createKindTestCases()

	runValidatorTests(
		t,
		validatorInstance,
		testCases,
		assertValidationResult[*kindapi.Cluster],
	)
}

func createKindTestCases() []validatorTestCase[*kindapi.Cluster] {
	return []validatorTestCase[*kindapi.Cluster]{
		{
			Name: "valid_kind_config",
			Config: &kindapi.Cluster{
				TypeMeta: kindapi.TypeMeta{
					APIVersion: "kind.x-k8s.io/v1alpha4",
					Kind:       "Cluster",
				},
				Name: "test-cluster",
				Nodes: []kindapi.Node{
					{Role: kindapi.ControlPlaneRole},
					{Role: kindapi.WorkerRole},
				},
			},
			ExpectedValid:  true,
			ExpectedErrors: []validator.ValidationError{},
		},
		{
			Name: "valid_kind_config_no_name",
			Config: &kindapi.Cluster{
				TypeMeta: kindapi.TypeMeta{
					APIVersion: "kind.x-k8s.io/v1alpha4",
					Kind:       "Cluster",
				},
				Nodes: []kindapi.Node{
					{Role: kindapi.ControlPlaneRole},
				},
			},
			ExpectedValid:  true,
			ExpectedErrors: []validator.ValidationError{},
		},
		createNilConfigTestCase[*kindapi.Cluster](),
	}
}

type validatorTestCase[T any] struct {
	Name           string
	Config         T
	ExpectedValid  bool
	ExpectedErrors []validator.ValidationError
}

func runValidatorTests[T any](
	t *testing.T,
	validatorInstance validator.Validator[T],
	testCases []validatorTestCase[T],
	assertFunc func(*testing.T, validatorTestCase[T], *validator.ValidationResult),
) {
	t.Helper()

	require.NotNil(t, validatorInstance, "Validator constructor must return non-nil validator")

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			t.Parallel()

			result := validatorInstance.Validate(testCase.Config)
			require.NotNil(t, result, "Validation result cannot be nil")

			assertFunc(t, testCase, result)
		})
	}
}

func assertValidationResult[T any](
	t *testing.T,
	testCase validatorTestCase[T],
	result *validator.ValidationResult,
) {
	t.Helper()

	if testCase.ExpectedValid {
		require.True(
			t,
			result.Valid,
			"Expected validation to pass but it failed: %v",
			result.Errors,
		)
		require.Empty(t, result.Errors, "Expected no validation errors")

		return
	}

	require.False(t, result.Valid, "Expected validation to fail but it passed")
	require.NotEmpty(t, result.Errors, "Expected validation errors")

	if len(testCase.ExpectedErrors) > 0 {
		require.Len(
			t,
			result.Errors,
			len(testCase.ExpectedErrors),
			"Expected %d validation errors, got %d",
			len(testCase.ExpectedErrors),
			len(result.Errors),
		)

		for errorIndex, expectedError := range testCase.ExpectedErrors {
			require.Equal(
				t,
				expectedError.Field,
				result.Errors[errorIndex].Field,
				"Error %d field mismatch",
				errorIndex,
			)
			require.Contains(
				t,
				result.Errors[errorIndex].Message,
				expectedError.Message,
				"Error %d message should contain expected message",
				errorIndex,
			)
		}
	}
}

func createNilConfigTestCase[T any]() validatorTestCase[T] {
	var nilConfig T

	return validatorTestCase[T]{
		Name:          "nil_config",
		Config:        nilConfig,
		ExpectedValid: false,
		ExpectedErrors: []validator.ValidationError{
			{Field: "config", Message: "configuration is nil"},
		},
	}
}

func runNewValidatorConstructorTest[T any](
	t *testing.T,
	constructorFunc func() validator.Validator[T],
) {
	t.Helper()

	t.Run("constructor", func(t *testing.T) {
		t.Parallel()

		validatorInstance := constructorFunc()
		if validatorInstance == nil {
			t.Fatal("NewValidator should return non-nil validator")
		}
	})
}

func runValidateTest[T any](
	t *testing.T,
	contractTestFunc func(*testing.T),
	edgeTestFuncs ...func(*testing.T),
) {
	t.Helper()

	t.Run("contract_scenarios", func(t *testing.T) {
		t.Parallel()
		contractTestFunc(t)
	})

	if len(edgeTestFuncs) > 0 {
		t.Run("edge_cases", func(t *testing.T) {
			t.Parallel()

			for _, testFunc := range edgeTestFuncs {
				testFunc(t)
			}
		})
	}
}
