package browser_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/browser"
	"github.com/stretchr/testify/assert"
)

func TestCommandFor(t *testing.T) {
	t.Parallel()

	const url = "http://127.0.0.1:8080/"

	tests := []struct {
		name     string
		goos     string
		wantName string
		wantArgs []string
	}{
		{
			name:     "darwin uses open",
			goos:     "darwin",
			wantName: "open",
			wantArgs: []string{url},
		},
		{
			name:     "windows uses rundll32",
			goos:     "windows",
			wantName: "rundll32",
			wantArgs: []string{"url.dll,FileProtocolHandler", url},
		},
		{
			name:     "linux falls back to xdg-open",
			goos:     "linux",
			wantName: "xdg-open",
			wantArgs: []string{url},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			name, args := browser.CommandFor(test.goos, url)
			assert.Equal(t, test.wantName, name)
			assert.Equal(t, test.wantArgs, args)
		})
	}
}
