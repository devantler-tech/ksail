package k8s_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
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

			result := k8s.SanitizeToDNSLabel(test.input)

			assert.Equal(t, test.expected, result)
		})
	}
}
