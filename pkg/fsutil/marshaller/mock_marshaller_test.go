package marshaller_test

import (
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/marshaller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var (
	errMockMarshallerMarshalFailed         = errors.New("marshal failed")
	errMockMarshallerUnmarshalFailed       = errors.New("unmarshal failed")
	errMockMarshallerUnmarshalStringFailed = errors.New("unmarshal string failed")
)

// TestMockMarshaller_Marshal exercises the MockMarshaller.Marshal method
// to verify the generated mock implements the Marshaller interface correctly.
func TestMockMarshaller_Marshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		model       TestModel
		returnStr   string
		returnErr   error
		expectError bool
	}{
		{
			name:        "successful marshal",
			model:       TestModel{Name: "test", Value: 42},
			returnStr:   "Name: test\nValue: 42\n",
			returnErr:   nil,
			expectError: false,
		},
		{
			name:        "marshal returns error",
			model:       TestModel{Name: "bad", Value: -1},
			returnStr:   "",
			returnErr:   errMockMarshallerMarshalFailed,
			expectError: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockM := marshaller.NewMockMarshaller[TestModel](t)

			mockM.EXPECT().
				Marshal(testCase.model).
				Return(testCase.returnStr, testCase.returnErr)

			result, err := mockM.Marshal(testCase.model)

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

// TestMockMarshaller_Unmarshal exercises the MockMarshaller.Unmarshal method.
func TestMockMarshaller_Unmarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		data        []byte
		returnErr   error
		expectError bool
	}{
		{
			name:        "successful unmarshal",
			data:        []byte("Name: test\nValue: 42\n"),
			returnErr:   nil,
			expectError: false,
		},
		{
			name:        "unmarshal error",
			data:        []byte("invalid"),
			returnErr:   errMockMarshallerUnmarshalFailed,
			expectError: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockM := marshaller.NewMockMarshaller[TestModel](t)

			var model TestModel

			mockM.EXPECT().
				Unmarshal(testCase.data, &model).
				Return(testCase.returnErr)

			err := mockM.Unmarshal(testCase.data, &model)

			if testCase.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestMockMarshaller_UnmarshalString exercises the MockMarshaller.UnmarshalString method.
func TestMockMarshaller_UnmarshalString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		data        string
		returnErr   error
		expectError bool
	}{
		{
			name:        "successful unmarshal string",
			data:        "Name: test\nValue: 42\n",
			returnErr:   nil,
			expectError: false,
		},
		{
			name:        "unmarshal string error",
			data:        "bad yaml",
			returnErr:   errMockMarshallerUnmarshalStringFailed,
			expectError: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockM := marshaller.NewMockMarshaller[TestModel](t)

			var model TestModel

			mockM.EXPECT().
				UnmarshalString(testCase.data, &model).
				Return(testCase.returnErr)

			err := mockM.UnmarshalString(testCase.data, &model)

			if testCase.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestMockMarshaller_RunAndReturn verifies the RunAndReturn fluent API on mock calls.
//
//nolint:funlen // Subtests keep each mock callback scenario readable.
func TestMockMarshaller_RunAndReturn(t *testing.T) {
	t.Parallel()

	t.Run("marshal RunAndReturn", func(t *testing.T) {
		t.Parallel()

		mockM := marshaller.NewMockMarshaller[TestModel](t)

		mockM.EXPECT().
			Marshal(mock.Anything).
			RunAndReturn(func(model TestModel) (string, error) {
				return "custom-" + model.Name, nil
			})

		result, err := mockM.Marshal(TestModel{Name: "dynamic", Value: 1})

		require.NoError(t, err)
		assert.Equal(t, "custom-dynamic", result)
	})

	t.Run("unmarshal RunAndReturn", func(t *testing.T) {
		t.Parallel()

		mockM := marshaller.NewMockMarshaller[TestModel](t)

		mockM.EXPECT().
			Unmarshal(mock.Anything, mock.Anything).
			RunAndReturn(func(_ []byte, model *TestModel) error {
				model.Name = "injected"
				model.Value = 99

				return nil
			})

		var model TestModel

		err := mockM.Unmarshal([]byte("anything"), &model)

		require.NoError(t, err)
		assert.Equal(t, "injected", model.Name)
		assert.Equal(t, 99, model.Value)
	})

	t.Run("unmarshal string RunAndReturn", func(t *testing.T) {
		t.Parallel()

		mockM := marshaller.NewMockMarshaller[TestModel](t)

		mockM.EXPECT().
			UnmarshalString(mock.Anything, mock.Anything).
			RunAndReturn(func(_ string, model *TestModel) error {
				model.Name = "from-string"

				return nil
			})

		var model TestModel

		err := mockM.UnmarshalString("anything", &model)

		require.NoError(t, err)
		assert.Equal(t, "from-string", model.Name)
	})
}

// TestMockMarshaller_Run verifies the Run fluent API on mock calls.
//
//nolint:funlen // Subtests keep each mock callback scenario readable.
func TestMockMarshaller_Run(t *testing.T) {
	t.Parallel()

	t.Run("marshal Run captures model", func(t *testing.T) {
		t.Parallel()

		mockM := marshaller.NewMockMarshaller[TestModel](t)

		var captured TestModel

		mockM.EXPECT().
			Marshal(mock.Anything).
			Run(func(model TestModel) {
				captured = model
			}).
			Return("captured", nil)

		input := TestModel{Name: "capture-me", Value: 77}
		result, err := mockM.Marshal(input)

		require.NoError(t, err)
		assert.Equal(t, "captured", result)
		assert.Equal(t, input, captured)
	})

	t.Run("unmarshal Run captures data", func(t *testing.T) {
		t.Parallel()

		mockM := marshaller.NewMockMarshaller[TestModel](t)

		var capturedData []byte

		mockM.EXPECT().
			Unmarshal(mock.Anything, mock.Anything).
			Run(func(data []byte, _ *TestModel) {
				capturedData = data
			}).
			Return(nil)

		inputData := []byte("test-data")

		var model TestModel

		err := mockM.Unmarshal(inputData, &model)

		require.NoError(t, err)
		assert.Equal(t, inputData, capturedData)
	})

	t.Run("unmarshal string Run captures string data", func(t *testing.T) {
		t.Parallel()

		mockM := marshaller.NewMockMarshaller[TestModel](t)

		var capturedStr string

		mockM.EXPECT().
			UnmarshalString(mock.Anything, mock.Anything).
			Run(func(data string, _ *TestModel) {
				capturedStr = data
			}).
			Return(nil)

		var model TestModel

		err := mockM.UnmarshalString("captured-string", &model)

		require.NoError(t, err)
		assert.Equal(t, "captured-string", capturedStr)
	})
}
