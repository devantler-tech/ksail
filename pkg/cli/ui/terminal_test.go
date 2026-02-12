package ui

import (
	"bytes"
	"os"
	"testing"
)

func TestSetTerminalTitle(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{
			name:  "simple title",
			title: "KSail",
			want:  "\033]0;KSail\007",
		},
		{
			name:  "title with spaces",
			title: "KSail - Cluster Management",
			want:  "\033]0;KSail - Cluster Management\007",
		},
		{
			name:  "empty title",
			title: "",
			want:  "\033]0;\007",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Call the function
			SetTerminalTitle(tt.title)

			// Restore stdout
			w.Close()
			os.Stdout = oldStdout

			// Read captured output
			var buf bytes.Buffer
			buf.ReadFrom(r)
			got := buf.String()

			// Compare
			if got != tt.want {
				t.Errorf("SetTerminalTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}
