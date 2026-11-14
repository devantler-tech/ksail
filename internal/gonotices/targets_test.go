package gonotices_test

import (
	"slices"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/gonotices"
	"github.com/stretchr/testify/require"
)

func TestValidateTargetsRequiresEveryDistinctRecipientGraph(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"complete", "missing-cli", "missing-desktop", "duplicate", "extra", "wrong-target"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			targets := []string{
				"cli-darwin-arm64",
				"cli-linux-amd64",
				"cli-linux-arm64",
				"cli-windows-amd64",
				"cli-windows-arm64",
				"desktop-darwin-arm64",
				"desktop-linux-amd64",
				"desktop-windows-amd64",
			}

			switch name {
			case "missing-cli":
				targets = targets[1:]
			case "missing-desktop":
				targets = targets[:7]
			case "duplicate":
				targets[7] = targets[6]
			case "extra":
				targets = append(targets, "cli-linux-s390x")
			case "wrong-target":
				targets[0] = "desktop-darwin-amd64"
			}

			slices.Reverse(targets)

			err := gonotices.ValidateTargets(targets)
			if name == "complete" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, gonotices.ErrIncomplete)
			}
		})
	}
}
