package yamlmarshaller_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/io/marshaller"
	yamlmarshaller "github.com/devantler-tech/ksail/v5/pkg/io/marshaller/yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sample model used for tests.
type sample struct {
	Name   string   `json:"name"           yaml:"name"`
	Count  int      `json:"count"          yaml:"count"`
	Active bool     `json:"active"         yaml:"active"`
	Tags   []string `json:"tags,omitempty" yaml:"tags,omitempty"`
}

// bad is a type that cannot be marshaled/unmarshaled due to the func field.
type bad struct {
	F func()
}

func TestMarshalSuccess(t *testing.T) {
	t.Parallel()

	mar := yamlmarshaller.NewMarshaller[sample]()
	want := sample{
		Name:   "app",
		Count:  3,
		Active: true,
		Tags:   []string{"dev", "test"},
	}

	out, err := mar.Marshal(want)

	require.NoError(t, err)
	assert.NotEmpty(t, out)

	// Round-trip to ensure content encodes the same data
	var got sample

	mustUnmarshalString[sample](t, mar, out, &got)
	assert.Equal(t, want, got)
}

func TestMarshalStringSuccess(t *testing.T) {
	t.Parallel()

	mar := yamlmarshaller.NewMarshaller[sample]()
	input := sample{
		Name:   "app",
		Count:  3,
		Active: true,
		Tags:   []string{"dev", "test"},
	}

	out := mustMarshal(t, mar, input)
	// Some yaml libs may preserve struct field names; accept either lowercase (from tags) or field name casing.
	assertStringContainsOneOf(t, out, "name: app", "Name: app")
	assertStringContainsOneOf(t, out, "count: 3", "Count: 3")
	assertStringContainsOneOf(t, out, "active: true", "Active: true")
	assertStringContains(t, out, "- dev", "- test")
}

func TestUnmarshalSuccess(t *testing.T) {
	t.Parallel()

	mar := yamlmarshaller.NewMarshaller[sample]()
	data := []byte("" +
		"name: app\n" +
		"count: 3\n" +
		"active: true\n" +
		"tags:\n" +
		"- dev\n" +
		"- test\n",
	)
	want := sample{
		Name:   "app",
		Count:  3,
		Active: true,
		Tags:   []string{"dev", "test"},
	}

	var got sample

	mustUnmarshal[sample](t, mar, data, &got)
	assert.Equal(t, want, got)
}

func TestUnmarshalStringSuccess(t *testing.T) {
	t.Parallel()

	mar := yamlmarshaller.NewMarshaller[sample]()
	data := "" +
		"name: app\n" +
		"count: 3\n" +
		"active: true\n" +
		"tags:\n" +
		"- dev\n" +
		"- test\n"
	want := sample{
		Name:   "app",
		Count:  3,
		Active: true,
		Tags:   []string{"dev", "test"},
	}

	var got sample

	mustUnmarshalString[sample](t, mar, data, &got)
	assert.Equal(t, want, got)
}

func TestMarshalErrorUnsupportedType(t *testing.T) {
	t.Parallel()

	// Arrange: a type that cannot be marshaled (contains a func field)
	type bad struct {
		F func()
	}

	mar := yamlmarshaller.NewMarshaller[bad]()
	input := bad{F: func() {}}

	yamlText, err := mar.Marshal(input)

	require.Error(t, err)
	assert.Empty(t, yamlText)
	assert.ErrorContains(t, err, "failed to marshal YAML")
}

func TestUnmarshalErrorUnsupportedType(t *testing.T) {
	t.Parallel()

	assertBadUnmarshalError(t, func(mar marshaller.Marshaller[bad], model *bad) error {
		return mar.Unmarshal([]byte("F: !!js/function 'function() {}'"), model)
	})
}

func TestUnmarshalStringErrorUnsupportedType(t *testing.T) {
	t.Parallel()

	assertBadUnmarshalError(t, func(mar marshaller.Marshaller[bad], model *bad) error {
		return mar.UnmarshalString("F: !!js/function 'function() {}'", model)
	})
}

// assertBadUnmarshalError runs the provided unmarshal op and asserts the wrapped error.
func assertBadUnmarshalError(
	t *testing.T,
	operation func(mar marshaller.Marshaller[bad], model *bad) error,
) {
	t.Helper()

	mar := yamlmarshaller.NewMarshaller[bad]()
	model := bad{F: func() {}}

	err := operation(mar, &model)

	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to unmarshal YAML")
}

func mustMarshal[T any](t *testing.T, m marshaller.Marshaller[T], v T) string {
	t.Helper()

	s, err := m.Marshal(v)
	require.NoError(t, err)

	return s
}

func mustUnmarshal[T any](t *testing.T, m marshaller.Marshaller[T], data []byte, out *T) {
	t.Helper()

	err := m.Unmarshal(data, out)
	require.NoError(t, err)
}

func mustUnmarshalString[T any](t *testing.T, m marshaller.Marshaller[T], data string, out *T) {
	t.Helper()

	err := m.UnmarshalString(data, out)
	require.NoError(t, err)
}

func assertStringContains(t *testing.T, s string, substrings ...string) {
	t.Helper()

	for _, sub := range substrings {
		assert.Contains(t, s, sub)
	}
}

func assertStringContainsOneOf(t *testing.T, s string, substrings ...string) {
	t.Helper()

	for _, sub := range substrings {
		if strings.Contains(s, sub) {
			return
		}
	}

	t.Fatalf("expected string to contain one of %v", substrings)
}
