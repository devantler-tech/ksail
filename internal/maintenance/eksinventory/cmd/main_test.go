package main_test

import (
	"bytes"
	"strings"
	"testing"

	command "github.com/devantler-tech/ksail/v7/internal/maintenance/eksinventory/cmd"
)

func TestUsageDoesNotRequireAWS(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		args []string
		want int
	}{
		{[]string{"--help"}, 0},
		{nil, 2},
		{[]string{"--region"}, 2},
		{[]string{"--unknown"}, 2},
		{[]string{"--region", "eu-central-1;command"}, 2},
		{[]string{"--region", "eu-central-1", "extra"}, 2},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			if got := command.RunForTest(test.args, &output); got != test.want {
				t.Fatalf("exit %d, want %d", got, test.want)
			}
		})
	}
}
