package io_test

import (
	"testing"

	io "github.com/devantler-tech/ksail-go/pkg/io"
	"github.com/stretchr/testify/assert"
)

func TestTrimNonEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		input         string
		expectedStr   string
		expectedValid bool
	}{
		{"empty string returns false", "", "", false},
		{"whitespace only returns false", "   ", "", false},
		{"tabs and spaces returns false", "\t  \n  ", "", false},
		{"valid string returns true and trimmed value", "docker.io", "docker.io", true},
		{"string with leading whitespace is trimmed", "  ghcr.io", "ghcr.io", true},
		{"string with trailing whitespace is trimmed", "registry.local  ", "registry.local", true},
		{
			"string with both leading and trailing whitespace",
			"  localhost:5000  ",
			"localhost:5000",
			true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			str, valid := io.TrimNonEmpty(test.input)

			assert.Equal(t, test.expectedStr, str, "trimmed string should match")
			assert.Equal(t, test.expectedValid, valid, "validity should match")
		})
	}
}
