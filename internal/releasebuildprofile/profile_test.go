package releasebuildprofile_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/releasebuildprofile"
	"github.com/stretchr/testify/require"
)

func TestReadReportRejectsUnboundedOrAmbiguousDocuments(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]string{
		"extra document":  "{} {}",
		"unknown field":   "{\"unreviewed\":true}",
		"invalid JSON":    "{",
		"oversized input": strings.Repeat(" ", releasebuildprofile.MaximumReportBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := releasebuildprofile.ReadReport(strings.NewReader(input))
			require.Error(t, err)
		})
	}
}
