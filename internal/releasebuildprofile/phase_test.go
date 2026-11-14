package releasebuildprofile_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/releasebuildprofile"
	"github.com/stretchr/testify/require"
)

func TestPhaseSecondsRejectsHookReceiptOrMissingBoundary(t *testing.T) {
	t.Parallel()

	const (
		begin = "2026-11-02T00:00:00Z\t  • building binaries\n"
		end   = "2026-11-02T00:01:00Z\t  • writing artifacts metadata\n"
	)

	for name, receipt := range map[string]string{
		"complete phase":   "2026-11-02T00:01:00Z\t      • took: 1m0s\n",
		"nested hook only": "2026-11-02T00:01:00Z\t      • took: 10s\n",
		"missing receipt":  "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			seconds, err := releasebuildprofile.PhaseSeconds(
				strings.NewReader(begin + receipt + end),
			)
			if name != "complete phase" {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)

			const tolerance = 0.0001
			require.InDelta(t, 60, seconds, tolerance)
		})
	}
}
