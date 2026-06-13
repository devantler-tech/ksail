package fsutil_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

const (
	docA = "kind: A"
	docB = "kind: B"
)

func docsToStrings(docs [][]byte) []string {
	out := make([]string, len(docs))
	for i, doc := range docs {
		out[i] = string(doc)
	}

	return out
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}

	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}

	return true
}

func TestSplitYAMLDocuments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "single document",
			input: "kind: ConfigMap\nmetadata:\n  name: a",
			want:  []string{"kind: ConfigMap\nmetadata:\n  name: a"},
		},
		{
			name:  "two documents",
			input: docA + "\n---\n" + docB,
			want:  []string{docA, docB},
		},
		{
			name:  "leading separator is handled",
			input: "---\n" + docA + "\n---\n" + docB + "\n",
			want:  []string{docA, docB},
		},
		{
			name:  "blank documents are dropped",
			input: docA + "\n---\n\n---\n" + docB,
			want:  []string{docA, docB},
		},
		{
			name:  "empty input yields no documents",
			input: "",
			want:  []string{},
		},
		{
			name:  "whitespace-only input yields no documents",
			input: "\n\n  \n",
			want:  []string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := docsToStrings(fsutil.SplitYAMLDocuments([]byte(test.input)))
			if !equalStrings(got, test.want) {
				t.Errorf("SplitYAMLDocuments(%q) = %#v, want %#v", test.input, got, test.want)
			}
		})
	}
}

// TestSplitYAMLDocuments_BlockScalarSeparator verifies the YAML-aware behavior:
// a line that looks like a separator but lives inside a block scalar must NOT
// split the document. This is the latent bug the naive strings.Split variants
// carried.
func TestSplitYAMLDocuments_BlockScalarSeparator(t *testing.T) {
	t.Parallel()

	input := "kind: ConfigMap\ndata:\n  script: |\n    line1\n    ---\n    line3\n"

	got := fsutil.SplitYAMLDocuments([]byte(input))
	if len(got) != 1 {
		t.Fatalf("expected the block scalar to stay a single document, got %d documents: %#v",
			len(got), docsToStrings(got))
	}
}

func TestIsYAMLFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{"config.yaml", true},
		{"config.yml", true},
		{"CONFIG.YAML", true},
		{"config.YML", true},
		{"/abs/path/to/file.yaml", true},
		{"config.json", false},
		{"config", false},
		{"config.yamlx", false},
		{"yaml", false},
		{"", false},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()

			got := fsutil.IsYAMLFile(test.path)
			if got != test.want {
				t.Errorf("IsYAMLFile(%q) = %v, want %v", test.path, got, test.want)
			}
		})
	}
}
