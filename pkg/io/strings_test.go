package io_test

import (
	"testing"

	io "github.com/devantler-tech/ksail/v5/pkg/io"
	"github.com/stretchr/testify/assert"
)

func TestSanitizeToDNSLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"whitespace only", "   ", ""},
		{"lowercase letters", "hello", "hello"},
		{"uppercase letters normalized", "HELLO", "hello"},
		{"mixed case", "HelloWorld", "helloworld"},
		{"spaces become hyphens", "hello world", "hello-world"},
		{"special characters become hyphens", "hello.world/foo", "hello-world-foo"},
		{"consecutive specials collapse to single hyphen", "hello...world", "hello-world"},
		{"leading specials trimmed", "...hello", "hello"},
		{"trailing specials trimmed", "hello...", "hello"},
		{"leading and trailing whitespace trimmed", "  hello  ", "hello"},
		{"numbers preserved", "hello123", "hello123"},
		{"numeric only", "12345", "12345"},
		{"mixed with numbers", "my-app-2.0", "my-app-2-0"},
		{"unicode characters become hyphens", "héllo wörld", "h-llo-w-rld"},
		{"single character", "a", "a"},
		{"single special becomes empty", ".", ""},
		{"path-like input", "k8s/clusters/local", "k8s-clusters-local"},
		{"underscores become hyphens", "my_app_name", "my-app-name"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := io.SanitizeToDNSLabel(test.input)

			assert.Equal(t, test.expected, result)
		})
	}
}

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
